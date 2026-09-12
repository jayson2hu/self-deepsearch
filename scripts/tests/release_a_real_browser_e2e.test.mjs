import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  cleanupOwnedResources, createFrontendRuntime, evidencePath, extractInvitationCode, frontendEnvironment,
  inheritedEnvironment, loopbackOrigin, mailpitMessages, markFailure, messageID,
  namedSelect, newReport, parseArgs, processIdentity, publicStateMatches, removeFrontendRuntime, validateEnvironment,
} from "../release_a_real_browser_e2e.mjs";
import { standalonePaths } from "../release_a_frontend_smoke.mjs";

function environment() {
  return {
    CONFIRM_RELEASE_A_BROWSER_E2E: "disposable-database",
    REAL_BROWSER_API_URL: "http://127.0.0.1:31001",
    REAL_BROWSER_DISPLAY_URL: "http://127.0.0.1:31002",
    REAL_BROWSER_OPS_URL: "http://127.0.0.1:31003",
    REAL_BROWSER_MAILPIT_URL: "http://127.0.0.1:31004",
    REAL_BROWSER_WORKER_URL: "http://127.0.0.1:31005",
    REAL_BROWSER_RUN_ID: "0123456789abcdef",
    REAL_BROWSER_BUILD_VERSION: "real-stack-e2e-0123456789abcdef",
    REAL_BROWSER_OWNER_EMAIL: "owner-0123456789abcdef@example.test",
    REAL_BROWSER_OWNER_PASSWORD: "synthetic-owner-password",
    CACHE_HMAC_SECRET: "synthetic-cache-secret-with-32-characters",
    REAL_BROWSER_PROCESS_REGISTRY: ".cache/synthetic-browser-processes.json",
  };
}

test("real browser configuration accepts only a confirmed synthetic, versioned loopback stack", () => {
  const env = environment();
  const config = validateEnvironment(env);
  assert.equal(config.api, env.REAL_BROWSER_API_URL);
  assert.equal(config.version, env.REAL_BROWSER_BUILD_VERSION);
  for (const key of Object.keys(env)) assert.throws(() => validateEnvironment({ ...env, [key]: undefined }), key);
  for (const overrides of [
    { CONFIRM_RELEASE_A_BROWSER_E2E: "yes" },
    { REAL_BROWSER_OWNER_EMAIL: "owner@real-domain.com" },
    { REAL_BROWSER_OWNER_EMAIL: "x@example.test.attacker.test" },
    { REAL_BROWSER_OWNER_PASSWORD: "short" },
    { REAL_BROWSER_OWNER_PASSWORD: "x".repeat(129) },
    { REAL_BROWSER_RUN_ID: "x".repeat(16) },
    { REAL_BROWSER_RUN_ID: "abcdef" },
    { REAL_BROWSER_BUILD_VERSION: "version\nsecret" },
    { REAL_BROWSER_OPS_URL: env.REAL_BROWSER_API_URL },
    { CACHE_HMAC_SECRET: " ".repeat(32) },
  ]) assert.throws(() => validateEnvironment({ ...env, ...overrides }));
});

test("HTTP origin guard rejects external hosts, credentials, redirects and ambiguous endpoints", () => {
  assert.equal(loopbackOrigin("http://127.0.0.1:31001/"), "http://127.0.0.1:31001");
  for (const value of [
    "https://127.0.0.1:31001", "http://localhost:31001", "http://example.test:31001",
    "http://127.0.0.1", "http://127.0.0.1:0", "http://127.0.0.1:65536",
    "http://user:password@127.0.0.1:31001", "http://127.0.0.1:31001/api", "http://127.0.0.1:31001?redirect=external",
    "http://127.0.0.1:31001#fragment", "file:///tmp/test",
  ]) assert.throws(() => loopbackOrigin(value), value);
});

