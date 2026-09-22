#!/usr/bin/env node

import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import path from "node:path";
import process from "node:process";
import { pathToFileURL } from "node:url";
import { chromium, expect } from "@playwright/test";
import { createIsolatedStandalone, removeIsolatedRuntime } from "./release_a_cache_contract.mjs";
import { findFreePort, REPOSITORY_ROOT, standalonePaths, startChild, startMockUpstream, stopChild, waitForHTTP } from "./release_a_frontend_smoke.mjs";

const DEFAULT_OUTPUT = "docs/evidence/frontend-optimization-2026-09-22";
const WIDTHS = [320, 390, 768, 1440];

function optionsFrom(argv) {
  const options = { output: DEFAULT_OUTPUT, channel: "chrome", help: false };
  for (let index = 0; index < argv.length; index += 1) {
    if (argv[index] === "--help" || argv[index] === "-h") options.help = true;
    else if (argv[index] === "--output" && argv[index + 1] && !argv[index + 1].startsWith("--")) options.output = argv[++index];
    else if (argv[index] === "--browser-channel" && argv[index + 1] && !argv[index + 1].startsWith("--")) options.channel = argv[++index];
    else throw new Error(`Unknown or incomplete option: ${argv[index]}`);
  }
  return options;
}

function outputDirectory(value) {
  const target = path.resolve(REPOSITORY_ROOT, value);
  const root = path.join(REPOSITORY_ROOT, "docs", "evidence");
  const relative = path.relative(root, target);
  if (!relative || relative.startsWith(`..${path.sep}`) || relative === ".." || path.isAbsolute(relative)) {
    throw new Error("Browser evidence must use a subdirectory of docs/evidence");
  }
  return target;
}

async function assertFitsViewport(page, label) {
  const geometry = await page.evaluate(() => {
    const width = window.innerWidth;
    return {
      viewport: width,
      scroll_width: document.documentElement.scrollWidth,
      overflowing: [...document.querySelectorAll("main *")].filter((element) => {
        const bounds = element.getBoundingClientRect();
        return bounds.width > 0 && (bounds.right > width + 1 || bounds.left < -1);
      }).slice(0, 8).map((element) => ({ tag: element.tagName, class: String(element.className).slice(0, 120) })),
    };
  });
  assert.ok(geometry.scroll_width <= geometry.viewport + 1, `${label}: horizontal overflow ${JSON.stringify(geometry)}`);
  return { viewport: geometry.viewport, scroll_width: geometry.scroll_width };
}

