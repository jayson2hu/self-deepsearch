import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import vm from "node:vm";
import ts from "typescript";

const source = await readFile(new URL("../../apps/ops-web/lib/media-upload-client.ts", import.meta.url), "utf8");
const reauthSource = await readFile(new URL("../../apps/ops-web/lib/reauth-client.ts", import.meta.url), "utf8");
function load(source, globals = {}) {
  const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  const exports = {};
  vm.runInNewContext(output, { exports, AbortSignal, TextDecoder, ...globals });
  return exports;
}
function client(fetcher = () => { throw new Error("unexpected request"); }) {
  return load(source, { require: (name) => { assert.equal(name, "./reauth-client"); return { adminFetch: fetcher }; } });
}
const now = Date.parse("2026-09-11T00:01:00Z");
const actor = "40000000-0000-4000-8000-000000000004";
const id = "92000000-0000-4000-8000-000000000002";
const epoch = "91000000-0000-4000-8000-000000000001";
function overview() {
  return { state: { target_ref: "d".repeat(64), receipt: { status: "observed", epoch, generation: 1, mode: "paused", command_id: epoch, applied_at: "2026-09-11T00:00:00Z" },
    last_success_at: "2026-09-11T00:00:59.123456Z", updated_at: "2026-09-11T00:00:59.123456Z", error_code: null, fresh: true }, commands: [] };
}
function input() {
  const s = overview().state;
  return { idempotency_key: id, target_ref: s.target_ref, expected_epoch: epoch, expected_generation: 1, expected_observed_at: s.last_success_at, mode: "enabled", reason: "人工核对后恢复", confirmed: true };
}
function command() {
  return { command_id: id, actor_id: actor, mode: "enabled", reason: input().reason, status: "pending", created_at: "2026-09-11T00:01:00.123456Z", expires_at: "2026-09-11T00:06:00Z",
    first_dispatched_at: null, completed_at: null, dispatch_count: 0, error_code: null, receipt: null, idempotent_replay: false };
}
function applied() {
  return { ...command(), status: "applied", first_dispatched_at: "2026-09-11T00:02:00Z", completed_at: "2026-09-11T00:02:01Z", dispatch_count: 1,
    receipt: { status: "observed", epoch, generation: 2, mode: "enabled", command_id: id, applied_at: "2026-09-11T00:02:00Z" } };
}
const json = (value, status = 200) => Response.json(value, { status });

test("upload overview requires complete bounded consistent evidence", () => {
  const c = client();
  assert.equal(c.validateUploadOverview(overview()), true);
  assert.equal(c.validateUploadOverview({ state: null, commands: [] }), true);
  const variants = [
    (v) => { delete v.state; }, (v) => { delete v.commands; }, (v) => { v.state.receipt = {}; },
    (v) => { v.state.receipt.generation = 1.5; }, (v) => { v.state.receipt.generation = Number.MAX_SAFE_INTEGER + 1; },
    (v) => { v.state.receipt.status = "unknown"; }, (v) => { v.state.receipt.applied_at = "2026-02-30T00:00:00Z"; },
    (v) => { v.state.receipt.mode = ["paused"]; }, (v) => { v.commands = [command()]; v.commands[0].status = ["pending"]; },
    (v) => { v.state.receipt.extra = "secret"; }, (v) => { v.state.last_success_at = null; },
    (v) => { v.state.fresh = "true"; }, (v) => { v.state.error_code = "control_apply_unconfirmed"; },
    (v) => { v.commands = [command(), command()]; }, (v) => { v.commands = Array(11).fill(command()); },
    (v) => { v.commands = [applied()]; v.commands[0].receipt = null; },
    (v) => { v.commands = [applied()]; v.commands[0].receipt.command_id = epoch; },
    (v) => { v.commands = [command()]; v.commands[0].status = "running"; },
    (v) => { v.commands = [command()]; v.commands[0].completed_at = "2026-09-11T00:02:00Z"; },
    (v) => { v.commands = [command()]; v.commands[0].expires_at = "2026-09-11T00:06:01Z"; },
    (v) => { v.commands = [applied()]; v.commands[0].error_code = "failure"; },
  ];
  for (const change of variants) { const v = overview(); change(v); assert.equal(c.validateUploadOverview(v), false); }
});

