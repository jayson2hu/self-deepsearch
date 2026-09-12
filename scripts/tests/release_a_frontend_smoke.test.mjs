import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  countOccurrences,
  jsonLDFailures,
  missingSignals,
  npmBuildInvocation,
  pageFailures,
  nonceRotationFailures,
  parseArgs,
  presentSignals,
  prepareStandaloneAssets,
  proxyFailures,
  RELEASE_A_FORBIDDEN_AD_SIGNALS,
  securityHeaderFailures,
  standalonePaths,
  stylesheetFailures,
  stylesheetPaths,
} from "../release_a_frontend_smoke.mjs";
import { assetPaths, stageAssets } from "../start-next-standalone.mjs";

function response(status, body, headers = {}) {
  return { status, body, headers: new Headers(headers) };
}

test("parseArgs accepts build and timeout options", () => {
  assert.deepEqual(parseArgs(["--build", "--timeout", "9000"]), { build: true, help: false, timeoutMs: 9000 });
});

test("npmBuildInvocation avoids spawning npm.cmd directly on Windows", () => {
  assert.deepEqual(
    npmBuildInvocation({ platform: "win32", npmExecPath: "C:\\npm\\npm-cli.js", nodeExecutable: "C:\\node.exe" }),
    { command: "C:\\node.exe", args: ["C:\\npm\\npm-cli.js", "run", "build"] },
  );
  assert.deepEqual(
    npmBuildInvocation({ platform: "win32", npmExecPath: "", commandShell: "C:\\Windows\\cmd.exe" }),
    { command: "C:\\Windows\\cmd.exe", args: ["/d", "/s", "/c", "npm run build"] },
  );
});

test("pageFailures reports status and missing HTML signals", () => {
  assert.deepEqual(
    pageFailures("首页", response(500, "hello"), { includes: ["hello", "18+"] }),
    ["首页: expected HTTP 200, got 500", "首页: missing signals: 18+"],
  );
});

test("proxyFailures enforces 503, no-store, and API_UNAVAILABLE", () => {
  const result = response(503, '{"error":{"code":"API_UNAVAILABLE"}}', { "cache-control": "no-store" });
  assert.deepEqual(proxyFailures("proxy", result), []);
  assert.equal(proxyFailures("proxy", response(502, "upstream", {})).length, 3);
});

test("signal helpers count repeated ad placeholders", () => {
  const html = 'data-ad-state="placeholder" data-ad-state="placeholder"';
  assert.deepEqual(missingSignals(html, ['data-ad-state="placeholder"']), []);
  assert.equal(countOccurrences(html, 'data-ad-state="placeholder"'), 2);
});

test("Release A ad guard rejects network, active, floating, countdown, and popunder signals", () => {
  const safe = '<aside data-ad-state="placeholder" data-ad-placement="rail">测试占位，不加载第三方脚本</aside>';
  assert.deepEqual(presentSignals(safe, RELEASE_A_FORBIDDEN_AD_SIGNALS), []);

  const unsafe = [
    '<script src="https://pagead2.googlesyndication.com/pagead/js/adsbygoogle.js"></script>',
    '<aside data-ad-state="active" data-ad-behavior="floating" data-ad-close-countdown="5"></aside>',
    '<button class="popunder">open</button>',
  ].join("");
  assert.deepEqual(presentSignals(unsafe, RELEASE_A_FORBIDDEN_AD_SIGNALS), [
    "pagead2.googlesyndication.com",
    "adsbygoogle",
    'data-ad-state="active"',
    'data-ad-behavior="floating"',
    "data-ad-close-countdown",
    "popunder",
  ]);
});

test("stylesheet helpers find local CSS links and reject missing or empty CSS responses", () => {
  const html = [
    '<link rel="preload" href="/_next/static/a.css">',
    "<link href='/_next/static/a.css' rel='stylesheet preload'>",
    '<link rel="stylesheet" href="https://cdn.example.test/external.css">',
    '<link rel="stylesheet" href="/_next/static/b.css">',
  ].join("");
  assert.deepEqual(stylesheetPaths(html), ["/_next/static/a.css", "/_next/static/b.css"]);
  assert.deepEqual(stylesheetFailures("CSS", response(200, "body{}", { "content-type": "text/css; charset=utf-8" })), []);
  assert.deepEqual(stylesheetFailures("CSS", response(404, "", {})), [
    "CSS: expected HTTP 200, got 404",
    "CSS: expected text/css content type, got missing",
    "CSS: stylesheet body is empty",
  ]);
});