test("frontend and Chromium environments exclude account, provider, proxy and Node injection secrets", () => {
  const env = {
    ...environment(), PATH: "/synthetic/bin", HOME: "/synthetic/home", PLAYWRIGHT_BROWSERS_PATH: "/synthetic/browsers",
    DATABASE_URL: "private-database", AUTH_HMAC_SECRET: "private-auth-key", SMTP_URL: "private-smtp",
    HTTPS_PROXY: "private-proxy", NODE_OPTIONS: "--require malicious.cjs", AWS_SECRET_ACCESS_KEY: "private-provider",
  };
  const config = validateEnvironment(env);
  const browser = inheritedEnvironment(env);
  const display = frontendEnvironment(config, "display-web", env);
  const ops = frontendEnvironment(config, "ops-web", env);
  for (const child of [browser, display, ops]) {
    assert.equal(child.PATH, env.PATH);
    assert.equal(child.PLAYWRIGHT_BROWSERS_PATH, env.PLAYWRIGHT_BROWSERS_PATH);
    assert.equal(child.NO_PROXY, "*");
    for (const key of ["DATABASE_URL", "AUTH_HMAC_SECRET", "SMTP_URL", "HTTPS_PROXY", "NODE_OPTIONS", "AWS_SECRET_ACCESS_KEY", "REAL_BROWSER_OWNER_EMAIL", "REAL_BROWSER_OWNER_PASSWORD"]) assert.equal(child[key], undefined);
  }
  assert.equal(display.PLATFORM_API_URL, config.api);
  assert.equal(display.SITE_URL, config.display);
  assert.equal(display.CACHE_HMAC_SECRET, config.cacheSecret);
  assert.equal(display.PORT, "31002");
  assert.equal(ops.PORT, "31003");
  assert.equal(ops.CACHE_HMAC_SECRET, undefined);
  assert.equal(browser.CACHE_HMAC_SECRET, undefined);
  assert.throws(() => frontendEnvironment(config, "unknown", env));
});

test("CLI accepts one output option, never skip/build/install or unknown switches", () => {
  assert.deepEqual(parseArgs(["--output", ".cache/new.json"]), { output: ".cache/new.json", help: false });
  assert.equal(parseArgs(["--help"]).help, true);
  for (const args of [["--output"], ["--output", "--help"], ["--output", "a.json", "--output", "b.json"], ["--skip-worker"], ["--build"], ["--install"]]) assert.throws(() => parseArgs(args));
});

test("new evidence paths are JSON and confined to approved evidence/cache/temp roots", () => {
  const root = path.resolve("real-browser-unit-root");
  const temporaryRoot = path.resolve("real-browser-unit-temp");
  assert.equal(evidencePath("docs/evidence/new.json", root, temporaryRoot), path.join(root, "docs/evidence/new.json"));
  assert.equal(evidencePath(".cache/run/result.json", root, temporaryRoot), path.join(root, ".cache/run/result.json"));
  assert.equal(evidencePath(path.join(temporaryRoot, "run/result.json"), root, temporaryRoot), path.join(temporaryRoot, "run/result.json"));
  for (const value of ["README.json", "docs/evidence/../../escaped.json", "docs/evidence/new.md", ".cache/../escape.json", `${temporaryRoot}-outside/result.json`]) assert.throws(() => evidencePath(value, root, temporaryRoot));
});

test("failure evidence records only allowlisted error types, not credentials or Playwright details", () => {
  const report = newReport(new Date("2026-09-12T00:00:00Z"));
  const secret = "owner@example.test password-and-cookie mail-code-123456";
  const error = new Error(secret);
  error.name = secret;
  report.stage = "owner_browser_login";
  markFailure(report, error);
  assert.equal(report.error_type, "Error");
  assert.equal(report.status, "failed");
  assert.equal(report.stage, "owner_browser_login");
  assert.equal(JSON.stringify(report).includes(secret), false);
  markFailure(report, new TypeError(secret));
  assert.equal(report.error_type, "TypeError");
  assert.equal(JSON.stringify(report).includes(secret), false);
  assert.equal(Object.keys(report.checks).length, 15);
  assert.ok(Object.values(report.checks).every((status) => status === "not_run"));
  assert.deepEqual(report.cache.cached_surfaces, ["home", "detail", "sitemap"]);
  assert.deepEqual(report.cache.uncached_visibility_surfaces, ["search"]);
  markFailure(report, { name: "Error", failureCode: secret });
  assert.equal(report.failure_code, undefined);
  markFailure(report, { name: "Error", failureCode: "new_work_form_error_after_success" });
  assert.equal(report.failure_code, "new_work_form_error_after_success");
});

