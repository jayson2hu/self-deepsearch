#!/usr/bin/env node

import { chromium } from "@playwright/test";
import { createHash } from "node:crypto";
import { promises as fs } from "node:fs";
import path from "node:path";
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

export const VISUAL_EVIDENCE_ROOT = path.join("docs", "evidence");
export const VISUAL_CAPTURE_PLAN = Object.freeze([
  Object.freeze({
    key: "display-home",
    surface: "display",
    route: "/",
    heading: "按番号找到作品公开资料",
    desktopHeight: 900,
  }),
  Object.freeze({
    key: "work-detail",
    surface: "display",
    route: "/works/test-001-00000002",
    heading: "示例作品资料：春日档案",
    desktopHeight: 1000,
  }),
  Object.freeze({
    key: "ops-login",
    surface: "ops",
    route: "/login",
    heading: "后台登录",
    desktopHeight: 900,
    auth: "anonymous",
  }),
  Object.freeze({
    key: "ops-dashboard",
    surface: "ops",
    route: "/",
    heading: "运营概览",
    desktopHeight: 900,
    auth: "owner",
  }),
  Object.freeze({
    key: "ops-users",
    surface: "ops",
    route: "/users",
    heading: "用户与权限",
    desktopHeight: 900,
    auth: "owner",
  }),
  Object.freeze({
    key: "ops-audit",
    surface: "ops",
    route: "/audit?request_id=release-a%3Apublish-42",
    heading: "审计查询",
    desktopHeight: 900,
    auth: "owner",
  }),
]);

export const VISUAL_VIEWPORTS = Object.freeze([
  Object.freeze({ key: "desktop", width: 1440, height: 900, isMobile: false, hasTouch: false }),
  Object.freeze({ key: "mobile", width: 390, height: 844, isMobile: true, hasTouch: true }),
]);

function argumentValue(argv, index, option) {
  const value = argv[index + 1];
  if (!value || value.startsWith("--")) throw new SmokeFailure(`${option} 缺少参数值`);
  return value;
}

function parseTimeout(value) {
  const timeoutMs = Number(value);
  if (!Number.isFinite(timeoutMs) || timeoutMs < 5_000) {
    throw new SmokeFailure("--timeout 必须是至少 5000 毫秒的数字");
  }
  return timeoutMs;
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

export function shanghaiDate(now = new Date()) {
  const parts = new Intl.DateTimeFormat("en", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]));
  return `${values.year}-${values.month}-${values.day}`;
}

