import assert from "node:assert/strict";
import test from "node:test";

import { createMediaGateway } from "../../infra/cloudflare/media-gateway/src/index.mjs";

const origin = "https://media.test.example";
const policyURL = "https://api.test.example/edge/v1/media-delivery-policy";
const token = "edge-policy-token-012345678901234567890123";
const key = "media-public/default/works/10000000-0000-4000-8000-000000000001/20000000-0000-4000-8000-000000000001/v1/cover-w640.webp";

class MemoryCache {
  constructor() { this.values = new Map(); }
  async match(request) { return this.values.get(request.url)?.clone(); }
  async put(request, response) { this.values.set(request.url, response.clone()); }
  clearPolicy() {
    for (const value of this.values.keys()) if (value.startsWith("https://media-policy-cache.invalid/")) this.values.delete(value);
  }
}

function fixture() {
  const cache = new MemoryCache();
  const body = new Uint8Array([1, 2, 3, 4]);
  let policyMode = "normal";
  let policyAvailable = true;
  let policyCalls = 0;
  let objectCalls = 0;
  const bucket = {
    async get(requested) {
      objectCalls++;
      if (requested !== key) return null;
      return { body, size: body.byteLength, httpEtag: '"etag-test"', writeHttpMetadata(headers) { headers.set("Content-Type", "text/plain"); } };
    },
  };
  const fetcher = async (_url, options) => {
    policyCalls++;
    assert.equal(options.headers.Authorization, `Bearer ${token}`);
    if (!policyAvailable) throw new Error("policy unavailable");
    return new Response(JSON.stringify({ mode: policyMode }), {
      status: 200,
      headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store", "X-Media-Delivery-Mode": policyMode },
    });
  };
  const waiting = [];
  const ctx = { waitUntil(value) { waiting.push(value); } };
  const env = {
    MEDIA_EDGE_GATEWAY_MODE: "enforce", MEDIA_PUBLIC_ORIGIN: origin, MEDIA_POLICY_URL: policyURL,
    MEDIA_EDGE_POLICY_TOKEN: token, MEDIA_POLICY_CACHE_TTL_SECONDS: "5", MEDIA_BUCKET: bucket,
  };
  return {
    gateway: createMediaGateway({ cache, fetcher }), cache, ctx, env,
    request(method = "GET", suffix = key) { return new Request(`${origin}/${suffix}`, { method }); },
    setPolicy(mode, available = true) { policyMode = mode; policyAvailable = available; cache.clearPolicy(); },
    counts() { return { policyCalls, objectCalls }; },
    async settle() { await Promise.all(waiting.splice(0)); },
  };
}

test("normal mode reads R2 once and serves the object cache only after policy evaluation", async () => {
  const item = fixture();
  const first = await item.gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(first.status, 200);
  assert.equal(first.headers.get("content-type"), "image/webp");
  assert.equal(first.headers.get("x-media-delivery-mode"), "normal");
  assert.deepEqual(new Uint8Array(await first.arrayBuffer()), new Uint8Array([1, 2, 3, 4]));
  await item.settle();

  const second = await item.gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(second.headers.get("x-media-delivery-mode"), "normal");
  assert.deepEqual(item.counts(), { policyCalls: 1, objectCalls: 1 });
});

test("a policy transition blocks an already cached object and returns a no-store default image", async () => {
  const item = fixture();
  await item.gateway.fetch(item.request(), item.env, item.ctx);
  await item.settle();
  item.setPolicy("default_only");

  const stopped = await item.gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(stopped.status, 200);
  assert.match(stopped.headers.get("content-type"), /^image\/svg\+xml/);
  assert.equal(stopped.headers.get("cache-control"), "private, no-store");
  assert.equal(stopped.headers.get("cloudflare-cdn-cache-control"), "no-store");
  assert.equal(stopped.headers.get("x-media-delivery-mode"), "default_only");
  assert.match(await stopped.text(), /作品图片暂不可用/);
  assert.deepEqual(item.counts(), { policyCalls: 2, objectCalls: 1 });
});

test("policy failure is cached briefly as fail-closed and never reads R2", async () => {
  const item = fixture();
  item.setPolicy("normal", false);
  const failed = await item.gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(failed.status, 200);
  assert.equal(failed.headers.get("x-media-delivery-mode"), "default_only");
  assert.equal(failed.headers.get("x-media-policy-available"), "0");
  const repeated = await item.gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(repeated.headers.get("x-media-delivery-mode"), "default_only");
  assert.deepEqual(item.counts(), { policyCalls: 1, objectCalls: 0 });
});

test("oversized or contradictory policy responses fail closed before R2", async () => {
  const item = fixture();
  const gateway = createMediaGateway({
    cache: new MemoryCache(),
    fetcher: async () => new Response(JSON.stringify({ mode: "normal", padding: "x".repeat(100) }), {
      status: 200,
      headers: { "Content-Type": "application/json", "Cache-Control": "no-store", "X-Media-Delivery-Mode": "normal" },
    }),
  });
  const response = await gateway.fetch(item.request(), item.env, item.ctx);
  assert.equal(response.headers.get("x-media-delivery-mode"), "default_only");
  assert.equal(response.headers.get("x-media-policy-available"), "0");
  assert.deepEqual(item.counts(), { policyCalls: 0, objectCalls: 0 });
});

test("missing R2 objects and origin errors use defaults without caching the fallback", async () => {
  const item = fixture();
  const missingKey = key.replace("cover-w640.webp", "cover-w320.webp");
  const missing = await item.gateway.fetch(item.request("GET", missingKey), item.env, item.ctx);
  assert.equal(missing.headers.get("x-media-fallback-reason"), "missing");
  assert.equal(missing.headers.get("cache-control"), "private, no-store");
});

test("path, host, query and method boundaries are closed before policy or storage access", async () => {
  const item = fixture();
  const invalid = [
    new Request("https://other.test.example/" + key),
    item.request("GET", key + "?variant=1"),
    item.request("GET", "media-master/private.webp"),
    item.request("GET", key.replace("/works/", "/performers/")),
  ];
  for (const request of invalid) assert.equal((await item.gateway.fetch(request, item.env, item.ctx)).status, 404);
  const method = await item.gateway.fetch(item.request("POST"), item.env, item.ctx);
  assert.equal(method.status, 405);
  assert.equal(method.headers.get("allow"), "GET, HEAD");
  assert.deepEqual(item.counts(), { policyCalls: 0, objectCalls: 0 });
});

test("HEAD has the same policy and metadata without returning an image body", async () => {
  const item = fixture();
  const response = await item.gateway.fetch(item.request("HEAD"), item.env, item.ctx);
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-length"), "4");
  assert.equal((await response.arrayBuffer()).byteLength, 0);
});

test("invalid deployment configuration is no-store and never falls through to R2", async () => {
  const item = fixture();
  const response = await item.gateway.fetch(item.request(), { ...item.env, MEDIA_EDGE_GATEWAY_MODE: "off" }, item.ctx);
  assert.equal(response.status, 503);
  assert.equal(response.headers.get("cache-control"), "private, no-store");
  assert.deepEqual(item.counts(), { policyCalls: 0, objectCalls: 0 });

  const recursive = await item.gateway.fetch(item.request(), { ...item.env, MEDIA_POLICY_URL: `${origin}/edge/v1/media-delivery-policy` }, item.ctx);
  assert.equal(recursive.status, 503);
});