test("freshness expires locally and never treats future or missing evidence as healthy", () => {
  const c = client(), v = overview();
  assert.equal(c.isUploadStateFresh(v.state, now), true);
  assert.equal(c.isUploadStateFresh(v.state, now + 120000), false);
  assert.equal(c.isUploadStateFresh(v.state, now - 60000), false);
  assert.equal(c.isUploadStateFresh(null, now), false);
  v.state.fresh = false;
  assert.equal(c.isUploadStateFresh(v.state, now), false);
});

test("upload reads use no-store, bounded signal and complete response validation", async () => {
  const c = client(async (path, options) => {
    assert.equal(path, "/admin/v1/media/upload-control");
    assert.equal(options.cache, "no-store"); assert.equal(options.redirect, "error"); assert.ok(options.signal);
    return json(overview());
  });
  assert.equal((await c.getUploadControl()).state.last_success_at, overview().state.last_success_at);
  for (const response of [json({ state: {} }), json(overview(), 202), new Response("private malformed value", { headers: { "Content-Type": "application/json" } }),
    new Response(" ".repeat(131073), { headers: { "Content-Type": "application/json" } }), new Response("{}", { headers: { "Content-Type": "text/html" } }),
    new Response(new Uint8Array([0xff]), { headers: { "Content-Type": "application/json" } })]) {
    await assert.rejects(client(() => response).getUploadControl(), (e) => !e.message.includes("private malformed") && /不完整|限制/.test(e.message));
  }
});

test("queued and replayed submission preserve the original exact request", async () => {
  const body = input(), sent = [];
  const c = client(async (_path, options) => { sent.push(options.body); return sent.length === 1 ? json({ command: command() }, 202) : json({ command: { ...applied(), idempotent_replay: true } }); });
  assert.equal((await c.submitUploadCommand(body, actor)).command.status, "pending");
  assert.equal((await c.submitUploadCommand(body, actor)).command.status, "applied");
  assert.equal(sent[0], sent[1]);
  assert.equal(JSON.parse(sent[0]).expected_observed_at, "2026-09-11T00:00:59.123456Z");
});

test("false successes and unrelated receipts stay unconfirmed", async () => {
  const cases = [
    [202, { status: "pending" }], [202, { ...command(), actor_id: epoch }], [202, { ...command(), mode: "paused" }],
    [202, { ...command(), reason: "other request" }], [202, { ...command(), idempotent_replay: true }], [202, applied()], [200, command()],
    [200, { ...applied(), idempotent_replay: true, receipt: { ...applied().receipt, epoch: id } }],
    [200, { ...applied(), idempotent_replay: true, receipt: { ...applied().receipt, generation: 3 } }],
  ];
  for (const [status, value] of cases) await assert.rejects(client(() => json({ command: value }, status)).submitUploadCommand(input(), actor), /不完整或不匹配/);
});

test("only a definite conflict clears confirmation, errors never expose private bodies", async () => {
  for (const status of [400, 401, 403, 404, 409, 429, 500, 503]) {
    const c = client(() => json({ error: { message: "private password sample" } }, status));
    await assert.rejects(c.submitUploadCommand(input(), actor), (e) => !e.message.includes("private") && (e instanceof c.UploadControlConflict) === (status === 409));
  }
});

test("shared recent-auth request now inherits the caller deadline", async () => {
  const controller = new AbortController(), calls = [];
  const c = load(reauthSource, { window: { prompt: () => "synthetic password" }, fetch: async (path, options) => {
    calls.push([path, options]);
    return calls.length === 1 ? json({ error: { code: "REAUTH_REQUIRED" } }, 401) : json({ ok: true });
  } });
  await c.adminFetch("/admin/v1/media/upload-control/commands", { method: "POST", body: "{}", signal: controller.signal });
  assert.equal(calls.length, 3);
  assert.equal(calls[1][0], "/api/v1/auth/reauth");
  assert.equal(calls[1][1].signal, controller.signal);
  assert.equal(calls[2][1].body, calls[0][1].body);
});
