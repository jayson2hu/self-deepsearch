import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
import vm from "node:vm";

async function compile(relativePath, require = () => { throw new Error("unexpected import"); }, globals = {}) {
  const source = await readFile(new URL(relativePath, import.meta.url), "utf8");
  const output = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  const module = { exports: {} };
  vm.runInNewContext(output, { module, exports: module.exports, require, URL, URLSearchParams, ...globals }, { filename: relativePath });
  return module.exports;
}

const targets = await compile("../../apps/display-web/lib/return-target.ts");
const intents = await compile("../../apps/display-web/lib/auth-intent.ts", (name) => {
  assert.equal(name, "./return-target");
  return targets;
});

function memoryStorage({ reject = false } = {}) {
  const values = new Map();
  return {
    getItem(key) { if (reject) throw new Error("disabled"); return values.get(key) ?? null; },
    setItem(key, value) { if (reject) throw new Error("disabled"); values.set(key, value); },
    removeItem(key) { if (reject) throw new Error("disabled"); values.delete(key); },
    value(key) { return values.get(key) ?? null; },
  };
}

test("returnTo only accepts known same-site routes and whitelisted query keys", () => {
  const valid = [
    ["/", "/"],
    ["/works/test-001", "/works/test-001"],
    ["/performers/person_1", "/performers/person_1"],
    ["/studios/studio.example", "/studios/studio.example"],
    ["/account/favorites", "/account/favorites"],
    ["/search?q=中文&type=work", "/search?q=%E4%B8%AD%E6%96%87&type=work"],
  ];
  for (const [input, expected] of valid) assert.equal(targets.sanitizeReturnTo(input), expected, input);

  const invalid = [
    "https://evil.example/works/a", "javascript:alert(1)", "//evil.example/path", "/\\evil.example/path",
    "/works/a%2fb", "/%2e%2e/login", "/%252f%252fevil.example", "/works/a%5cb", "/works/a\nnext",
    "/login", "/unknown", "/works/a?next=1", "/search?returnTo=%2Fworks%2Fa", "/search?evil=1",
    "", null, ["/works/a", "/works/b"],
  ];
  for (const input of invalid) assert.equal(targets.sanitizeReturnTo(input), null, String(input));
});

test("auth links preserve only a sanitized target and ordinary login stays at its default", () => {
  assert.equal(targets.authPath("/login", "/works/test-001"), "/login?returnTo=%2Fworks%2Ftest-001");
  assert.equal(targets.authPath("/register", "//evil.example"), "/register");
  assert.equal(targets.authPath("/forgot-password"), "/forgot-password");
  assert.equal(targets.returnTargetFromQuery(["/works/a", "/works/b"]), null);
});

test("short-lived action intent validates entity, target, expiry and exact match", () => {
  const now = Date.parse("2026-09-16T08:00:00Z");
  const entityId = "10000000-0000-4000-8000-000000000001";
  const intent = intents.createAuthIntent("favorite", entityId, "/works/test-001", now);
  assert.ok(intent);
  const storage = memoryStorage();
  assert.equal(intents.saveAuthIntent(storage, intent), true);
  assert.deepEqual(
    { ...intents.matchingAuthIntent(storage, { kind: "favorite", entityId, targetPath: "/works/test-001" }, now + 60_000) },
    { ...intent },
  );
  assert.equal(intents.matchingAuthIntent(storage, { kind: "follow", entityId, targetPath: "/works/test-001" }, now + 60_000), null);
  assert.ok(storage.value(intents.AUTH_INTENT_STORAGE_KEY), "a visit to another target must not consume the intent");
  assert.equal(intents.loadAuthIntent(storage, now + intents.AUTH_INTENT_TTL_MS + 1), null);
  assert.equal(storage.value(intents.AUTH_INTENT_STORAGE_KEY), null);
  assert.equal(intents.createAuthIntent("follow", entityId, "/works/test-001", now), null);
  assert.equal(intents.createAuthIntent("favorite", "not-a-uuid", "/works/test-001", now), null);
});

test("malformed or unavailable sessionStorage fails closed without blocking navigation", () => {
  const broken = memoryStorage();
  broken.setItem(intents.AUTH_INTENT_STORAGE_KEY, "{private malformed value");
  assert.equal(intents.loadAuthIntent(broken), null);
  assert.equal(broken.value(intents.AUTH_INTENT_STORAGE_KEY), null);

  const denied = memoryStorage({ reject: true });
  const intent = intents.createAuthIntent("follow", "20000000-0000-4000-8000-000000000001", "/performers/test-person", Date.now());
  assert.ok(intent);
  assert.equal(intents.saveAuthIntent(denied, intent), false);
  assert.equal(intents.loadAuthIntent(denied), null);
  assert.doesNotThrow(() => intents.clearAuthIntent(denied));
});

test("continuation UI keeps mutations behind explicit POST confirmation", async () => {
  const favorite = await readFile(new URL("../../apps/display-web/components/favorite-button.tsx", import.meta.url), "utf8");
  const follow = await readFile(new URL("../../apps/display-web/components/follow-button.tsx", import.meta.url), "utf8");
  const authClient = await readFile(new URL("../../apps/display-web/lib/auth-client.ts", import.meta.url), "utf8");
  for (const source of [favorite, follow]) {
    assert.match(source, /确认(?:收藏|关注)/);
    assert.match(source, /async function confirmResume\(\)/);
    assert.match(source, /apiMutation\([^\n]+, "POST"\)/);
    assert.match(source, /onClick=\{confirmResume\}/);
    assert.match(source, /CONTENT_NOT_FOUND/);
    assert.doesNotMatch(source, /useEffect\(async/);
  }
  assert.match(authClient, /sanitizeReturnTo\(path\) \?\? "\/"/);
});
