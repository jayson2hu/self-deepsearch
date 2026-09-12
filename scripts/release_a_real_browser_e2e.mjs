#!/usr/bin/env node
// Real Release A browser flow. The Python parent owns the disposable database,
// Mailpit, API and worker; this process owns only its two Next copies and browsers.
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium, expect } from "@playwright/test";
import { standalonePaths, stopChild } from "./release_a_frontend_smoke.mjs";

export const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const STAGE_TIMEOUT = 60_000;
const SAFE_ENVIRONMENT_KEYS = new Set(["PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG", "LC_ALL", "PLAYWRIGHT_BROWSERS_PATH"]);
const SAFE_FAILURE_CODES = new Set(["invitation_create_http_status", "new_work_create_http_status", "new_work_form_error_after_success"]);
const CHECKS = [
  "real_services_ready", "production_frontends_ready", "owner_browser_login",
  "owner_ui_invitation_mailpit", "editor_browser_accept_invitation", "editor_ui_create_with_sources",
  "author_review_separation", "owner_ui_claim_approve", "owner_ui_publish",
  "worker_publication_cache_invalidation", "anonymous_desktop_mobile_search_detail",
  "owner_ui_hide", "worker_hidden_cache_invalidation", "anonymous_desktop_mobile_hidden",
  "browser_loopback_only",
];

export function loopbackOrigin(value) {
  const url = new URL(value);
  if (url.protocol !== "http:" || url.hostname !== "127.0.0.1" || !url.port || Number(url.port) < 1 ||
      url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
    throw new TypeError("explicit loopback HTTP origin required");
  }
  return url.origin;
}

export function validateEnvironment(environment) {
  if (environment.CONFIRM_RELEASE_A_BROWSER_E2E !== "disposable-database") {
    throw new TypeError("disposable database confirmation required");
  }
  const config = {};
  for (const [key, name] of Object.entries({ api: "API", display: "DISPLAY", ops: "OPS", mailpit: "MAILPIT", worker: "WORKER" })) {
    config[key] = loopbackOrigin(environment[`REAL_BROWSER_${name}_URL`]);
  }
  if (new Set([config.api, config.display, config.ops, config.mailpit, config.worker]).size !== 5) {
    throw new TypeError("independent service origins required");
  }
  config.runID = environment.REAL_BROWSER_RUN_ID;
  config.version = environment.REAL_BROWSER_BUILD_VERSION;
  config.ownerEmail = environment.REAL_BROWSER_OWNER_EMAIL;
  config.ownerPassword = environment.REAL_BROWSER_OWNER_PASSWORD;
  config.cacheSecret = environment.CACHE_HMAC_SECRET;
  if (!environment.REAL_BROWSER_PROCESS_REGISTRY) throw new TypeError("parent-owned process registry required");
  config.processRegistry = evidencePath(environment.REAL_BROWSER_PROCESS_REGISTRY);
  if (typeof config.runID !== "string" || !/^[a-f0-9]{16,32}$/.test(config.runID) ||
      typeof config.version !== "string" || !/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$/.test(config.version) ||
      typeof config.ownerEmail !== "string" || !/^[a-zA-Z0-9._+-]{1,100}@example\.test$/.test(config.ownerEmail) ||
      typeof config.ownerPassword !== "string" || config.ownerPassword.length < 12 || config.ownerPassword.length > 128 ||
      typeof config.cacheSecret !== "string" || config.cacheSecret.trim().length < 32 || config.cacheSecret.length > 4096) {
    throw new TypeError("synthetic account, run identity and private cache key required");
  }
  return config;
}

export function inheritedEnvironment(environment) {
  return {
    ...Object.fromEntries(Object.entries(environment).filter(([key]) => SAFE_ENVIRONMENT_KEYS.has(key.toUpperCase()))),
    NO_PROXY: "*", no_proxy: "*",
  };
}

