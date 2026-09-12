#!/usr/bin/env node

import { createHash } from "node:crypto";
import { promises as fs } from "node:fs";
import path from "node:path";
import { performance } from "node:perf_hooks";
import process from "node:process";
import { pathToFileURL } from "node:url";

import {
  DEFAULT_TIMEOUT_MS,
  ensureArtifacts,
  findFreePort,
  prepareStandaloneAssets,
  REPOSITORY_ROOT,
  SmokeFailure,
  startChild,
  startMockUpstream,
  stopChild,
  waitForHTTP,
} from "./release_a_frontend_smoke.mjs";
import { validateDisplayURL } from "./release_a_performance_probe.mjs";
import { shanghaiDate } from "./release_a_visual_capture.mjs";

export const HTTP_LATENCY_THRESHOLDS = Object.freeze({ publicApiP95Ms: 300, searchP95Ms: 500 });
export const LOCAL_SEARCH_CODE = "TEST-001";
const MAX_RESPONSE_BYTES = 2 * 1024 * 1024;

function argumentValue(argv, index, option) {
  const value = argv[index + 1];
  if (!value || value.startsWith("--")) throw new SmokeFailure(`${option} 缺少参数值`);
  return value;
}

function parseInteger(value, option, minimum, maximum) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < minimum || parsed > maximum) {
    throw new SmokeFailure(`${option} 必须是 ${minimum}-${maximum} 之间的整数`);
  }
  return parsed;
}

function validateDate(value) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new SmokeFailure("--date 必须使用 YYYY-MM-DD 格式");
  const [year, month, day] = value.split("-").map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) {
    throw new SmokeFailure("--date 不是有效日期");
  }
  return value;
}

function validateSearchCode(value) {
  const code = value.trim();
  if (!/^[A-Za-z0-9._-]{1,64}$/.test(code)) {
    throw new SmokeFailure("--search-code 必须是 1-64 位番号字符，仅允许字母、数字、点、下划线和连字符");
  }
  return code;
}