export function parseVisualArgs(argv, now = new Date()) {
  const options = {
    build: false,
    browserChannel: "chrome",
    date: shanghaiDate(now),
    outputDir: null,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    help: false,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") options.build = true;
    else if (argument === "--browser-channel") {
      const channel = argumentValue(argv, index, argument);
      if (!new Set(["chrome", "chromium"]).has(channel)) {
        throw new SmokeFailure("--browser-channel 只允许 chrome 或 chromium");
      }
      options.browserChannel = channel;
      index += 1;
    } else if (argument === "--date") {
      options.date = validateDate(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--output-dir") {
      options.outputDir = argumentValue(argv, index, argument);
      index += 1;
    } else if (argument === "--timeout") {
      options.timeoutMs = parseTimeout(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--help" || argument === "-h") options.help = true;
    else throw new SmokeFailure(`未知参数：${argument}`);
  }
  return options;
}

export function visualHelp() {
  return [
    "用法：node scripts/release_a_visual_capture.mjs [选项]",
    "",
    "在随机回环端口启动 Release A 合成环境，用本机浏览器生成公开站和运营后台桌面/移动全页截图。",
    "不会启动 Docker、PostgreSQL、邮件、对象存储、自动采集或广告联盟。",
    "",
    "--build                       截图前重新构建两套 Next.js 应用",
    "--browser-channel <名称>      chrome（默认）或已安装的 Playwright chromium",
    "--date <YYYY-MM-DD>           证据日期，默认使用 Asia/Shanghai 当天",
    "--output-dir <目录>           输出目录，必须位于 docs/evidence 内",
    "--timeout <毫秒>              页面和服务等待时间，默认 45000",
  ].join("\n");
}

export function resolveEvidenceOutput(options, repositoryRoot = REPOSITORY_ROOT) {
  const evidenceRoot = path.resolve(repositoryRoot, VISUAL_EVIDENCE_ROOT);
  const output = options.outputDir
    ? path.resolve(repositoryRoot, options.outputDir)
    : path.join(evidenceRoot, `release-a-visual-${options.date}`);
  const relative = path.relative(evidenceRoot, output);
  const segments = relative.split(path.sep).filter(Boolean);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative) || segments.length !== 1) {
    throw new SmokeFailure("视觉证据输出目录必须是 docs/evidence 下的独立子目录");
  }
  return { evidenceRoot, output };
}

async function assertOutputDirectorySafe(output) {
  try {
    const stat = await fs.lstat(output);
    if (stat.isSymbolicLink()) throw new SmokeFailure("视觉证据输出目录不能是符号链接");
    if (!stat.isDirectory()) throw new SmokeFailure("视觉证据输出路径已存在但不是目录");
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
}

export function browserLaunchOptions(browserChannel) {
  return {
    headless: true,
    channel: browserChannel === "chromium" ? undefined : browserChannel,
  };
}

function captureViewport(plan, viewport) {
  return {
    width: viewport.width,
    height: viewport.key === "desktop" ? plan.desktopHeight : viewport.height,
  };
}

export function plannedCaptures() {
  return VISUAL_VIEWPORTS.flatMap((viewport) => VISUAL_CAPTURE_PLAN.map((plan) => ({
    ...plan,
    filename: `${plan.key}-${viewport.key}.png`,
    viewport: captureViewport(plan, viewport),
    viewportKey: viewport.key,
  })));
}

async function waitForVisualReady(page, heading, timeoutMs) {
  await page.getByRole("heading", { name: heading, exact: true }).waitFor({ state: "visible", timeout: timeoutMs });
  await page.waitForLoadState("networkidle", { timeout: Math.min(timeoutMs, 3_000) }).catch(() => undefined);
  await page.evaluate(async (imageTimeoutMs) => {
    await document.fonts?.ready;
    const step = Math.max(Math.floor(window.innerHeight * 0.8), 240);
    for (let y = 0; y < document.documentElement.scrollHeight; y += step) {
      window.scrollTo(0, y);
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    window.scrollTo(0, 0);
    const imagesReady = Promise.all(Array.from(document.images, (image) => {
      if (image.complete) return Promise.resolve();
      return new Promise((resolve) => {
        image.addEventListener("load", resolve, { once: true });
        image.addEventListener("error", resolve, { once: true });
      });
    }));
    await Promise.race([
      imagesReady,
      new Promise((resolve) => setTimeout(resolve, imageTimeoutMs)),
    ]);
  }, Math.min(timeoutMs, 5_000));
  const pageState = await page.evaluate(() => ({
    brokenImages: Array.from(document.images)
      .filter((image) => image.getClientRects().length > 0 && image.naturalWidth === 0)
      .map((image) => image.getAttribute("src") ?? "<missing-src>"),
    horizontalOverflow: document.documentElement.scrollWidth > window.innerWidth,
  }));
  if (pageState.horizontalOverflow) throw new SmokeFailure(`${page.url()} 存在页面级横向溢出`);
  if (pageState.brokenImages.length) {
    throw new SmokeFailure(`${page.url()} 存在坏图：${pageState.brokenImages.join(", ")}`);
  }
}

async function openVisualPage(page, url, heading, timeoutMs) {
  const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: timeoutMs });
  if (!response || !response.ok()) {
    throw new SmokeFailure(`${url} 截图导航失败：HTTP ${response?.status() ?? "no-response"}`);
  }
  await waitForVisualReady(page, heading, timeoutMs);
}

async function loginOwner(page, opsURL, timeoutMs) {
  await openVisualPage(page, `${opsURL}/login`, "后台登录", timeoutMs);
  await page.getByLabel("邮箱", { exact: true }).fill("owner@example.test");
  await page.getByLabel("密码", { exact: true }).fill("Release-A-owner-pass");
  await page.getByRole("button", { name: "登录", exact: true }).click();
  await page.waitForURL(`${opsURL}/`, { timeout: timeoutMs });
  await waitForVisualReady(page, "运营概览", timeoutMs);
}

async function fileIntegrity(filePath) {
  const data = await fs.readFile(filePath);
  return {
    bytes: data.byteLength,
    sha256: createHash("sha256").update(data).digest("hex"),
  };
}

export function visualManifest({ date, generatedAt, files }) {
  return {
    schema_version: 1,
    release: "A",
    generated_at: generatedAt,
    timezone: "Asia/Shanghai",
    evidence_date: date,
    source: {
      frontend: "Next.js production standalone build",
      api: "stateful synthetic mock",
      browser: "local Playwright-controlled browser",
    },
    synthetic_data: {
      fixtures: ["TEST-001"],
      operations_account: "owner@example.test",
      persistence: "memory-only; cleared when capture exits",
    },
    excluded_dependencies: [
      "Docker",
      "PostgreSQL",
      "SMTP/Mailpit",
      "S3/R2",
      "Cloudflare",
      "automatic collection",
      "ad network scripts",
    ],
    files,
  };
}

async function captureViewportSet({ browser, displayURL, opsURL, output, timeoutMs, viewport }) {
  const context = await browser.newContext({
    viewport: { width: viewport.width, height: viewport.height },
    isMobile: viewport.isMobile,
    hasTouch: viewport.hasTouch,
    deviceScaleFactor: 1,
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    colorScheme: "light",
    reducedMotion: "reduce",
  });
  const page = await context.newPage();
  const pageErrors = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  const files = [];
  let ownerLoggedIn = false;
  try {
    for (const plan of VISUAL_CAPTURE_PLAN) {
      const viewportSize = captureViewport(plan, viewport);
      await page.setViewportSize(viewportSize);
      const baseURL = plan.surface === "display" ? displayURL : opsURL;
      if (plan.auth === "owner" && !ownerLoggedIn) {
        await loginOwner(page, opsURL, timeoutMs);
        ownerLoggedIn = true;
      } else {
        await openVisualPage(page, `${baseURL}${plan.route}`, plan.heading, timeoutMs);
      }
      if (pageErrors.length) throw new SmokeFailure(`${page.url()} 出现浏览器脚本异常：${pageErrors.join(" | ")}`);
      const filename = `${plan.key}-${viewport.key}.png`;
      const filePath = path.join(output, filename);
      await page.screenshot({ path: filePath, fullPage: true, animations: "disabled" });
      files.push({
        file: filename,
        surface: plan.surface,
        route: plan.route,
        auth: plan.auth ?? "anonymous",
        viewport: viewportSize,
        ...await fileIntegrity(filePath),
      });
    }
    await page.waitForTimeout(100);
    return files;
  } finally {
    await context.close();
  }
}

export async function runVisualCapture(options, repositoryRoot = REPOSITORY_ROOT) {
  const { evidenceRoot, output } = resolveEvidenceOutput(options, repositoryRoot);
  const paths = await ensureArtifacts({ build: options.build, repositoryRoot });
  await fs.mkdir(evidenceRoot, { recursive: true });
  await assertOutputDirectorySafe(output);
  const staging = await fs.mkdtemp(path.join(evidenceRoot, ".release-a-visual-"));
  const stagedAssets = [];
  let upstream;
  let display;
  let ops;
  let browser;
  try {
    stagedAssets.push(...await prepareStandaloneAssets(paths.display));
    stagedAssets.push(...await prepareStandaloneAssets(paths.ops));
    const ports = new Set();
    const displayPort = await findFreePort({ avoid: ports });
    ports.add(displayPort);
    const opsPort = await findFreePort({ avoid: ports });
    ports.add(opsPort);
    const apiPort = await findFreePort({ avoid: ports });

    process.env.APP_ENV = "test";
    process.env.TURNSTILE_BYPASS = "true";
    process.env.SITE_URL = `http://127.0.0.1:${displayPort}`;
    upstream = await startMockUpstream(apiPort, { accountMode: "stateful" });
    const apiURL = `http://127.0.0.1:${apiPort}`;
    display = startChild({
      name: "display-web-visual-capture",
      server: paths.display.server,
      port: displayPort,
      apiURL,
      repositoryRoot,
      stdio: "inherit",
    });
    ops = startChild({
      name: "ops-web-visual-capture",
      server: paths.ops.server,
      port: opsPort,
      apiURL,
      repositoryRoot,
      stdio: "inherit",
    });
    await Promise.all([
      waitForHTTP(display, "/", options.timeoutMs),
      waitForHTTP(ops, "/login", options.timeoutMs),
    ]);

    try {
      browser = await chromium.launch(browserLaunchOptions(options.browserChannel));
    } catch (error) {
      throw new SmokeFailure(
        `无法启动 ${options.browserChannel}：${error instanceof Error ? error.message : String(error)}`,
        options.browserChannel === "chromium"
          ? "请先安装 Playwright Chromium，或改用 --browser-channel chrome。"
          : "请确认本机已安装 Chrome，或使用已安装的 Playwright Chromium。",
      );
    }
    const displayURL = `http://127.0.0.1:${displayPort}`;
    const opsURL = `http://127.0.0.1:${opsPort}`;
    const files = [];
    for (const viewport of VISUAL_VIEWPORTS) {
      files.push(...await captureViewportSet({
        browser,
        displayURL,
        opsURL,
        output: staging,
        timeoutMs: options.timeoutMs,
        viewport,
      }));
    }

    await fs.mkdir(output, { recursive: true });
    for (const entry of files) await fs.copyFile(path.join(staging, entry.file), path.join(output, entry.file));
    const manifest = visualManifest({
      date: options.date,
      generatedAt: new Date().toISOString(),
      files,
    });
    const manifestPath = path.join(output, "manifest.json");
    await fs.writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
    return { output, manifestPath, files };
  } finally {
    if (browser) await browser.close();
    await Promise.all([display, ops].filter(Boolean).map((entry) => stopChild(entry)));
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(() => resolve()));
    }
    await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
    await fs.rm(staging, { recursive: true, force: true });
  }
}

export async function main(argv = process.argv.slice(2)) {
  try {
    const options = parseVisualArgs(argv);
    if (options.help) {
      console.log(visualHelp());
      return 0;
    }
    const result = await runVisualCapture(options);
    console.log(JSON.stringify({
      status: "captured",
      output: result.output,
      manifest: result.manifestPath,
      files: result.files.length,
      warning: "仅为本地合成数据视觉证据，不代表真实依赖已经联调。",
    }, null, 2));
    return 0;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    if (error instanceof SmokeFailure && error.details) console.error(error.details);
    return 1;
  }
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === entryPoint) process.exitCode = await main();