export function frontendEnvironment(config, appName, environment = process.env) {
  if (!["display-web", "ops-web"].includes(appName)) throw new TypeError("unknown frontend");
  return {
    ...inheritedEnvironment(environment), NODE_ENV: "production", APP_ENV: "test",
    NEXT_TELEMETRY_DISABLED: "1", HOSTNAME: "127.0.0.1",
    PORT: new URL(appName === "display-web" ? config.display : config.ops).port,
    PLATFORM_API_URL: config.api, SITE_URL: config.display, TURNSTILE_BYPASS: "true",
    ...(appName === "display-web" ? { CACHE_HMAC_SECRET: config.cacheSecret } : {}),
  };
}

export function parseArgs(argv) {
  const result = { output: "docs/evidence/release-a-real-browser-e2e.json", help: false };
  let hasOutput = false;
  for (let index = 0; index < argv.length; index += 1) {
    if (argv[index] === "--help") result.help = true;
    else if (argv[index] === "--output" && !hasOutput && argv[index + 1] && !argv[index + 1].startsWith("--")) {
      result.output = argv[++index];
      hasOutput = true;
    }
    else throw new TypeError("unsupported or incomplete arguments");
  }
  return result;
}

export function evidencePath(value, root = ROOT, temporaryRoot = os.tmpdir()) {
  const target = path.resolve(root, value);
  const within = (base) => {
    const relative = path.relative(path.resolve(base), target);
    return relative && !relative.startsWith(`..${path.sep}`) && relative !== ".." && !path.isAbsolute(relative);
  };
  if (path.extname(target) !== ".json" || ![path.join(root, "docs", "evidence"), path.join(root, ".cache"), temporaryRoot].some(within)) {
    throw new TypeError("JSON evidence must stay in the evidence, cache or temporary directory");
  }
  return target;
}

export function newReport(now = new Date()) {
  return {
    schema_version: 1, generated_at: now.toISOString(), status: "failed", stage: "configuration",
    scope: "real-go-api-worker-postgresql-mailpit-two-production-frontends-chromium",
    browser_profiles: { owner: "desktop", editor: "mobile", anonymous: ["desktop", "mobile"] },
    checks: Object.fromEntries(CHECKS.map((name) => [name, "not_run"])),
    cache: {
      ttl_seconds: 300, observation_timeout_seconds: STAGE_TIMEOUT / 1000, manual_revalidation_requests: 0,
      cached_surfaces: ["home", "detail", "sitemap"], uncached_visibility_surfaces: ["search"],
    },
    not_covered: ["turnstile_provider", "s3_r2", "cloudflare", "target_servers", "automated_collection"],
  };
}

export function markFailure(report, error) {
  // Never copy error.message/stack, HTTP bodies, dialogs, URLs, cookies, mail,
  // account addresses or Playwright diagnostics into persisted evidence.
  report.status = "failed";
  report.error_type = ["AssertionError", "TimeoutError", "TypeError", "AbortError"].includes(error?.name) ? error.name : "Error";
  if (SAFE_FAILURE_CODES.has(error?.failureCode)) report.failure_code = error.failureCode;
}

function requireContract(condition, failureCode) {
  if (!condition) {
    const error = new Error("real browser contract failed");
    error.failureCode = failureCode;
    throw error;
  }
}

export function namedSelect(page, name) {
  if (!["role", "source_type"].includes(name)) throw new TypeError("unknown form select");
  // A wrapping label includes its option text in Playwright's label matcher;
  // exact getByLabel("角色") therefore does not identify the real select.
  return page.locator(`select[name="${name}"]`);
}

export function mailpitMessages(value) {
  const items = Array.isArray(value) ? value : value?.messages ?? value?.Messages;
  if (!Array.isArray(items)) throw new TypeError("unsupported Mailpit listing");
  return items.filter((item) => item && typeof item === "object" && !Array.isArray(item));
}

