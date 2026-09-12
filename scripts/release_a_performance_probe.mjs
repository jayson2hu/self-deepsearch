#!/usr/bin/env node

import { chromium } from "@playwright/test";
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
import { browserLaunchOptions, shanghaiDate } from "./release_a_visual_capture.mjs";

export const PERFORMANCE_THRESHOLDS = Object.freeze({ lcpMs: 2_500, mobileCLS: 0.1 });
export const REQUEST_DRAIN = Object.freeze({ timeoutMs: 2_000, quietMs: 250, pollMs: 25 });
export const PERFORMANCE_VIEWPORTS = Object.freeze([
  Object.freeze({ key: "desktop", width: 1366, height: 900, isMobile: false, hasTouch: false }),
  Object.freeze({ key: "mobile", width: 390, height: 844, isMobile: true, hasTouch: true }),
]);
export const LOCAL_WORK_PATH = "/works/test-001-00000002";

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

export function validateDisplayURL(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new SmokeFailure("--display-url 必须是完整的 HTTP(S) origin");
  }
  if (url.username || url.password) throw new SmokeFailure("--display-url 不能包含用户名或密码");
  if (url.search || url.hash || (url.pathname !== "/" && url.pathname !== "")) {
    throw new SmokeFailure("--display-url 只能提供 origin，不能包含路径、查询或片段");
  }
  const loopback = new Set(["127.0.0.1", "localhost", "[::1]"]).has(url.hostname);
  if (url.protocol !== "https:" && !(url.protocol === "http:" && loopback)) {
    throw new SmokeFailure("远程 --display-url 必须使用 HTTPS；HTTP 只允许回环地址");
  }
  return url.origin;
}

function validateWorkPath(value) {
  if (!value.startsWith("/works/") || value.includes("?") || value.includes("#") || value.includes("\\") || value.includes("..")) {
    throw new SmokeFailure("--work-path 必须是站内 /works/<slug> 路径且不能包含查询、片段或路径穿越");
  }
  return value;
}

export function parsePerformanceArgs(argv, now = new Date()) {
  const options = {
    browserChannel: "chrome",
    build: false,
    date: shanghaiDate(now),
    displayURL: null,
    help: false,
    output: null,
    reportOnly: false,
    samples: 3,
    timeoutMs: DEFAULT_TIMEOUT_MS,
    workPath: null,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") options.build = true;
    else if (argument === "--report-only") options.reportOnly = true;
    else if (argument === "--browser-channel") {
      const channel = argumentValue(argv, index, argument);
      if (!new Set(["chrome", "chromium"]).has(channel)) throw new SmokeFailure("--browser-channel 只允许 chrome 或 chromium");
      options.browserChannel = channel;
      index += 1;
    } else if (argument === "--date") {
      options.date = validateDate(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--display-url") {
      options.displayURL = validateDisplayURL(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--output") {
      options.output = argumentValue(argv, index, argument);
      index += 1;
    } else if (argument === "--samples") {
      options.samples = parseInteger(argumentValue(argv, index, argument), argument, 1, 7);
      index += 1;
    } else if (argument === "--timeout") {
      options.timeoutMs = parseInteger(argumentValue(argv, index, argument), argument, 5_000, 180_000);
      index += 1;
    } else if (argument === "--work-path") {
      options.workPath = validateWorkPath(argumentValue(argv, index, argument));
      index += 1;
    } else if (argument === "--help" || argument === "-h") options.help = true;
    else throw new SmokeFailure(`未知参数：${argument}`);
  }
  if (options.displayURL && !options.workPath) throw new SmokeFailure("远程性能探针必须显式提供 --work-path");
  if (options.displayURL && options.build) throw new SmokeFailure("--build 只适用于本地合成模式，不能与 --display-url 同时使用");
  if (!options.workPath) options.workPath = LOCAL_WORK_PATH;
  return options;
}

export function performanceHelp() {
  return [
    "用法：node scripts/release_a_performance_probe.mjs [选项]",
    "",
    "预热后使用 Chrome/Chromium 采样公开首页与作品详情的 LCP、CLS、TTFB 和资源字节。",
    "默认启动随机回环端口的本地合成环境；提供 --display-url 后改为探测最终 HTTPS 站点。",
    "",
    "--build                       本地探测前重新构建两套 Next.js 应用",
    "--browser-channel <名称>      chrome（默认）或已安装的 Playwright chromium",
    "--date <YYYY-MM-DD>           证据日期，默认使用 Asia/Shanghai 当天",
    "--display-url <HTTPS origin>  最终公开站 origin；远程模式必须使用 HTTPS",
    "--work-path </works/slug>     远程模式必填；本地默认 TEST-001 详情",
    "--samples <1-7>               每个页面和视口的预热后采样数，默认 3",
    "--timeout <毫秒>              单次页面等待上限，默认 45000",
    "--output <JSON 文件>          必须是 docs/evidence 下的直接 JSON 文件",
    "--report-only                 即使性能阈值未通过也返回成功，只保留报告",
  ].join("\n");
}

export function resolvePerformanceOutput(options, repositoryRoot = REPOSITORY_ROOT) {
  const evidenceRoot = path.resolve(repositoryRoot, "docs", "evidence");
  const defaultName = options.displayURL
    ? `release-a-performance-target-${options.date}.json`
    : `release-a-performance-${options.date}.json`;
  const output = options.output
    ? path.resolve(repositoryRoot, options.output)
    : path.join(evidenceRoot, defaultName);
  const relative = path.relative(evidenceRoot, output);
  const segments = relative.split(path.sep).filter(Boolean);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative) || segments.length !== 1 || path.extname(output) !== ".json") {
    throw new SmokeFailure("性能证据必须写入 docs/evidence 下的直接 .json 文件");
  }
  return { evidenceRoot, output };
}