test("security header helper enforces CSP and browser hardening headers", () => {
  const headers = {
    "content-security-policy": "default-src 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' https://challenges.cloudflare.com",
    "permissions-policy": "camera=()",
    "referrer-policy": "strict-origin-when-cross-origin",
    "x-content-type-options": "nosniff",
    "x-frame-options": "DENY",
  };
  assert.deepEqual(securityHeaderFailures("display", response(200, "", headers), { allowTurnstile: true }), []);
  assert.deepEqual(securityHeaderFailures("ops", response(200, "", { ...headers, "content-security-policy": "default-src 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'" })), []);
  const nonce = "0123456789abcdef0123456789abcdef";
  const strictHeaders = { ...headers, "cache-control": "private, no-store", "content-security-policy": `default-src 'self'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' 'nonce-${nonce}' 'strict-dynamic'; script-src-attr 'none'` };
  assert.deepEqual(securityHeaderFailures("strict ops", response(200, `<script nonce="${nonce}">self.__next_f=[]</script>`, strictHeaders), { requireScriptNonce: true }), []);
  const unsafePolicy = strictHeaders["content-security-policy"].replace("'strict-dynamic'", "'strict-dynamic' 'unsafe-inline'");
  assert.ok(securityHeaderFailures("unsafe ops", response(200, "<script>bad()</script>", { ...strictHeaders, "content-security-policy": unsafePolicy }), { requireScriptNonce: true }).length >= 2);
  assert.ok(securityHeaderFailures("unsafe", response(200, "", {})).length >= 8);
});

test("nonce rotation helper rejects missing or reused response nonces", () => {
  const csp = (nonce) => response(200, "", { "content-security-policy": `default-src 'self'; script-src 'nonce-${nonce}'` });
  assert.deepEqual(nonceRotationFailures("ops", csp("a".repeat(32)), csp("b".repeat(32))), []);
  assert.deepEqual(nonceRotationFailures("ops", csp("a".repeat(32)), csp("a".repeat(32))), ["ops: response CSP nonce was reused across requests"]);
  assert.deepEqual(nonceRotationFailures("ops", response(200), csp("a".repeat(32))), ["ops: response CSP nonce is missing"]);
});

test("JSON-LD helper parses the target document without confusing adjacent Next scripts", () => {
  const json = JSON.stringify({
    "@context": "https://schema.org",
    "@type": "CreativeWork",
    name: "安全结构化数据 </script><script>alert(1)</script>",
    mainEntityOfPage: "https://example.test/works/test",
  }).replaceAll("<", "\\u003c");
  const html = `<script type="application/ld+json">${json}</script><script src="/_next/static/app.js"></script>`;
  assert.deepEqual(jsonLDFailures("作品详情", response(200, html), "CreativeWork"), []);
});

test("JSON-LD helper rejects raw script-closing content", () => {
  const html = '<script type="application/ld+json">{"@type":"CreativeWork","name":"unsafe </script><script>alert(1)</script>"}</script>';
  const failures = jsonLDFailures("作品详情", response(200, html), "CreativeWork");
  assert.ok(failures.some((failure) => failure.includes("not valid JSON")));
  assert.ok(failures.some((failure) => failure.includes("missing CreativeWork")));
});

test("standalonePaths points at both source and staged asset trees", () => {
  const paths = standalonePaths("display-web", "C:\\repo");
  assert.equal(paths.server, "C:\\repo\\apps\\display-web\\.next\\standalone\\apps\\display-web\\server.js");
  assert.equal(paths.targetStatic, "C:\\repo\\apps\\display-web\\.next\\standalone\\apps\\display-web\\.next\\static");
  assert.equal(paths.targetPublic, "C:\\repo\\apps\\display-web\\.next\\standalone\\apps\\display-web\\public");
});

test("prepareStandaloneAssets reports only directories created by the runner", async (context) => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "release-a-smoke-"));
  context.after(() => fs.rm(root, { recursive: true, force: true }));
  const paths = {
    sourceStatic: path.join(root, "source-static"),
    targetStatic: path.join(root, "target", ".next", "static"),
    sourcePublic: path.join(root, "source-public"),
    targetPublic: path.join(root, "target", "public"),
  };
  await fs.mkdir(paths.sourceStatic, { recursive: true });
  await fs.mkdir(paths.sourcePublic, { recursive: true });
  await fs.writeFile(path.join(paths.sourceStatic, "app.css"), "body{}");
  await fs.writeFile(path.join(paths.sourcePublic, "default-work.svg"), "<svg/>");

  assert.deepEqual(await prepareStandaloneAssets(paths), [paths.targetStatic, paths.targetPublic]);
  assert.equal(await fs.readFile(path.join(paths.targetStatic, "app.css"), "utf8"), "body{}");
  assert.deepEqual(await prepareStandaloneAssets(paths), []);
});

test("standalone start stages current static and public assets before importing the server", async (context) => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "release-a-start-"));
  context.after(() => fs.rm(root, { recursive: true, force: true }));
  const appRoot = path.join(root, "apps", "display-web");
  const server = path.join(appRoot, ".next", "standalone", "apps", "display-web", "server.js");
  await fs.mkdir(path.dirname(server), { recursive: true });
  await fs.mkdir(path.join(appRoot, ".next", "static"), { recursive: true });
  await fs.mkdir(path.join(appRoot, "public"), { recursive: true });
  await fs.writeFile(server, "// test server");
  await fs.writeFile(path.join(appRoot, ".next", "static", "app.css"), "body{color:red}");
  await fs.writeFile(path.join(appRoot, "public", "default-work.svg"), "<svg/>");

  const paths = assetPaths(server, root);
  await stageAssets(paths);
  assert.equal(await fs.readFile(path.join(paths.targetStatic, "app.css"), "utf8"), "body{color:red}");
  assert.equal(await fs.readFile(path.join(paths.targetPublic, "default-work.svg"), "utf8"), "<svg/>");
});