export function messageID(value) {
  return [value.ID, value.Id, value.id].find((id) => typeof id === "string" && /^[a-zA-Z0-9_-]{1,100}$/.test(id)) ?? "";
}

export function nestedStrings(value) {
  if (typeof value === "string") return [value];
  return value && typeof value === "object" ? Object.values(value).flatMap(nestedStrings) : [];
}

export function extractInvitationCode(value) {
  for (const text of nestedStrings(value)) {
    const match = /验证码\s*[：:]\s*(\d{6})(?!\d)/u.exec(text);
    if (match) return match[1];
  }
  return null;
}

export async function createFrontendRuntime(appName, repositoryRoot = ROOT) {
  if (!["display-web", "ops-web"].includes(appName)) throw new TypeError("unknown frontend");
  const source = standalonePaths(appName, repositoryRoot);
  for (const required of [source.server, source.sourceStatic, path.join(source.appRoot, ".next", "BUILD_ID")]) await fs.access(required);
  const cacheRoot = path.join(repositoryRoot, ".cache");
  await fs.mkdir(cacheRoot, { recursive: true });
  const runtimeRoot = await fs.mkdtemp(path.join(cacheRoot, "real-browser-e2e-"));
  const target = standalonePaths(appName, runtimeRoot);
  try {
    // Independent writable files: ISR must never alter the source build or
    // resurrect the synthetic API fetch cache used by the separate 82-test suite.
    await fs.cp(path.join(source.appRoot, ".next", "standalone"), path.join(target.appRoot, ".next", "standalone"), { recursive: true, dereference: true });
    await fs.rm(path.join(path.dirname(target.server), ".next", "cache"), { recursive: true, force: true });
    await fs.cp(source.sourceStatic, target.targetStatic, { recursive: true, dereference: true });
    if (await fs.stat(source.sourcePublic).then(() => true, () => false)) {
      await fs.cp(source.sourcePublic, target.targetPublic, { recursive: true, dereference: true });
    }
    return { runtimeRoot, server: target.server };
  } catch (error) {
    await removeFrontendRuntime(runtimeRoot, repositoryRoot);
    throw error;
  }
}

export async function removeFrontendRuntime(runtimeRoot, repositoryRoot = ROOT) {
  const cacheRoot = await fs.realpath(path.join(repositoryRoot, ".cache"));
  const target = await fs.realpath(runtimeRoot);
  if (path.dirname(target) !== cacheRoot || !path.basename(target).startsWith("real-browser-e2e-")) {
    throw new TypeError("refusing cleanup outside owned temporary runtime");
  }
  await fs.rm(target, { recursive: true, force: true });
}

export function processIdentity(stat) {
  // comm may contain spaces and parentheses; fields after its final ") " start
  // at state (field 3), so process-group ID is index 2 and starttime index 19.
  const end = stat.lastIndexOf(") ");
  const fields = stat.slice(end + 2).trim().split(/\s+/u);
  if (end < 0 || fields.length < 20 || !/^\d+$/u.test(fields[1]) || !/^\d+$/u.test(fields[2]) || !/^\d+$/u.test(fields[19])) {
    throw new TypeError("invalid Linux process identity");
  }
  return { parentPID: Number(fields[1]), groupPID: Number(fields[2]), startTicks: fields[19] };
}

async function registerBrowserProcess(target, browserPID) {
  if (process.platform !== "linux" || !Number.isSafeInteger(browserPID) || browserPID <= 1) throw new TypeError("Linux browser process required");
  const node = processIdentity(await fs.readFile(`/proc/${process.pid}/stat`, "utf8"));
  const browser = processIdentity(await fs.readFile(`/proc/${browserPID}/stat`, "utf8"));
  assert.equal(browser.parentPID, process.pid);
  assert.equal(browser.groupPID, browserPID);
  const registry = { node_pid: process.pid, node_start_ticks: node.startTicks, browser_pid: browserPID, browser_start_ticks: browser.startTicks };
  // The Python parent supplies a fresh path in its own temporary directory and
  // checks both PID identities before its emergency detached-group cleanup.
  await fs.writeFile(target, `${JSON.stringify(registry)}\n`, { flag: "wx", mode: 0o600 });
}