async function assertOutputFileSafe(output) {
  try {
    const stat = await fs.lstat(output);
    if (stat.isSymbolicLink()) throw new SmokeFailure("性能证据输出文件不能是符号链接");
    if (!stat.isFile()) throw new SmokeFailure("性能证据输出路径已存在但不是文件");
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
}

export function median(values) {
  if (!values.length) return null;
  const sorted = [...values].sort((left, right) => left - right);
  const middle = Math.floor(sorted.length / 2);
  return sorted.length % 2 ? sorted[middle] : (sorted[middle - 1] + sorted[middle]) / 2;
}

function rounded(value, digits = 2) {
  if (!Number.isFinite(value)) return null;
  const factor = 10 ** digits;
  return Math.round(value * factor) / factor;
}

export function summarizeSamples(samples) {
  const metric = (key) => samples.map((sample) => sample[key]).filter(Number.isFinite);
  const summary = {};
  const precisions = { cls: 5, transfer_bytes: 0, encoded_body_bytes: 0, resource_count: 0 };
  for (const key of ["lcp_ms", "cls", "fcp_ms", "ttfb_ms", "dom_content_loaded_ms", "load_ms", "transfer_bytes", "encoded_body_bytes", "resource_count"]) {
    const values = metric(key);
    const digits = precisions[key] ?? 2;
    summary[key] = {
      median: rounded(median(values), digits),
      min: values.length ? rounded(Math.min(...values), digits) : null,
      max: values.length ? rounded(Math.max(...values), digits) : null,
    };
  }
  return summary;
}

export function performanceGates(result, thresholds = PERFORMANCE_THRESHOLDS) {
  const failures = [];
  const lcp = result.summary.lcp_ms.median;
  if (!(lcp > 0)) failures.push("LCP 未被浏览器 PerformanceObserver 捕获");
  else if (lcp >= thresholds.lcpMs) failures.push(`LCP 中位数 ${lcp}ms 未低于 ${thresholds.lcpMs}ms`);
  if (result.viewport.key === "mobile") {
    const cls = result.summary.cls.max;
    if (!(cls >= 0)) failures.push("CLS 未被浏览器 PerformanceObserver 捕获");
    else if (cls >= thresholds.mobileCLS) failures.push(`移动端 CLS 最大值 ${cls} 未低于 ${thresholds.mobileCLS}`);
  }
  return failures;
}

function installPerformanceObservers() {
  window.__releaseAPerformance = { cls: 0, lcp: 0 };
  try {
    new PerformanceObserver((list) => {
      const entries = list.getEntries();
      const latest = entries[entries.length - 1];
      if (latest) window.__releaseAPerformance.lcp = latest.startTime;
    }).observe({ type: "largest-contentful-paint", buffered: true });
  } catch {
    // A missing metric is reported by the gate instead of breaking page execution.
  }
  try {
    new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (!entry.hadRecentInput) window.__releaseAPerformance.cls += entry.value;
      }
    }).observe({ type: "layout-shift", buffered: true });
  } catch {
    // A missing metric is reported by the gate instead of breaking page execution.
  }
}

export function shouldDrainRequest(request, expectedOrigin) {
  if (!new Set(["document", "fetch", "xhr"]).has(request.resourceType())) return false;
  try {
    return new URL(request.url()).origin === expectedOrigin;
  } catch {
    return false;
  }
}

