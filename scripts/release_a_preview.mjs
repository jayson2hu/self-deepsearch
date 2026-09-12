#!/usr/bin/env node

import { promises as fs } from "node:fs";
import net from "node:net";
import path from "node:path";
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";

import {
  DEFAULT_TIMEOUT_MS,
  ensureArtifacts,
  prepareStandaloneAssets,
  REPOSITORY_ROOT,
  SmokeFailure,
  startChild,
  startMockUpstream,
  stopChild,
  waitForHTTP,
} from "./release_a_frontend_smoke.mjs";

export const DEFAULT_PREVIEW_PORTS = Object.freeze({ display: 4173, ops: 4174, api: 4175 });

function parsePort(value, option) {
  const port = Number(value);
  if (!Number.isInteger(port) || port < 1024 || port > 65_535) {
    throw new SmokeFailure(`${option} 必须是 1024-65535 之间的整数`);
  }
  return port;
}

export function parsePreviewArgs(argv) {
  const options = {
    build: false,
    displayPort: DEFAULT_PREVIEW_PORTS.display,
    opsPort: DEFAULT_PREVIEW_PORTS.ops,
    apiPort: DEFAULT_PREVIEW_PORTS.api,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    help: false,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") options.build = true;
    else if (argument === "--display-port") options.displayPort = parsePort(argv[++index], argument);
    else if (argument === "--ops-port") options.opsPort = parsePort(argv[++index], argument);
    else if (argument === "--api-port") options.apiPort = parsePort(argv[++index], argument);
    else if (argument === "--timeout") {
      const timeoutMs = Number(argv[++index]);
      if (!Number.isFinite(timeoutMs) || timeoutMs < 5_000) {
        throw new SmokeFailure("--timeout 必须是至少 5000 毫秒的数字");
      }
      options.timeoutMs = timeoutMs;
    } else if (argument === "--help" || argument === "-h") options.help = true;
    else throw new SmokeFailure(`未知参数：${argument}`);
  }
  const ports = [options.displayPort, options.opsPort, options.apiPort];
  if (new Set(ports).size !== ports.length) throw new SmokeFailure("公开站、运营后台和 mock API 端口不能重复");
  return options;
}

export function previewHelp() {
  return [
    "用法：node scripts/release_a_preview.mjs [选项]",
    "",
    "在 127.0.0.1 持续运行 Release A 公开站、运营后台和有状态合成 mock API；按 Ctrl+C 停止。",
    "不会启动 Docker、PostgreSQL、邮件、对象存储、自动采集或广告联盟。所有状态在进程退出后丢弃。",
    "",
    "--build                  启动前重新构建两套 Next.js 应用",
    "--display-port <端口>    公开站端口，默认 4173",
    "--ops-port <端口>        运营后台端口，默认 4174",
    "--api-port <端口>        本地 mock API 端口，默认 4175",
    "--timeout <毫秒>         等待前端启动的时间，默认 45000",
  ].join("\n");
}

export function previewSummary({ displayPort, opsPort, apiPort }) {
  return {
    status: "ready",
    displayURL: `http://127.0.0.1:${displayPort}/`,
    opsURL: `http://127.0.0.1:${opsPort}/login`,
    mockApiURL: `http://127.0.0.1:${apiPort}`,
    verificationCode: "123456",
    accounts: [
      { surface: "公开站", role: "user", email: "browser-user@example.test", password: "Release-A-browser-pass" },
      { surface: "运营后台", role: "owner", email: "owner@example.test", password: "Release-A-owner-pass" },
      { surface: "运营后台", role: "admin", email: "operator@example.test", password: "Release-A-operator-pass" },
      { surface: "运营后台", role: "editor", email: "editor@example.test", password: "Release-A-editor-pass" },
    ],
    warning: "仅用于本机合成数据预览；不代表真实 PostgreSQL、SMTP、S3/R2 或 Cloudflare 已联调。",
  };
}

export async function assertLocalPortsAvailable(ports, host = "127.0.0.1") {
  for (const port of ports) {
    await new Promise((resolve, reject) => {
      const probe = net.createServer();
      probe.unref();
      probe.once("error", (error) => {
        reject(new SmokeFailure(`本地预览端口 ${host}:${port} 不可用：${error.code ?? error.message}`));
      });
      probe.listen(port, host, () => {
        probe.close((error) => {
          if (error) reject(new SmokeFailure(`无法释放端口检查 ${host}:${port}：${error.message}`));
          else resolve();
        });
      });
    });
  }
}

function waitForStop(children) {
  return new Promise((resolve, reject) => {
    const cleanupListeners = () => {
      process.off("SIGINT", onInterrupt);
      process.off("SIGTERM", onTermination);
      for (const entry of children) entry.child.off("exit", entry.onExit);
    };
    const finish = (value) => {
      cleanupListeners();
      resolve(value);
    };
    const fail = (error) => {
      cleanupListeners();
      reject(error);
    };
    const onInterrupt = () => finish(130);
    const onTermination = () => finish(143);
    process.once("SIGINT", onInterrupt);
    process.once("SIGTERM", onTermination);
    for (const entry of children) {
      entry.onExit = (code, signal) => fail(new SmokeFailure(`${entry.name} 意外退出（code=${code ?? "null"}, signal=${signal ?? "none"}）`));
      entry.child.once("exit", entry.onExit);
    }
  });
}

export async function runPreview(options, repositoryRoot = REPOSITORY_ROOT) {
  const paths = await ensureArtifacts({ build: options.build, repositoryRoot });
  const stagedAssets = [];
  let upstream;
  let display;
  let ops;
  try {
    await assertLocalPortsAvailable([options.displayPort, options.opsPort, options.apiPort]);
    stagedAssets.push(...await prepareStandaloneAssets(paths.display));
    stagedAssets.push(...await prepareStandaloneAssets(paths.ops));
    process.env.APP_ENV = "test";
    process.env.TURNSTILE_BYPASS = "true";
    process.env.SITE_URL = `http://127.0.0.1:${options.displayPort}`;
    upstream = await startMockUpstream(options.apiPort, { accountMode: "stateful" });
    const apiURL = `http://127.0.0.1:${options.apiPort}`;
    display = startChild({
      name: "display-web-preview",
      server: paths.display.server,
      port: options.displayPort,
      apiURL,
      repositoryRoot,
      stdio: "inherit",
    });
    ops = startChild({
      name: "ops-web-preview",
      server: paths.ops.server,
      port: options.opsPort,
      apiURL,
      repositoryRoot,
      stdio: "inherit",
    });
    await Promise.all([
      waitForHTTP(display, "/", options.timeoutMs),
      waitForHTTP(ops, "/login", options.timeoutMs),
    ]);
    console.log(JSON.stringify(previewSummary(options), null, 2));
    console.log("\n预览服务正在运行；按 Ctrl+C 停止并清理。\n");
    return await waitForStop([display, ops]);
  } finally {
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(() => resolve()));
    }
    await Promise.all([display, ops].filter(Boolean).map((entry) => stopChild(entry)));
    await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
  }
}

export async function main(argv = process.argv.slice(2)) {
  const options = parsePreviewArgs(argv);
  if (options.help) {
    console.log(previewHelp());
    return 0;
  }
  try {
    return await runPreview(options);
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    return 1;
  }
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === entryPoint) process.exitCode = await main();
