import assert from "node:assert/strict";
import test from "node:test";
import ts from "typescript";
import vm from "node:vm";
import { readFile } from "node:fs/promises";

const source = await readFile(new URL("../../apps/ops-web/lib/security-policy.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const module = { exports: {} };
vm.runInNewContext(compiled, { crypto, module, exports: module.exports, Uint8Array }, { filename: "security-policy.js" });
const policy = module.exports;

test("ops CSP nonce uses 128 bits of request-local randomness", () => {
  const first = policy.createRequestNonce();
  const second = policy.createRequestNonce();
  assert.match(first, /^[a-f0-9]{32}$/);
  assert.match(second, /^[a-f0-9]{32}$/);
  assert.notEqual(first, second);
});

test("ops CSP permits only nonce-bearing bootstrap scripts", () => {
  const nonce = "0123456789abcdef0123456789abcdef";
  const value = policy.opsContentSecurityPolicy(nonce);
  assert.match(value, new RegExp(`script-src 'self' 'nonce-${nonce}' 'strict-dynamic'`));
  assert.doesNotMatch(value, /script-src[^;]*'unsafe-inline'/);
  assert.match(value, /script-src-attr 'none'/);
  assert.match(value, /frame-ancestors 'none'/);
  assert.match(value, /form-action 'self'/);
});

test("ops CSP rejects malformed or operator-supplied nonces", () => {
  for (const nonce of ["", "short", "contains quote'", "contains/slash", "a".repeat(129)]) {
    assert.throws(() => policy.opsContentSecurityPolicy(nonce), /invalid CSP nonce/);
  }
});