export function createSameOriginRequestDrain(page, expectedOrigin) {
  const pending = new Set();
  const started = (request) => {
    if (shouldDrainRequest(request, expectedOrigin)) pending.add(request);
  };
  const settled = (request) => pending.delete(request);
  page.on("request", started);
  page.on("requestfinished", settled);
  page.on("requestfailed", settled);

  return {
    async wait({ timeoutMs = REQUEST_DRAIN.timeoutMs, quietMs = REQUEST_DRAIN.quietMs, pollMs = REQUEST_DRAIN.pollMs } = {}) {
      const deadline = Date.now() + timeoutMs;
      let quietSince = pending.size === 0 ? Date.now() : null;
      while (Date.now() < deadline) {
        if (pending.size === 0) {
          if (quietSince === null) quietSince = Date.now();
          if (Date.now() - quietSince >= quietMs) return true;
        } else {
          quietSince = null;
        }
        await new Promise((resolve) => setTimeout(resolve, Math.min(pollMs, Math.max(1, deadline - Date.now()))));
      }
      return pending.size === 0 && quietSince !== null && Date.now() - quietSince >= quietMs;
    },
    dispose() {
      page.off("request", started);
      page.off("requestfinished", settled);
      page.off("requestfailed", settled);
      pending.clear();
    },
    pendingCount() {
      return pending.size;
    },
  };
}

async function navigateForMeasurement(page, url, expectedOrigin, timeoutMs) {
  const response = await page.goto(url, { waitUntil: "load", timeout: timeoutMs });
  if (!response || !response.ok()) throw new SmokeFailure(`${url} 性能导航失败：HTTP ${response?.status() ?? "no-response"}`);
  if (new URL(page.url()).origin !== expectedOrigin) throw new SmokeFailure(`${url} 重定向到了其他 origin，性能探针已停止`);
  await page.locator("main").first().waitFor({ state: "visible", timeout: timeoutMs });
  await page.evaluate(async (imageTimeoutMs) => {
    await document.fonts?.ready;
    const visibleImages = Array.from(document.images).filter((image) => {
      const rect = image.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0
        && rect.bottom > 0 && rect.right > 0
        && rect.top < window.innerHeight && rect.left < window.innerWidth;
    });
    const imagesReady = Promise.all(visibleImages.map((image) => {
      if (image.complete) return Promise.resolve();
      return new Promise((resolve) => {
        image.addEventListener("load", resolve, { once: true });
        image.addEventListener("error", resolve, { once: true });
      });
    }));
    await Promise.race([imagesReady, new Promise((resolve) => setTimeout(resolve, imageTimeoutMs))]);
  }, Math.min(timeoutMs, 3_000));
  await page.waitForTimeout(750);
}

async function readPerformanceSample(page, index) {
  return page.evaluate((sampleIndex) => {
    const navigation = performance.getEntriesByType("navigation")[0];
    const resources = performance.getEntriesByType("resource");
    const paint = performance.getEntriesByName("first-contentful-paint")[0];
    const state = window.__releaseAPerformance ?? { cls: null, lcp: null };
    return {
      sample: sampleIndex,
      lcp_ms: state.lcp,
      cls: state.cls,
      fcp_ms: paint?.startTime ?? null,
      ttfb_ms: navigation?.responseStart ?? null,
      dom_content_loaded_ms: navigation?.domContentLoadedEventEnd ?? null,
      load_ms: navigation?.loadEventEnd ?? null,
      resource_count: resources.length,
      transfer_bytes: resources.reduce((total, entry) => total + (entry.transferSize || 0), 0),
      encoded_body_bytes: resources.reduce((total, entry) => total + (entry.encodedBodySize || 0), 0),
    };
  }, index);
}