test("nested-label selects use unique field names rather than option-dependent exact labels", () => {
  const selectors = [];
  const page = { locator: (selector) => { selectors.push(selector); return { selector }; } };
  assert.equal(namedSelect(page, "role").selector, 'select[name="role"]');
  assert.equal(namedSelect(page, "source_type").selector, 'select[name="source_type"]');
  assert.throws(() => namedSelect(page, 'role"] input'));
  assert.deepEqual(selectors, ['select[name="role"]', 'select[name="source_type"]']);
});

test("Linux process identity parser handles complex names and preserves exact start ticks", () => {
  const fields = ["S", "101", "202", ...Array(16).fill("0"), "9876543210123456789", "0"];
  assert.deepEqual(processIdentity(`202 (browser (process) name) ${fields.join(" ")}\n`), { parentPID: 101, groupPID: 202, startTicks: "9876543210123456789" });
  for (const value of ["", "not a stat", "202 (name) S 1 2", `202 (name) ${fields.map((value, index) => index === 19 ? "unsafe" : value).join(" ")}`]) assert.throws(() => processIdentity(value));
});

test("browser or child shutdown failure never prevents the other owned cleanup steps", async () => {
  const actions = [];
  await assert.rejects(cleanupOwnedResources({
    browser: { close: async () => { actions.push("browser"); throw new Error("synthetic close failure"); } },
    browserServer: { close: async () => { actions.push("browser_server"); } },
    children: ["display", "ops"], runtimes: ["display_cache", "ops_cache"],
  }, {
    stopChild: async (child) => { actions.push(child); if (child === "display") throw new Error("synthetic child failure"); },
    removeRuntime: async (runtime) => { actions.push(runtime); if (runtime === "display_cache") throw new Error("synthetic directory failure"); },
  }), AggregateError);
  assert.deepEqual(actions, ["browser", "browser_server", "display", "ops", "display_cache", "ops_cache"]);
  await cleanupOwnedResources({ children: [], runtimes: [] });
});

test("Mailpit parsing supports documented list variants and rejects malformed message IDs", () => {
  const item = { ID: "abcdef-012345", To: [{ Address: "editor@example.test" }] };
  for (const list of [[item], { messages: [item] }, { Messages: [item] }]) assert.deepEqual(mailpitMessages(list), [item]);
  for (const value of [null, {}, { messages: "wrong" }]) assert.throws(() => mailpitMessages(value));
  assert.deepEqual(mailpitMessages({ messages: [null, "bad", [], item] }), [item]);
  for (const name of ["ID", "Id", "id"]) assert.equal(messageID({ [name]: "abcdef-012345" }), "abcdef-012345");
  for (const value of ["../private", "?secret", "x/y", "", 123]) assert.equal(messageID({ ID: value }), "");
});

test("invitation code extraction requires a six-digit verification label in the real message", () => {
  assert.equal(extractInvitationCode({ Text: "幕鉴后台邀请验证码：123456" }), "123456");
  assert.equal(extractInvitationCode({ nested: [{ HTML: "验证码 : 654321" }] }), "654321");
  for (const body of [null, { Text: "123456" }, { Text: "验证码:12345" }, { Text: "验证码:1234567" }]) assert.equal(extractInvitationCode(body), null);
});

const fixture = { code: "BROWSER-001", slug: "browser-001", title: "Synthetic browser work" };
const origin = "http://127.0.0.1:31002";
function visibleSnapshots() {
  return {
    home: { status: 200, body: `<a href="/works/${fixture.slug}">${fixture.title}</a>` },
    search: { status: 200, body: `<a href="/works/${fixture.slug}">${fixture.title}</a>` },
    work: { status: 200, body: `<h1>${fixture.title}</h1>` },
    sitemap: { status: 200, body: `<loc>${origin}/works/${fixture.slug}</loc>` },
  };
}
function hiddenSnapshots() {
  return {
    home: { status: 200, body: "empty home" },
    search: { status: 200, body: "没有找到已发布资料" },
    work: { status: 404, body: '<meta name="robots" content="noindex"/><h2>This page could not be found.</h2>' },
    sitemap: { status: 200, body: "<urlset/>" },
  };
}