export async function cleanupOwnedResources({ browser, browserServer, children, runtimes }, hooks = {}) {
  const stop = hooks.stopChild ?? stopChild;
  const remove = hooks.removeRuntime ?? removeFrontendRuntime;
  const failures = [];
  const attempt = async (action) => {
    try { await action(); } catch { failures.push(true); }
  };
  await attempt(async () => { await browser?.close(); });
  await attempt(async () => { await browserServer?.close(); });
  for (const child of children) await attempt(() => stop(child));
  for (const runtime of runtimes) await attempt(() => remove(runtime));
  if (failures.length) throw new AggregateError([], "owned resource cleanup failed");
}

async function getJSON(origin, route) {
  const response = await fetch(origin + route, { redirect: "error", signal: AbortSignal.timeout(5_000) });
  assert.equal(response.status, 200);
  return response.json();
}

async function poll(check, signal, timeout = STAGE_TIMEOUT) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    signal.throwIfAborted();
    if (await check()) return;
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new DOMException("stage timed out", "TimeoutError");
}

async function checkServices(config) {
  for (const origin of [config.api, config.worker]) {
    for (const route of ["/healthz", "/readyz"]) {
      const body = await getJSON(origin, route);
      assert.equal(body.version, config.version);
      if (origin === config.worker) assert.equal(body.region, "japan");
    }
  }
  mailpitMessages(await getJSON(config.mailpit, "/api/v1/messages"));
}

async function waitForInvitation(config, email, previousIDs, signal) {
  let code;
  await poll(async () => {
    for (const summary of mailpitMessages(await getJSON(config.mailpit, "/api/v1/messages"))) {
      const id = messageID(summary);
      const strings = nestedStrings(summary);
      if (!id || previousIDs.has(id) || !strings.some((text) => text.toLowerCase().includes(email)) ||
          !strings.some((text) => text.includes("幕鉴后台邀请验证码"))) continue;
      code = extractInvitationCode(await getJSON(config.mailpit, `/api/v1/message/${id}`));
      if (code) return true;
    }
    return false;
  }, signal);
  return code;
}

export function publicStateMatches(snapshots, fixture, visible, displayOrigin) {
  const href = `/works/${fixture.slug}`;
  const hasHomeLink = snapshots.home.body.includes(`href="${href}"`);
  const hasSearchLink = snapshots.search.body.includes(`href="${href}"`);
  const hasSitemapURL = snapshots.sitemap.body.includes(`<loc>${displayOrigin}${href}</loc>`);
  if ([snapshots.home, snapshots.search, snapshots.sitemap].some((item) => item.status !== 200)) return false;
  if (visible) {
    return snapshots.work.status === 200 && snapshots.work.body.includes(`<h1>${fixture.title}</h1>`) &&
      hasHomeLink && hasSearchLink && hasSitemapURL;
  }
  const notFound = [200, 404].includes(snapshots.work.status) && snapshots.work.body.includes('name="robots" content="noindex') &&
    snapshots.work.body.includes("This page could not be found.");
  return notFound && !hasHomeLink && !hasSearchLink && !hasSitemapURL &&
    !snapshots.home.body.includes(fixture.title) && !snapshots.work.body.includes(fixture.title) &&
    snapshots.search.body.includes("没有找到已发布资料");
}

async function publicSnapshots(config, fixture) {
  return Object.fromEntries(await Promise.all(Object.entries({ home: "/", search: `/search?q=${encodeURIComponent(fixture.code)}`, work: `/works/${fixture.slug}`, sitemap: "/sitemaps/works" }).map(async ([key, route]) => {
    // Node fetch has no HTTP cookie/cache store. Do not add cache-busting query
    // strings or no-cache headers: the real Next server cache is under test.
    const response = await fetch(config.display + route, { redirect: "error", signal: AbortSignal.timeout(10_000) });
    return [key, { status: response.status, body: await response.text() }];
  })));
}