async function measureScenario(browser, origin, route, viewport, sampleCount, timeoutMs) {
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
  await context.addInitScript(installPerformanceObservers);
  const url = new URL(route.path, origin).href;
  const pageErrors = [];
  try {
    const warmup = await context.newPage();
    const warmupDrain = createSameOriginRequestDrain(warmup, origin);
    warmup.on("pageerror", (error) => pageErrors.push(error.message));
    try {
      await navigateForMeasurement(warmup, url, origin, timeoutMs);
      await warmupDrain.wait({ timeoutMs: Math.min(timeoutMs, REQUEST_DRAIN.timeoutMs) });
    } finally {
      warmupDrain.dispose();
      await warmup.close();
    }
    const samples = [];
    for (let index = 1; index <= sampleCount; index += 1) {
      const page = await context.newPage();
      const requestDrain = createSameOriginRequestDrain(page, origin);
      page.on("pageerror", (error) => pageErrors.push(error.message));
      try {
        await navigateForMeasurement(page, url, origin, timeoutMs);
        samples.push(await readPerformanceSample(page, index));
        await requestDrain.wait({ timeoutMs: Math.min(timeoutMs, REQUEST_DRAIN.timeoutMs) });
      } finally {
        requestDrain.dispose();
        await page.close();
      }
    }
    if (pageErrors.length) throw new SmokeFailure(`${route.path} 出现浏览器脚本异常：${pageErrors.join(" | ")}`);
    const result = { route, viewport, samples, summary: summarizeSamples(samples) };
    result.failures = performanceGates(result);
    result.status = result.failures.length ? "failed" : "passed";
    return result;
  } finally {
    await context.close();
  }
}

export function performanceReport({ date, generatedAt, mode, origin, options, results }) {
  const failures = results.flatMap((result) => result.failures.map((failure) => `${result.route.key}/${result.viewport.key}: ${failure}`));
  return {
    schema_version: 1,
    release: "A",
    status: failures.length ? "failed" : "passed",
    generated_at: generatedAt,
    timezone: "Asia/Shanghai",
    evidence_date: date,
    mode,
    target_origin: mode === "remote-target" ? origin : "local-random-loopback",
    browser_channel: options.browserChannel,
    samples_per_route_after_warmup: options.samples,
    thresholds: {
      lcp_ms_strictly_less_than: PERFORMANCE_THRESHOLDS.lcpMs,
      mobile_cls_strictly_less_than: PERFORMANCE_THRESHOLDS.mobileCLS,
    },
    measurement: {
      network_throttling: "none",
      cache_state: "one warm-up navigation in the same browser context before samples",
      statistic: "median LCP; maximum mobile CLS",
      field_data: false,
      post_capture_cleanup: "after metrics are captured, wait up to 2s for same-origin document/fetch/xhr streams to settle before closing the page; cleanup time is excluded from reported metrics",
    },
    synthetic_boundary: mode === "local-synthetic"
      ? "Next.js standalone build with memory-only synthetic API; no Docker/PostgreSQL/SMTP/S3/R2/Cloudflare"
      : "Browser lab measurement of the supplied public origin; not real-user field data and not a substitute for API/resource monitoring",
    failures,
    results,
  };
}

async function writePerformanceReport(report, options, repositoryRoot) {
  const { evidenceRoot, output } = resolvePerformanceOutput(options, repositoryRoot);
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

async function measureOrigin(browser, origin, options) {
  const routes = [
    { key: "display-home", path: "/" },
    { key: "work-detail", path: options.workPath },
  ];
  const results = [];
  for (const viewport of PERFORMANCE_VIEWPORTS) {
    for (const route of routes) {
      results.push(await measureScenario(browser, origin, route, viewport, options.samples, options.timeoutMs));
    }
  }
  return results;
}

async function runWithBrowser(origin, mode, options, repositoryRoot) {
  let browser;
  try {
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
    const results = await measureOrigin(browser, origin, options);
    const report = performanceReport({
      date: options.date,
      generatedAt: new Date().toISOString(),
      mode,
      origin,
      options,
      results,
    });
    const output = await writePerformanceReport(report, options, repositoryRoot);
    return { output, report };
  } finally {
    if (browser) await browser.close();
  }
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
      name: "display-web-performance-probe",
      server: paths.display.server,
      port: displayPort,
      apiURL: `http://127.0.0.1:${apiPort}`,
      repositoryRoot,
      stdio: "inherit",
    });
    await waitForHTTP(display, "/", options.timeoutMs);
    return await runWithBrowser(`http://127.0.0.1:${displayPort}`, "local-synthetic", options, repositoryRoot);
  } finally {
    await stopChild(display);
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(() => resolve()));
    }
    await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
  }
}

export async function runPerformanceProbe(options, repositoryRoot = REPOSITORY_ROOT) {
  resolvePerformanceOutput(options, repositoryRoot);
  return options.displayURL
    ? runWithBrowser(options.displayURL, "remote-target", options, repositoryRoot)
    : runLocalProbe(options, repositoryRoot);
}

export async function main(argv = process.argv.slice(2)) {
  try {
    const options = parsePerformanceArgs(argv);
    if (options.help) {
      console.log(performanceHelp());
      return 0;
    }
    const result = await runPerformanceProbe(options);
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
