import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import ts from "typescript";

const source = readFileSync(new URL("../../packages/web-runtime/src/platform-proxy.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { forwardPlatformRequest } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const defaults = { baseURL: "http://api.internal:8080", prefix: "/api/v1", segments: ["test"] };
const encoder = new TextEncoder();

function streamedRequest(chunks, { headers, signal, neverFinish = false, cancel = () => {} } = {}) {
  let reads = 0;
  const body = new ReadableStream({
    pull(controller) {
      if (reads < chunks.length) controller.enqueue(encoder.encode(chunks[reads++]));
      else if (!neverFinish) controller.close();
    },
    cancel,
  }, { highWaterMark: 0 });
  return { request: new Request("http://site.test/api/v1/test", { method: "POST", body, duplex: "half", headers, signal }), reads: () => reads };
}

async function assertError(response, status, code) {
  assert.equal(response.status, status);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.equal((await response.json()).error.code, code);
}

test("gateway preserves encoded paths, queries, allowed headers, cookies and response streaming", async () => {
  const headers = new Headers({ "content-type": "application/json", "cache-control": "public", "x-request-id": "upstream", "retry-after": "5", "x-private": "secret" });
  headers.append("set-cookie", "session=one; HttpOnly");
  headers.append("set-cookie", "reauth=two; HttpOnly");
  let controller;
  const body = new ReadableStream({ start(value) { controller = value; } });
  const incoming = new Request("http://site.test/api/v1/search?q=a%26b", { headers: {
    cookie: "session=original", origin: "http://origin.test", "idempotency-key": "same-key", "x-request-id": "original",
    authorization: "not-forwarded", connection: "close",
  } });
  const response = await forwardPlatformRequest(incoming, { ...defaults, segments: ["search", "a/b", "中文"], fetcher: async (url, options) => {
    assert.equal(url.href, "http://api.internal:8080/api/v1/search/a%2Fb/%E4%B8%AD%E6%96%87?q=a%26b");
    assert.equal(options.body, undefined);
    assert.equal(options.redirect, "manual");
    assert.equal(options.cache, "no-store");
    for (const name of ["cookie", "origin", "idempotency-key", "x-request-id"]) assert.equal(options.headers.get(name), incoming.headers.get(name));
    for (const name of ["authorization", "connection"]) assert.equal(options.headers.has(name), false);
    return new Response(body, { status: 202, headers });
  } });
  assert.equal(response.status, 202);
  assert.equal(response.body, body);
  assert.equal(response.headers.get("cache-control"), "no-store");
  assert.equal(response.headers.has("x-private"), false);
  assert.equal(response.headers.get("retry-after"), "5");
  assert.deepEqual(response.headers.getSetCookie(), ["session=one; HttpOnly", "reauth=two; HttpOnly"]);
  controller.enqueue(encoder.encode("streamed")); controller.close();
  assert.equal(await response.text(), "streamed");
});

test("chunked bodies are accepted at the exact configured byte limit, including multibyte UTF-8", async () => {
  const { request } = streamedRequest(["中", "文"]);
  const response = await forwardPlatformRequest(request, { ...defaults, prefix: "/admin/v1", maximumBytes: 6, fetcher: async (url, options) => {
    assert.equal(url.pathname, "/admin/v1/test");
    assert.equal(new TextDecoder().decode(options.body), "中文");
    assert.equal(options.headers.get("origin"), "http://site.test");
    return new Response(null, { status: 204 });
  } });
  assert.equal(response.status, 204);
});

test("absent or understated lengths cannot bypass the limit; reading and forwarding stop immediately", async () => {
  for (const headers of [undefined, { "content-length": "1" }]) {
    let canceled = false;
    const input = streamedRequest(["1234", "5", "must-not-be-read"], { headers, cancel() { canceled = true; } });
    const response = await forwardPlatformRequest(input.request, { ...defaults, maximumBytes: 4, fetcher: () => assert.fail("oversized body was forwarded") });
    await assertError(response, 413, "BODY_TOO_LARGE");
    assert.equal(input.reads(), 2);
    assert.equal(canceled, true);
    assert.equal(input.request.body.locked, false);
  }
});

test("invalid or oversized declared lengths are rejected without consuming the stream", async () => {
  for (const length of ["5", "-1", "NaN", "1.2", "Infinity", "9007199254740992"]) {
    const input = streamedRequest(["12345"], { headers: { "content-length": length } });
    await assertError(await forwardPlatformRequest(input.request, { ...defaults, maximumBytes: 4, fetcher: () => assert.fail() }), 413, "BODY_TOO_LARGE");
    assert.equal(input.reads(), 0);
  }
});

test("the default body limit is 2 MiB and the SEO import override can accept larger bodies", async () => {
  const payload = "x".repeat(2 * 1024 * 1024 + 1);
  await assertError(await forwardPlatformRequest(streamedRequest([payload]).request, { ...defaults, fetcher: () => assert.fail() }), 413, "BODY_TOO_LARGE");
  const response = await forwardPlatformRequest(streamedRequest([payload]).request, { ...defaults, maximumBytes: 5 * 1024 * 1024 + 128 * 1024, fetcher: async (_, options) => {
    assert.equal(options.body.byteLength, payload.length);
    return new Response("accepted");
  } });
  assert.equal(await response.text(), "accepted");
});

test("broken inbound streams produce a controlled non-cacheable error", async () => {
  const request = new Request("http://site.test", { method: "POST", duplex: "half", body: new ReadableStream({ pull(controller) { controller.error(new Error("broken body")); } }) });
  await assertError(await forwardPlatformRequest(request, { ...defaults, fetcher: () => assert.fail() }), 400, "INVALID_BODY");
  assert.equal(request.body.locked, false);
});

test("incremental buffer growth forwards only received bytes without padding", async () => {
  const chunks = ["a".repeat(8192), "b".repeat(8192), "中".repeat(3000)];
  const response = await forwardPlatformRequest(streamedRequest(chunks).request, { ...defaults, fetcher: async (_, options) => {
    assert.equal(options.body.byteLength, 8192 * 2 + 3000 * 3);
    assert.equal(new TextDecoder().decode(options.body), chunks.join(""));
    return new Response("accepted");
  } });
  assert.equal(await response.text(), "accepted");
});

test("slow or canceled inbound streams stop within the deadline, even if producer cancellation hangs", async () => {
  for (const mode of ["timeout", "abort"]) {
    const controller = new AbortController();
    let canceled = false;
    const input = streamedRequest([], { neverFinish: true, signal: controller.signal, cancel() { canceled = true; return new Promise(() => {}); } });
    const keepAlive = setTimeout(() => {}, 1000);
    const abortTimer = mode === "abort" ? setTimeout(() => controller.abort(), 10) : undefined;
    try {
      await assertError(await forwardPlatformRequest(input.request, { ...defaults, timeoutMilliseconds: 30, fetcher: () => assert.fail() }), 503, "API_UNAVAILABLE");
      assert.equal(canceled, true);
      assert.equal(input.request.body.locked, false);
    } finally { clearTimeout(keepAlive); clearTimeout(abortTimer); }
  }
});

test("client cancellation propagates to the upstream and transport failures retain the 503 contract", async () => {
  const controller = new AbortController();
  await assertError(await forwardPlatformRequest(new Request("http://site.test", { signal: controller.signal }), { ...defaults, fetcher: async (_, options) => {
    controller.abort();
    assert.equal(options.signal.aborted, true);
    options.signal.throwIfAborted();
  } }), 503, "API_UNAVAILABLE");
  await assertError(await forwardPlatformRequest(new Request("http://site.test"), { ...defaults, fetcher: async () => { throw new TypeError("network unavailable"); } }), 503, "API_UNAVAILABLE");
});

test("dot segments cannot escape the selected API prefix", async () => {
  for (const segment of [".", ".."]) {
    await assertError(await forwardPlatformRequest(new Request("http://site.test"), { ...defaults, segments: [segment, "admin"], fetcher: () => assert.fail() }), 400, "INVALID_PATH");
  }
});