export async function runPublicOptimization(options) {
  const output = outputDirectory(options.output);
  await fs.mkdir(output, { recursive: true });
  const report = {
    schema_version: 1,
    status: "running",
    checked_at: new Date().toISOString(),
    mode: "isolated-standalone-with-synthetic-api",
    boundary: "Production Next.js build and Chromium with an in-memory synthetic API on random loopback ports; no real collector records, production API, database, external media or existing preview service.",
    widths: WIDTHS,
    checks: [],
    screenshots: [],
    failures: [],
  };
  let runtime;
  let upstream;
  let display;
  let browser;
  let page;
  let stage = "startup";
  const pageErrors = [];
  const externalRequests = [];
  const documentRequests = [];
  const check = async (name, callback) => {
    stage = name;
    const details = await callback();
    report.checks.push({ name, status: "passed", ...(details ? { details } : {}) });
    console.log(`PASS ${name}`);
  };
  const screenshot = async (name) => {
    await page.screenshot({ path: path.join(output, name), fullPage: true, animations: "disabled" });
    report.screenshots.push(name);
  };
  try {
    const source = standalonePaths("display-web");
    await Promise.all([fs.access(source.server), fs.access(source.sourceStatic)]);
    runtime = await createIsolatedStandalone(source);
    const displayPort = await findFreePort();
    const apiPort = await findFreePort({ avoid: new Set([displayPort]) });
    const origin = `http://127.0.0.1:${displayPort}`;
    upstream = await startMockUpstream(apiPort, { accountMode: "stateful" });
    display = startChild({
      name: "public-optimization-browser", server: runtime.server, port: displayPort,
      apiURL: `http://127.0.0.1:${apiPort}`, repositoryRoot: REPOSITORY_ROOT,
      extraEnv: {
        APP_ENV: "test", SITE_URL: origin, TURNSTILE_BYPASS: "true", RIGHTS_CONTACT_EMAIL: "rights@example.test",
        DISPLAY_ADS_ENABLED: "false", DISPLAY_AD_SOURCE: "env", MEDIA_DELIVERY_MODE: "normal", VISITOR_METRICS_ENABLED: "false",
      },
    });
    await waitForHTTP(display, "/", 45_000);
    report.build_id = (await fs.readFile(path.join(source.appRoot, ".next", "BUILD_ID"), "utf8")).trim();
    browser = await chromium.launch({ ...(options.channel === "bundled" ? {} : { channel: options.channel }), headless: true });
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, colorScheme: "light", reducedMotion: "reduce" });
    await context.route("**/*", (route) => {
      const url = new URL(route.request().url());
      if (url.origin === origin) return route.continue();
      externalRequests.push(url.origin);
      return route.abort();
    });
    page = await context.newPage();
    page.setDefaultTimeout(15_000);
    page.on("pageerror", (error) => pageErrors.push(error.message));
    page.on("request", (request) => {
      if (request.resourceType() !== "document") return;
      const url = new URL(request.url());
      documentRequests.push({ method: request.method(), path: url.pathname, query: url.search });
    });

    for (const fixture of [
      { type: "work", label: "作品", query: "TEST-001", result: "示例作品资料：春日档案", href: "/works/test-001-00000002" },
      { type: "performer", label: "人物", query: "测试别名", result: "测试人物", href: "/performers/test-jsonld-00000001" },
      { type: "studio", label: "厂牌", query: "测试厂牌", result: "测试厂牌", href: "/studios/test-jsonld-00000001" },
    ]) {
      await check(`home_${fixture.type}_get_search`, async () => {
        await page.goto(origin);
        await page.getByRole("radio", { name: fixture.label, exact: true }).check();
        await page.locator("#primary-query").fill(fixture.query);
        await Promise.all([
          page.waitForURL((url) => url.pathname === "/search" && url.searchParams.get("q") === fixture.query && url.searchParams.get("type") === fixture.type),
          page.locator("main").getByRole("button", { name: "查询", exact: true }).click(),
        ]);
        await expect(page.locator("#result-query")).toHaveValue(fixture.query);
        await expect(page.getByRole("navigation", { name: "搜索类型" }).getByRole("link", { name: fixture.label, exact: true })).toHaveAttribute("aria-current", "page");
        await expect(page.locator(`main a[href="${fixture.href}"]`).filter({ hasText: fixture.result }).first()).toBeVisible();
        const request = documentRequests.at(-1);
        assert.equal(request.method, "GET");
        assert.equal(request.path, "/search");
        return { query: fixture.query, type: fixture.type, method: request.method };
      });
    }

    await check("submitted_query_type_switch_and_history_preserved", async () => {
      const query = "测试别名";
      await page.goto(`${origin}/search?q=${encodeURIComponent(query)}&type=performer`);
      for (const [label, type] of [["厂牌", "studio"], ["作品", "work"], ["人物", "performer"]]) {
        await page.getByRole("navigation", { name: "搜索类型" }).getByRole("link", { name: label, exact: true }).click();
        await expect(page.locator("#result-query")).toHaveValue(query);
        await expect(page).toHaveURL((url) => url.searchParams.get("q") === query && (url.searchParams.get("type") || "work") === type);
      }
      await page.locator("#result-query").fill("Test Person");
      await page.locator("main").getByRole("button", { name: "查询", exact: true }).click();
      await expect(page).toHaveURL((url) => url.searchParams.get("q") === "Test Person" && url.searchParams.get("type") === "performer");
      await expect(page.locator("main").getByRole("link", { name: "测试人物", exact: true })).toBeVisible();
      await page.getByRole("navigation", { name: "搜索类型" }).getByRole("link", { name: "厂牌", exact: true }).click();
      await expect(page).toHaveURL((url) => url.searchParams.get("q") === "Test Person" && url.searchParams.get("type") === "studio");
      await expect(page.locator("#result-query")).toHaveValue("Test Person");
      await page.goBack();
      await expect(page).toHaveURL((url) => url.searchParams.get("q") === "Test Person" && url.searchParams.get("type") === "performer");
      await expect(page.locator("#result-query")).toHaveValue("Test Person");
      return { scope: "submitted keyword and entity type; browser back returns to the same submitted query" };
    });

    for (const width of WIDTHS) {
      await check(`public_layout_${width}`, async () => {
        await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 });
        await page.goto(origin);
        await expect(page.locator("#primary-query")).toBeVisible();
        const home = await assertFitsViewport(page, `home ${width}`);
        await screenshot(`public-home-${width}.png`);
        await page.goto(`${origin}/search?q=${encodeURIComponent("测试别名")}&type=performer`);
        await expect(page.locator("#result-query")).toHaveValue("测试别名");
        await expect(page.locator("main").getByRole("link", { name: "测试人物", exact: true })).toBeVisible();
        const search = await assertFitsViewport(page, `search ${width}`);
        const heading = await page.getByRole("heading", { level: 1 }).boundingBox();
        const input = await page.locator("#result-query").boundingBox();
        assert.ok(heading && input);
        if (width === 1440) assert.ok(input.x > heading.x + heading.width, "desktop query panel follows the title in a separate column");
        else assert.ok(input.y > heading.y + heading.height, "narrow query panel follows the title vertically");
        for (const tab of await page.getByRole("navigation", { name: "搜索类型" }).getByRole("link").all()) {
          const bounds = await tab.boundingBox();
          assert.ok(bounds && bounds.height >= 44, "search type targets remain at least 44px high");
        }
        await screenshot(`public-search-${width}.png`);
        return { home, search };
      });
    }

    await check("latest_unavailable_is_503_retry_preserves_cursor", async () => {
      await page.setViewportSize({ width: 1440, height: 1000 });
      const response = await page.goto(`${origin}/latest?cursor=unavailable&returnTo=https%3A%2F%2Foutside.example%2F`);
      assert.equal(response.status(), 503);
      assert.match(response.headers()["cache-control"], /no-store/);
      assert.equal(response.headers()["retry-after"], "60");
      assert.match(response.headers()["x-robots-tag"], /noindex/);
      await expect(page.getByRole("heading", { name: "资料暂时无法加载", exact: true })).toBeVisible();
      await expect(page.locator("main")).not.toContainText("暂无已发布作品");
      await expect(page.locator("main")).not.toContainText("测试内容");
      await expect(page.locator("body")).not.toContainText("outside.example");
      await expect(page.locator('meta[name="viewport"]')).toHaveAttribute("content", "width=device-width, initial-scale=1");
      const form = page.locator("form");
      await expect(form).toHaveAttribute("method", "get");
      await expect(form).toHaveAttribute("action", "/latest");
      await expect(form.locator("input")).toHaveCount(1);
      await expect(form.locator('input[name="cursor"]')).toHaveValue("unavailable");
      const requestsBefore = documentRequests.length;
      const [retryResponse] = await Promise.all([page.waitForNavigation(), page.getByRole("button", { name: "重试加载", exact: true }).click()]);
      assert.equal(retryResponse.status(), 503);
      await expect(page).toHaveURL(`${origin}/latest?cursor=unavailable`);
      assert.ok(documentRequests.length > requestsBefore, "retry performs a fresh document request");
      assert.equal(documentRequests.at(-1).method, "GET");
      for (const width of WIDTHS) {
        await page.setViewportSize({ width, height: width <= 390 ? 844 : 1000 });
        await assertFitsViewport(page, `latest failure ${width}`);
        await screenshot(`public-latest-unavailable-${width}.png`);
      }
      await expect(page.getByRole("link", { name: "返回目录首页", exact: true })).toHaveAttribute("href", "/latest");
      await page.getByRole("link", { name: "返回目录首页", exact: true }).click();
      await expect(page).toHaveURL(`${origin}/latest`);
      await expect(page.locator("main")).toContainText("TEST-JSONLD");
      return { status: 503, retry_method: "GET", retry_path: "/latest?cursor=unavailable", unrelated_parameters_dropped: true };
    });

    await check("invalid_cursor_is_400_and_returns_to_directory", async () => {
      const response = await page.goto(`${origin}/latest?cursor=broken`);
      assert.equal(response.status(), 400);
      assert.match(response.headers()["cache-control"], /no-store/);
      await expect(page.getByRole("heading", { name: "分页参数无效", exact: true })).toBeVisible();
      await expect(page.getByRole("link", { name: "返回目录首页", exact: true })).toHaveAttribute("href", "/latest");
      await expect(page.locator("form")).toHaveCount(0);
      await screenshot("public-latest-invalid-cursor.png");
      await page.getByRole("link", { name: "返回目录首页", exact: true }).click();
      await expect(page).toHaveURL(`${origin}/latest`);
      return { status: 400, return_path: "/latest" };
    });

    await check("browser_remains_local_and_has_no_runtime_errors", async () => {
      assert.deepEqual(externalRequests, []);
      assert.deepEqual(pageErrors, []);
      return { external_requests: 0, page_errors: 0 };
    });
    report.status = "passed";
    await fs.rm(path.join(output, "public-failure.png"), { force: true });
  } catch (error) {
    report.status = "failed";
    report.failures.push({ stage, message: String(error.message ?? error).slice(0, 2000) });
    if (page) await screenshot("public-failure.png").catch(() => {});
  } finally {
    await browser?.close().catch(() => {});
    await stopChild(display);
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(resolve));
    }
    if (runtime) await removeIsolatedRuntime(runtime.runtimeRoot);
    report.finished_at = new Date().toISOString();
    await fs.writeFile(path.join(output, "public-browser-report.json"), JSON.stringify(report, null, 2) + "\n");
    console.log(JSON.stringify(report));
  }
  return report.status === "passed" ? 0 : 1;
}

export async function main(argv = process.argv.slice(2)) {
  const options = optionsFrom(argv);
  if (options.help) {
    console.log("Usage: node scripts/test_frontend_public_optimization.mjs [--output docs/evidence/<directory>] [--browser-channel chrome|bundled]\nUses the existing display-web build in an isolated runtime with synthetic API data; never builds or touches running previews.");
    return 0;
  }
  return runPublicOptimization(options);
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) process.exitCode = await main();
