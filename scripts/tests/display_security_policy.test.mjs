import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
import vm from "node:vm";

async function compileCommonJS(relativePath) {
  const source = await readFile(new URL(relativePath, import.meta.url), "utf8");
  const compiled = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(compiled, { crypto, module, exports: module.exports, Uint8Array }, { filename: relativePath });
  return module.exports;
}

const security = await compileCommonJS("../../apps/display-web/lib/security-policy.ts");
const media = await compileCommonJS("../../apps/display-web/lib/media-policy.ts");

test("public account CSP nonce uses request-local 128-bit randomness", () => {
  const first = security.createRequestNonce();
  const second = security.createRequestNonce();
  assert.match(first, /^[a-f0-9]{32}$/);
  assert.match(second, /^[a-f0-9]{32}$/);
  assert.notEqual(first, second);
});

test("only account and identity HTML paths use the dynamic nonce policy", () => {
  for (const pathname of ["/login", "/register", "/forgot-password", "/invite", "/logout", "/account", "/account/history"]) {
    assert.equal(security.isSensitivePublicPage(pathname), true, pathname);
  }
  for (const pathname of ["/", "/latest", "/search", "/works/test-1", "/performers/person-1", "/api/v1/me", "/login-help", "/accounting"]) {
    assert.equal(security.isSensitivePublicPage(pathname), false, pathname);
  }
});

test("account policy requires nonce scripts while cached catalog policy keeps its explicit boundary", () => {
  const nonce = "0123456789abcdef0123456789abcdef";
  const account = media.mediaContentSecurityPolicy(false, nonce);
  assert.match(account, new RegExp(`script-src 'self' 'nonce-${nonce}' 'strict-dynamic' https://challenges\\.cloudflare\\.com`));
  assert.doesNotMatch(account, /script-src[^;]*'unsafe-inline'/);
  assert.match(account, /script-src-attr 'none'/);

  const cachedCatalog = media.mediaContentSecurityPolicy(false);
  assert.match(cachedCatalog, /script-src 'self' 'unsafe-inline' https:\/\/challenges\.cloudflare\.com/);
  assert.match(cachedCatalog, /script-src-attr 'none'/);
  assert.throws(() => media.mediaContentSecurityPolicy(false, "operator supplied'"), /invalid CSP nonce/);
});