export function parseHTTPLatencyArgs(argv, now = new Date()) {
  const options = {
    build: false,
    date: shanghaiDate(now),
    displayURL: null,
    help: false,
    output: null,
    reportOnly: false,
    samples: 20,
    searchCode: null,
    timeoutMs: Math.min(DEFAULT_TIMEOUT_MS, 15_000),
    warmup: 3,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") options.build = true;
    else if (argument === "--report-only") options.reportOnly = true;
    else if (argument === "--date") {
      options.date = validateDate(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--display-url") {
      options.displayURL = validateDisplayURL(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--output") {
      options.output = argumentValue(argv, index, argument);
      index += 1;
    } else if (argument === "--samples") {
      options.samples = parseInteger(argumentValue(argv, index, argument), argument, 5, 100);
      index += 1;
    } else if (argument === "--search-code") {
      options.searchCode = validateSearchCode(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--timeout") {
      options.timeoutMs = parseInteger(argumentValue(argv, index, argument), argument, 1_000, 60_000);
      index += 1;
    } else if (argument === "--warmup") {
      options.warmup = parseInteger(argumentValue(argv, index, argument), argument, 0, 20);
      index += 1;
    } else if (argument === "--help" || argument === "-h") options.help = true;
    else throw new SmokeFailure(`未知参数：${argument}`);
  }
  if (options.displayURL && !options.searchCode) throw new SmokeFailure("远程 HTTP 延迟探针必须显式提供 --search-code");
  if (options.displayURL && options.build) throw new SmokeFailure("--build 只适用于本地合成模式，不能与 --display-url 同时使用");
  if (!options.searchCode) options.searchCode = LOCAL_SEARCH_CODE;
  return options;
}

export function httpLatencyHelp() {
  return [
    "用法：node scripts/release_a_http_latency_probe.mjs [选项]",
    "",
    "低并发、顺序采样公开首页 API 与番号搜索 API 的完整响应耗时，并验证 JSON/HTTP 合同。",
    "默认启动本地合成环境；提供 --display-url 后探测最终 HTTPS 公开站同源 API。",
    "报告不保存搜索番号或响应正文，只保存番号 SHA-256、响应字节和耗时。",
    "",
    "--build                       本地探测前重新构建两套 Next.js 应用",
    "--display-url <HTTPS origin>  最终公开站 origin；远程模式必须使用 HTTPS",
    "--search-code <番号>          远程模式必填；本地默认 TEST-001",
    "--samples <5-100>             每个接口的预热后顺序采样数，默认 20",
    "--warmup <0-20>               每个接口的预热次数，默认 3",
    "--timeout <毫秒>              单请求上限，默认 15000",
    "--output <JSON 文件>          必须是 docs/evidence 下的直接 JSON 文件",
    "--report-only                 即使 P95 阈值未通过也返回成功，只保留报告",
  ].join("\n");
}

export function resolveHTTPLatencyOutput(options, repositoryRoot = REPOSITORY_ROOT) {
  const evidenceRoot = path.resolve(repositoryRoot, "docs", "evidence");
  const defaultName = options.displayURL
    ? `release-a-http-latency-target-${options.date}.json`
    : `release-a-http-latency-${options.date}.json`;
  const output = options.output ? path.resolve(repositoryRoot, options.output) : path.join(evidenceRoot, defaultName);
  const relative = path.relative(evidenceRoot, output);
  const segments = relative.split(path.sep).filter(Boolean);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative) || segments.length !== 1 || path.extname(output) !== ".json") {
    throw new SmokeFailure("HTTP 延迟证据必须写入 docs/evidence 下的直接 .json 文件");
  }
  return { evidenceRoot, output };
}

async function assertOutputFileSafe(output) {
  try {
    const stat = await fs.lstat(output);
    if (stat.isSymbolicLink()) throw new SmokeFailure("HTTP 延迟证据输出文件不能是符号链接");
    if (!stat.isFile()) throw new SmokeFailure("HTTP 延迟证据输出路径已存在但不是文件");
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
}

export function percentile(values, quantile) {
  if (!values.length) return null;
  const sorted = [...values].sort((left, right) => left - right);
  const rank = Math.max(1, Math.ceil(quantile * sorted.length));
  return sorted[rank - 1];
}

function round(value, digits = 2) {
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

export function summarizeLatencySamples(samples) {
  const durations = samples.map((sample) => sample.duration_ms);
  const bytes = samples.map((sample) => sample.response_bytes);
  return {
    duration_ms: {
      p50: round(percentile(durations, 0.5)),
      p95: round(percentile(durations, 0.95)),
      min: round(Math.min(...durations)),
      max: round(Math.max(...durations)),
    },
    response_bytes: {
      p50: round(percentile(bytes, 0.5), 0),
      min: Math.min(...bytes),
      max: Math.max(...bytes),
    },
  };
}

export function latencyGate(result) {
  const threshold = result.endpoint.key === "public-home-api"
    ? HTTP_LATENCY_THRESHOLDS.publicApiP95Ms
    : HTTP_LATENCY_THRESHOLDS.searchP95Ms;
  const p95 = result.summary.duration_ms.p95;
  return p95 < threshold ? [] : [`${result.endpoint.key} P95 ${p95}ms 未低于 ${threshold}ms`];
}

async function requestJSON(url, expectedOrigin, timeoutMs) {
  const started = performance.now();
  const response = await fetch(url, {
    headers: { accept: "application/json", "user-agent": "self-deepsearch-release-a-latency-probe/1" },
    redirect: "manual",
    signal: AbortSignal.timeout(timeoutMs),
  });
  if (new URL(response.url || url).origin !== expectedOrigin) throw new SmokeFailure(`${url} 响应 origin 发生变化，探针已停止`);
  if (response.status !== 200) throw new SmokeFailure(`${url} 返回 HTTP ${response.status}，期望 200`);
  const contentType = response.headers.get("content-type") ?? "";
  if (!contentType.toLowerCase().includes("application/json")) throw new SmokeFailure(`${url} 未返回 application/json`);
  const declaredLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > MAX_RESPONSE_BYTES) throw new SmokeFailure(`${url} 响应超过 2 MiB 安全上限`);
  const body = await response.arrayBuffer();
  if (body.byteLength > MAX_RESPONSE_BYTES) throw new SmokeFailure(`${url} 响应超过 2 MiB 安全上限`);
  try {
    JSON.parse(new TextDecoder().decode(body));
  } catch {
    throw new SmokeFailure(`${url} 返回了无效 JSON`);
  }
  return { duration_ms: round(performance.now() - started), response_bytes: body.byteLength };
}

async function measureEndpoint(origin, endpoint, options) {
  const url = new URL(endpoint.path, origin);
  if (endpoint.key === "search-api") url.searchParams.set("q", options.searchCode);
  for (let index = 0; index < options.warmup; index += 1) await requestJSON(url.href, origin, options.timeoutMs);
  const samples = [];
  for (let index = 1; index <= options.samples; index += 1) {
    samples.push({ sample: index, ...await requestJSON(url.href, origin, options.timeoutMs) });
  }
  const result = { endpoint: { key: endpoint.key, path: endpoint.path }, samples, summary: summarizeLatencySamples(samples) };
  result.failures = latencyGate(result);
  result.status = result.failures.length ? "failed" : "passed";
  return result;
}

export function httpLatencyReport({ date, generatedAt, mode, origin, options, results }) {
  const failures = results.flatMap((result) => result.failures);
  return {
    schema_version: 1,
    release: "A",
    status: failures.length ? "failed" : "passed",
    generated_at: generatedAt,
    timezone: "Asia/Shanghai",
    evidence_date: date,
    mode,
    target_origin: mode === "remote-target" ? origin : "local-random-loopback",
    sampling: { concurrency: 1, warmup_per_endpoint: options.warmup, samples_per_endpoint: options.samples },
    thresholds: {
      public_api_p95_ms_strictly_less_than: HTTP_LATENCY_THRESHOLDS.publicApiP95Ms,
      search_p95_ms_strictly_less_than: HTTP_LATENCY_THRESHOLDS.searchP95Ms,
    },
    privacy: {
      response_bodies_stored: false,
      search_code_stored: false,
      search_code_sha256: createHash("sha256").update(options.searchCode.trim().toUpperCase()).digest("hex"),
    },
    synthetic_boundary: mode === "local-synthetic"
      ? "Next.js standalone proxy with memory-only synthetic API; no Docker/PostgreSQL/Cloudflare/network latency"
      : "Low-concurrency lab requests to the supplied public origin; not real-user traffic or server-side percentile telemetry",
    failures,
    results,
  };
}

async function writeReport(report, options, repositoryRoot) {
  const { evidenceRoot, output } = resolveHTTPLatencyOutput(options, repositoryRoot);
  await fs.mkdir(evidenceRoot, { recursive: true });
  await assertOutputFileSafe(output);
  const temporary = path.join(evidenceRoot, `.${path.basename(output)}.${process.pid}.${Date.now()}.tmp`);
  try {
    await fs.writeFile(temporary, `${JSON.stringify(report, null, 2)}\n`, { encoding: "utf8", flag: "wx" });
    await fs.rm(output, { force: true });
    await fs.rename(temporary, output);
  } finally {
    await fs.rm(temporary, { force: true });
  }
  return output;
}

async function measureOrigin(origin, mode, options, repositoryRoot) {
  const endpoints = [
    { key: "public-home-api", path: "/api/v1/site/home" },
    { key: "search-api", path: "/api/v1/search/works" },
  ];
  const results = [];
  for (const endpoint of endpoints) results.push(await measureEndpoint(origin, endpoint, options));
  const report = httpLatencyReport({ date: options.date, generatedAt: new Date().toISOString(), mode, origin, options, results });
  const output = await writeReport(report, options, repositoryRoot);
  return { output, report };
}

async function runLocalProbe(options, repositoryRoot) {
  const paths = await ensureArtifacts({ build: options.build, repositoryRoot });
  const stagedAssets = [];
  let upstream;
  let display;
  try {
    stagedAssets.push(...await prepareStandaloneAssets(paths.display));
    const ports = new Set();
    const displayPort = await findFreePort({ avoid: ports });
    ports.add(displayPort);
    const apiPort = await findFreePort({ avoid: ports });
    process.env.APP_ENV = "test";
    process.env.TURNSTILE_BYPASS = "true";
    process.env.SITE_URL = `http://127.0.0.1:${displayPort}`;
    upstream = await startMockUpstream(apiPort, { accountMode: "stateful" });
    display = startChild({
      name: "display-web-http-latency-probe",
      server: paths.display.server,
      port: displayPort,
      apiURL: `http://127.0.0.1:${apiPort}`,
      repositoryRoot,
      stdio: "inherit",
    });
    await waitForHTTP(display, "/", options.timeoutMs);
    return await measureOrigin(`http://127.0.0.1:${displayPort}`, "local-synthetic", options, repositoryRoot);
  } finally {
    await stopChild(display);
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(() => resolve()));
    }
    await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
  }
}

export async function runHTTPLatencyProbe(options, repositoryRoot = REPOSITORY_ROOT) {
  resolveHTTPLatencyOutput(options, repositoryRoot);
  return options.displayURL
    ? measureOrigin(options.displayURL, "remote-target", options, repositoryRoot)
    : runLocalProbe(options, repositoryRoot);
}

export async function main(argv = process.argv.slice(2)) {
  try {
    const options = parseHTTPLatencyArgs(argv);
    if (options.help) {
      console.log(httpLatencyHelp());
      return 0;
    }
    const result = await runHTTPLatencyProbe(options);
    console.log(JSON.stringify({
      status: result.report.status,
      output: result.output,
      failures: result.report.failures,
      warning: result.report.synthetic_boundary,
    }, null, 2));
    return result.report.status === "passed" || options.reportOnly ? 0 : 1;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    if (error instanceof SmokeFailure && error.details) console.error(error.details);
    return 1;
  }
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === entryPoint) process.exitCode = await main();