async function session(page, origin, expectedRole) {
  const response = await page.request.get(`${origin}/api/v1/me`);
  assert.equal(response.status(), 200);
  const body = await response.json();
  assert.equal(body.user.role, expectedRole);
  assert.equal(typeof body.user.id, "string");
  return body.user.id;
}

async function browserPublicCheck(page, config, fixture, visible) {
  await page.goto(`${config.display}/search?q=${encodeURIComponent(fixture.code)}`);
  await expect(page.getByRole("heading", { name: "作品搜索", exact: true })).toBeVisible();
  const link = page.getByRole("link", { name: `查看 ${fixture.code}`, exact: true });
  if (visible) {
    await expect(link).toBeVisible();
    await link.click();
    await expect(page).toHaveURL(`${config.display}/works/${fixture.slug}`);
    await expect(page.getByRole("heading", { name: fixture.title, exact: true })).toBeVisible();
  } else {
    await expect(link).toHaveCount(0);
    await expect(page.getByText("没有找到已发布资料", { exact: true })).toBeVisible();
    await page.goto(`${config.display}/works/${fixture.slug}`);
    await expect(page.getByText("This page could not be found.", { exact: true })).toBeVisible();
    await expect(page.getByRole("heading", { name: fixture.title, exact: true })).toHaveCount(0);
  }
}