test("publication cache evidence requires fresh visible data on all four public surfaces", () => {
  const visible = visibleSnapshots();
  assert.equal(publicStateMatches(visible, fixture, true, origin), true);
  for (const surface of Object.keys(visible)) {
    assert.equal(publicStateMatches({ ...visible, [surface]: hiddenSnapshots()[surface] }, fixture, true, origin), false, surface);
    assert.equal(publicStateMatches({ ...visible, [surface]: { ...visible[surface], status: 503 } }, fixture, true, origin), false, surface);
  }
  assert.equal(publicStateMatches({ ...visible, work: { status: 200, body: `<title>${fixture.title}</title>` } }, fixture, true, origin), false);
  assert.equal(publicStateMatches(visible, fixture, true, "http://127.0.0.1:9999"), false);
});

test("hidden cache evidence rejects degraded pages, stale links and metadata-only false positives", () => {
  const hidden = hiddenSnapshots();
  assert.equal(publicStateMatches(hidden, fixture, false, origin), true);
  assert.equal(publicStateMatches({ ...hidden, work: { ...hidden.work, status: 200 } }, fixture, false, origin), true);
  for (const surface of Object.keys(hidden)) assert.equal(publicStateMatches({ ...hidden, [surface]: visibleSnapshots()[surface] }, fixture, false, origin), false, surface);
  assert.equal(publicStateMatches({ ...hidden, work: { status: 200, body: "资料暂时无法加载" } }, fixture, false, origin), false);
  assert.equal(publicStateMatches({ ...hidden, work: { ...hidden.work, body: hidden.work.body + fixture.title } }, fixture, false, origin), false);
  assert.equal(publicStateMatches({ ...hidden, search: { status: 200, body: "查询服务暂时不可用" } }, fixture, false, origin), false);
});

test("both standalone apps use owned, independent copies with no inherited fetch cache", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "real-browser-isolation-test-"));
  const runtimes = [];
  try {
    for (const appName of ["display-web", "ops-web"]) {
      const source = standalonePaths(appName, root);
      await fs.mkdir(path.dirname(source.server), { recursive: true });
      await fs.mkdir(source.sourceStatic, { recursive: true });
      await fs.mkdir(source.sourcePublic, { recursive: true });
      await fs.writeFile(path.join(source.appRoot, ".next", "BUILD_ID"), "synthetic-build");
      await fs.writeFile(source.server, "original server");
      await fs.writeFile(path.join(source.sourceStatic, "test.css"), "original css");
      await fs.writeFile(path.join(source.sourcePublic, "test.svg"), "original image");
      const sourceCache = path.join(path.dirname(source.server), ".next", "cache", "fetch-cache", "stale");
      await fs.mkdir(path.dirname(sourceCache), { recursive: true });
      await fs.writeFile(sourceCache, "synthetic mock catalog");
      const runtime = await createFrontendRuntime(appName, root);
      runtimes.push(runtime.runtimeRoot);
      assert.equal(await fs.readFile(runtime.server, "utf8"), "original server");
      await fs.writeFile(runtime.server, "isolated mutation");
      assert.equal(await fs.readFile(source.server, "utf8"), "original server");
      await assert.rejects(fs.access(path.join(path.dirname(runtime.server), ".next", "cache", "fetch-cache", "stale")));
      assert.equal(await fs.readFile(path.join(path.dirname(runtime.server), ".next", "static", "test.css"), "utf8"), "original css");
      await assert.rejects(removeFrontendRuntime(source.appRoot, root));
    }
    await assert.rejects(removeFrontendRuntime(path.join(root, ".cache"), root));
    for (const runtimeRoot of runtimes) await removeFrontendRuntime(runtimeRoot, root);
    for (const runtimeRoot of runtimes) await assert.rejects(fs.access(runtimeRoot));
  } finally {
    await fs.rm(root, { recursive: true, force: true });
  }
});
