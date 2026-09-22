import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = readFileSync(new URL("../../apps/ops-web/lib/reauth-client.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const json = (value, status = 200) => Response.json(value, { status });
const required = () => json({ error: { code: "REAUTH_REQUIRED" } }, 401);
function deferred() { let resolve; const promise = new Promise((done) => { resolve = done; }); return { promise, resolve }; }
function harness({ password = async () => "synthetic-password", fetcher } = {}) {
  const exports = {}, calls = [], prompts = [];
  vm.runInNewContext(javascript, { exports, window: {}, AbortSignal, AbortController,
    require(name) {
      assert.equal(name, "./password-confirmation");
      return { requestPassword: (signal) => { prompts.push(signal); return password(signal); } };
    },
    fetch: async (path, options) => { calls.push({ path, options }); return fetcher(path, options, calls); },
  });
  return { ...exports, calls, prompts };
}

test("only REAUTH_REQUIRED asks for a masked confirmation", async () => {
  for (const response of [json({ ok: true }), json({ error: { code: "AUTH_REQUIRED" } }, 401), json({}, 503)]) {
    const h = harness({ fetcher: async () => response });
    assert.equal(await h.adminFetch("/mutation"), response);
    assert.equal(h.prompts.length, 0);
  }
});

test("cancellation returns the initial rejection without confirming or retrying", async () => {
  const h = harness({ password: async () => null, fetcher: async () => required() });
  assert.equal((await h.adminFetch("/mutation")).status, 401);
  assert.equal(h.calls.length, 1);
});

test("password rejection and transport failure never retry the mutation", async () => {
  for (const failedTransport of [false, true]) {
    const h = harness({ fetcher: async (path) => {
      if (path !== "/api/v1/auth/reauth") return required();
      if (failedTransport) throw new TypeError("network failed");
      return json({ error: { code: "INVALID_CREDENTIALS" } }, 401);
    } });
    const result = h.adminFetch("/mutation", { method: "POST", body: "{}" });
    if (failedTransport) await assert.rejects(result, /network failed/);
    else assert.equal((await (await result).json()).error.code, "INVALID_CREDENTIALS");
    assert.equal(h.calls.filter((call) => call.path === "/mutation").length, 1);
  }
});

test("confirmation replays the exact request once and never enters another confirmation loop", async () => {
  const h = harness({ fetcher: async (path) => path === "/api/v1/auth/reauth" ? json({ ok: true }) : required() });
  const options = { method: "POST", body: '{"reason":"synthetic"}', headers: { "Idempotency-Key": "original-key" } };
  assert.equal((await h.adminFetch("/mutation", options)).status, 401);
  assert.equal(h.calls.length, 3);
  assert.equal(h.prompts.length, 1);
  assert.deepEqual(h.calls[2].options, h.calls[0].options);
  assert.equal(h.calls[1].options.credentials, "include");
  assert.equal(h.calls[1].options.body, JSON.stringify({ password: "synthetic-password" }));
});

test("concurrent mutations share one confirmation and independently readable rejection bodies", async () => {
  const entered = deferred(), proceed = deferred();
  const h = harness({ password: async () => { entered.resolve(); return proceed.promise; }, fetcher: async (path) =>
    path === "/api/v1/auth/reauth" ? json({ error: { code: "INVALID_CREDENTIALS" } }, 401) : required() });
  const first = h.adminFetch("/first"), second = h.adminFetch("/second");
  await entered.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
  proceed.resolve("incorrect-password");
  const responses = await Promise.all([first, second]);
  assert.equal(h.prompts.length, 1);
  assert.equal(h.calls.length, 3);
  assert.deepEqual(await Promise.all(responses.map((response) => response.json())), [
    { error: { code: "INVALID_CREDENTIALS" } }, { error: { code: "INVALID_CREDENTIALS" } },
  ]);
});

test("aborting one waiter keeps confirmation alive for the other mutation", async () => {
  const entered = deferred(), proceed = deferred(), controller = new AbortController();
  const h = harness({ password: async () => { entered.resolve(); return proceed.promise; }, fetcher: async (path, options, calls) =>
    path === "/api/v1/auth/reauth" || calls.filter((call) => call.path === path).length > 1 ? json({ ok: true }) : required() });
  const first = h.adminFetch("/first", { signal: controller.signal });
  const firstError = assert.rejects(first, { name: "AbortError" });
  const second = h.adminFetch("/second");
  await entered.promise;
  await new Promise((resolve) => setTimeout(resolve, 0));
  controller.abort();
  await firstError;
  assert.equal(h.prompts[0].aborted, false);
  proceed.resolve("synthetic-password");
  assert.equal((await second).status, 200);
  assert.equal(h.calls.filter((call) => call.path === "/first").length, 1);
  assert.equal(h.calls.filter((call) => call.path === "/second").length, 2);
});

test("aborting the last waiter cancels the dialog and prevents mutation replay", async () => {
  const entered = deferred(), controller = new AbortController();
  const h = harness({ password: (signal) => new Promise((resolve, reject) => {
    entered.resolve(); signal.addEventListener("abort", () => reject(signal.reason), { once: true });
  }), fetcher: async () => required() });
  const operation = h.adminFetch("/mutation", { signal: controller.signal });
  const rejected = assert.rejects(operation, { name: "AbortError" });
  await entered.promise;
  controller.abort();
  await rejected;
  assert.equal(h.prompts[0].aborted, true);
  assert.equal(h.calls.length, 1);
});

test("late concurrent reauth rejection reuses the completed outcome without another dialog", async () => {
  for (const canceled of [false, true]) {
    const late = deferred();
    const h = harness({ password: async () => canceled ? null : "synthetic-password", fetcher: async (path, options, calls) => {
      if (path === "/api/v1/auth/reauth") return json({ ok: true });
      const attempt = calls.filter((call) => call.path === path).length;
      if (path === "/late" && attempt === 1) await late.promise;
      return attempt === 1 ? required() : json({ ok: true });
    } });
    const delayed = h.adminFetch("/late");
    assert.equal((await h.adminFetch("/first")).status, canceled ? 401 : 200);
    late.resolve();
    assert.equal((await delayed).status, canceled ? 401 : 200);
    assert.equal(h.prompts.length, 1);
    assert.equal(h.calls.filter((call) => call.path === "/api/v1/auth/reauth").length, canceled ? 0 : 1);
  }
});