export async function execute(config, report, environment = process.env) {
  const runtimes = [], children = [];
  const controller = new AbortController();
  let browser, browserServer;
  const interrupt = () => {
    controller.abort();
    void browser?.close().catch(() => {});
    void browserServer?.close().catch(() => {});
    for (const entry of children) entry.child.kill("SIGTERM");
  };
  process.once("SIGINT", interrupt);
  process.once("SIGTERM", interrupt);
  const stage = async (name, action) => {
    controller.signal.throwIfAborted();
    report.stage = name;
    await action();
    report.checks[name] = "passed";
    // Only fixed stage names, never Playwright errors or request diagnostics.
    process.stdout.write(`real-browser: ${name} passed\n`);
  };
  try {
    await stage("real_services_ready", () => checkServices(config));
    await stage("production_frontends_ready", async () => {
      for (const appName of ["display-web", "ops-web"]) {
        const runtime = await createFrontendRuntime(appName);
        runtimes.push(runtime.runtimeRoot);
        controller.signal.throwIfAborted();
        const child = spawn(process.execPath, [runtime.server], { cwd: ROOT, env: frontendEnvironment(config, appName, environment), stdio: "ignore", windowsHide: true });
        const entry = { name: appName, child, startupError: false };
        child.once("error", () => { entry.startupError = true; });
        children.push(entry);
        const target = appName === "display-web" ? config.display : `${config.ops}/login`;
        await poll(async () => {
          if (entry.startupError || child.exitCode !== null || child.signalCode !== null) throw new Error("frontend exited");
          try {
            const response = await fetch(target, { redirect: "error", signal: AbortSignal.timeout(2_000) });
            await response.arrayBuffer();
            return response.status === 200;
          } catch { return false; }
        }, controller.signal);
      }
    });

    report.stage = "browser_startup";
    browserServer = await chromium.launchServer({ headless: true, env: inheritedEnvironment(environment), host: "127.0.0.1" });
    await registerBrowserProcess(config.processRegistry, browserServer.process().pid);
    browser = await chromium.connect(browserServer.wsEndpoint());
    const externalRequests = [];
    async function context(mobile = false) {
      const created = await browser.newContext({ viewport: mobile ? { width: 390, height: 844 } : { width: 1366, height: 900 }, isMobile: mobile, hasTouch: mobile, serviceWorkers: "block" });
      created.setDefaultTimeout(STAGE_TIMEOUT);
      created.setDefaultNavigationTimeout(STAGE_TIMEOUT);
      await created.route("**/*", async (route) => {
        const url = new URL(route.request().url());
        if (["data:", "blob:"].includes(url.protocol) || [config.display, config.ops].includes(url.origin)) await route.continue();
        else { externalRequests.push(true); await route.abort("blockedbyclient"); }
      });
      return created;
    }
    const ownerContext = await context();
    const editorContext = await context(true);
    const owner = await ownerContext.newPage();
    const editor = await editorContext.newPage();
    const anonymousPages = [await (await context()).newPage(), await (await context(true)).newPage()];
    const fixture = { code: `BROWSER-${config.runID.toUpperCase()}`, title: `真实浏览器验收 ${config.runID}`, slug: `browser-${config.runID}`, source: "Release A synthetic browser source evidence" };
    const editorEmail = `browser-editor-${config.runID}@example.test`;
    const editorPassword = randomBytes(32).toString("base64url");
    let ownerID, editorID, entityID;
    let unexpectedDialogs = 0;
    owner.on("dialog", async (dialog) => {
      const message = dialog.message();
      if (dialog.type() === "confirm" && message.startsWith("确认隐藏“")) await dialog.accept();
      else if (message.includes("请输入当前账号密码")) await dialog.accept(config.ownerPassword);
      else if (message.startsWith("填写公开页面 slug")) await dialog.accept(fixture.slug);
      else if (["填写审核通过理由", "填写发布理由", "填写隐藏理由"].includes(message)) await dialog.accept("Release A synthetic real browser verification");
      else { unexpectedDialogs += 1; await dialog.dismiss(); }
    });

    await stage("owner_browser_login", async () => {
      await owner.goto(`${config.ops}/login`);
      await owner.getByLabel("邮箱", { exact: true }).fill(config.ownerEmail);
      await owner.getByLabel("密码", { exact: true }).fill(config.ownerPassword);
      await owner.getByRole("button", { name: "登录", exact: true }).click();
      await expect(owner).toHaveURL(`${config.ops}/`);
      ownerID = await session(owner, config.ops, "owner");
    });
    let invitationCode;
    await stage("owner_ui_invitation_mailpit", async () => {
      report.stage = "owner_invitation_page_load";
      await owner.goto(`${config.ops}/users`);
      await expect(owner.getByRole("heading", { name: "用户与权限", exact: true })).toBeVisible();
      const previousIDs = new Set(mailpitMessages(await getJSON(config.mailpit, "/api/v1/messages")).map(messageID));
      report.stage = "owner_invitation_prepare_form";
      await owner.getByLabel("邮箱", { exact: true }).fill(editorEmail);
      await namedSelect(owner, "role").selectOption("editor");
      await owner.getByLabel("邀请理由", { exact: true }).fill("Release A synthetic browser editor invitation");
      report.stage = "owner_invitation_submit";
      const [response] = await Promise.all([
        owner.waitForResponse((item) => new URL(item.url()).pathname === "/admin/v1/invitations" && item.request().method() === "POST" && item.status() !== 401),
        owner.getByRole("button", { name: "发送邀请", exact: true }).click(),
      ]);
      requireContract(response.status() === 201, "invitation_create_http_status");
      await expect(owner.getByRole("status")).toContainText("验证码已发送到对应邮箱");
      report.stage = "owner_invitation_mailpit_delivery";
      invitationCode = await waitForInvitation(config, editorEmail, previousIDs, controller.signal);
    });
    await stage("editor_browser_accept_invitation", async () => {
      await editor.goto(`${config.display}/invite`);
      await editor.getByLabel("邀请邮箱", { exact: true }).fill(editorEmail);
      await editor.getByLabel("邮箱验证码", { exact: true }).fill(invitationCode);
      await editor.getByLabel("设置密码", { exact: true }).fill(editorPassword);
      await editor.getByLabel("确认密码", { exact: true }).fill(editorPassword);
      await editor.getByRole("button", { name: "接受邀请并登录", exact: true }).click();
      await expect(editor).toHaveURL(`${config.display}/`);
      editorID = await session(editor, config.display, "editor");
      assert.notEqual(ownerID, editorID);
      // Local ports share a cookie host unlike the eventual two domains. Clear
      // the display cookie so the ops login must verify the invited password.
      await editorContext.clearCookies();
      await editor.goto(`${config.ops}/login`);
      await editor.getByLabel("邮箱", { exact: true }).fill(editorEmail);
      await editor.getByLabel("密码", { exact: true }).fill(editorPassword);
      await editor.getByRole("button", { name: "登录", exact: true }).click();
      await expect(editor).toHaveURL(`${config.ops}/`);
      assert.equal(await session(editor, config.ops, "editor"), editorID);
    });
    await stage("editor_ui_create_with_sources", async () => {
      report.stage = "editor_work_prepare_form";
      await editor.goto(`${config.ops}/works/new`);
      await editor.getByLabel("作品番号", { exact: true }).fill(fixture.code);
      await editor.getByLabel("实际发行日期", { exact: true }).fill(new Date().toISOString().slice(0, 10));
      await editor.getByLabel("作品标题", { exact: true }).fill(fixture.title);
      await namedSelect(editor, "source_type").selectOption("other");
      await editor.getByLabel("来源标题（可选）", { exact: true }).fill(fixture.source);
      await editor.getByLabel("录入理由", { exact: true }).fill("Release A synthetic browser author submission");
      report.stage = "editor_work_submit";
      const [response] = await Promise.all([
        editor.waitForResponse((item) => new URL(item.url()).pathname === "/admin/v1/works" && item.request().method() === "POST"),
        editor.getByRole("button", { name: "保存并提交审核", exact: true }).click(),
      ]);
      requireContract(response.status() === 201, "new_work_create_http_status");
      entityID = (await response.json()).id;
      assert.match(entityID, /^[a-f0-9-]{36}$/);
      await expect(editor.getByText("作品已进入审核队列", { exact: false })).toBeVisible();
      report.stage = "editor_work_success_feedback";
      requireContract(await editor.locator(".form-error").count() === 0, "new_work_form_error_after_success");
      await expect(editor.getByLabel("作品番号", { exact: true })).toHaveValue("");
      await expect(editor.getByLabel("作品标题", { exact: true })).toHaveValue("");
    });
    await stage("author_review_separation", async () => {
      await editor.goto(`${config.ops}/catalog/work/${entityID}`);
      await expect(editor.getByRole("heading", { name: "版本历史", exact: true })).toBeVisible();
      await expect(editor.getByText(fixture.source, { exact: true })).toBeVisible();
      await expect(editor.getByRole("button", { name: "领取审核", exact: true })).toHaveCount(0);
      await expect(editor.getByRole("button", { name: "审核通过", exact: true })).toHaveCount(0);
      assert.equal(await session(owner, config.ops, "owner"), ownerID);
    });
    await stage("owner_ui_claim_approve", async () => {
      await owner.goto(`${config.ops}/catalog/work/${entityID}`);
      await expect(owner.getByText(fixture.source, { exact: true })).toBeVisible();
      await owner.getByRole("button", { name: "领取审核", exact: true }).click();
      await expect(owner.getByRole("button", { name: "审核通过", exact: true })).toBeEnabled();
      await owner.getByRole("button", { name: "审核通过", exact: true }).click();
      await expect(owner.getByText("已有修订等待发布", { exact: false })).toBeVisible();
    });
    // Warm the same public URLs immediately before publication, while they are
    // still absent. No mock mutations or direct signed invalidation calls exist.
    report.stage = "warm_unpublished_public_cache";
    assert.ok(publicStateMatches(await publicSnapshots(config, fixture), fixture, false, config.display));
    const publishStarted = Date.now();
    await stage("owner_ui_publish", async () => {
      await owner.goto(`${config.ops}/`);
      const row = owner.getByRole("row").filter({ hasText: `${fixture.code} / ${fixture.title}` });
      await row.getByRole("button", { name: "发布", exact: true }).click();
      await expect(row).toHaveCount(0);
    });
    await stage("worker_publication_cache_invalidation", async () => {
      await poll(async () => publicStateMatches(await publicSnapshots(config, fixture), fixture, true, config.display), controller.signal);
      report.cache.publication_elapsed_ms = Date.now() - publishStarted;
      assert.ok(report.cache.publication_elapsed_ms < STAGE_TIMEOUT);
      report.cache.publication_surfaces = ["home", "search", "detail", "sitemap"];
    });
    await stage("anonymous_desktop_mobile_search_detail", async () => {
      for (const page of anonymousPages) await browserPublicCheck(page, config, fixture, true);
    });
    report.stage = "warm_published_public_cache";
    assert.ok(publicStateMatches(await publicSnapshots(config, fixture), fixture, true, config.display));
    const hideStarted = Date.now();
    await stage("owner_ui_hide", async () => {
      await owner.goto(`${config.ops}/catalog`);
      const row = owner.getByRole("row").filter({ hasText: entityID });
      await row.getByRole("button", { name: "隐藏", exact: true }).click();
      await expect(row).toContainText("已隐藏");
    });
    await stage("worker_hidden_cache_invalidation", async () => {
      await poll(async () => publicStateMatches(await publicSnapshots(config, fixture), fixture, false, config.display), controller.signal);
      report.cache.hidden_elapsed_ms = Date.now() - hideStarted;
      assert.ok(report.cache.hidden_elapsed_ms < STAGE_TIMEOUT);
      report.cache.hidden_surfaces = ["home", "search", "detail", "sitemap"];
      await checkServices(config);
    });
    await stage("anonymous_desktop_mobile_hidden", async () => {
      for (const page of anonymousPages) await browserPublicCheck(page, config, fixture, false);
    });
    await stage("browser_loopback_only", async () => {
      assert.equal(externalRequests.length, 0);
      assert.equal(unexpectedDialogs, 0);
    });
    // Synthetic IDs allow the restricted database parent to independently check
    // author/reviewer/audit/outbox completion without receiving browser secrets.
    report.fixture = { entity_type: "work", entity_id: entityID, author_id: editorID, reviewer_id: ownerID, final_status: "hidden" };
    assert.ok(Object.values(report.checks).every((status) => status === "passed"));
    report.status = "passed";
    report.stage = "complete";
  } finally {
    try {
      await cleanupOwnedResources({ browser, browserServer, children, runtimes });
    } finally {
      process.removeListener("SIGINT", interrupt);
      process.removeListener("SIGTERM", interrupt);
    }
  }
}

export async function main(argv = process.argv.slice(2), environment = process.env) {
  let output;
  const report = newReport();
  try {
    const options = parseArgs(argv);
    if (options.help) {
      process.stdout.write("node scripts/release_a_real_browser_e2e.mjs --output <new-evidence.json>\nRequires the disposable real-stack orchestrator, both production builds and installed Chromium.\n");
      return 0;
    }
    output = evidencePath(options.output);
    // Never overwrite a historical evidence file, even on configuration failure.
    await fs.mkdir(path.dirname(output), { recursive: true });
    const handle = await fs.open(output, "wx", 0o600);
    await handle.close();
    await execute(validateEnvironment(environment), report, environment);
  } catch (error) {
    markFailure(report, error);
    if (error?.code === "EEXIST") output = undefined;
  }
  if (output) await fs.writeFile(output, `${JSON.stringify(report, null, 2)}\n`, { mode: 0o600 });
  process.stdout.write(`real-browser: ${report.status} (${report.stage})\n`);
  return report.status === "passed" ? 0 : 1;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) process.exitCode = await main();
