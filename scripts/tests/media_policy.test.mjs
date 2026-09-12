import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../../apps/display-web/lib/media-policy.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { mediaProjection, mediaDefaultOnly, mediaContentSecurityPolicy } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString("base64")}`);

const runtimeSource = (await readFile(new URL("../../apps/display-web/lib/media-runtime.ts", import.meta.url), "utf8"))
  .replace('import { mediaDefaultOnly } from "./media-policy";', "const mediaDefaultOnly = () => globalThis.__manualMediaDefaultOnly === true;");
const runtimeCompiled = ts.transpileModule(runtimeSource, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const { mediaDeliveryDynamicEnabled, readMediaRuntimePolicy } = await import(`data:text/javascript;base64,${Buffer.from(runtimeCompiled).toString("base64")}`);

test("media projection is immutable, covers nested catalog data and leaves ordinary URLs/text", () => {
  const previous = process.env.MEDIA_DELIVERY_MODE;
  try {
    const original = { image_url: "remote", image: { url: "remote", renditions: [{ url: "remote" }] }, images: [{ url: "remote" }], sections: [{ items: [{ image_url: "remote", title: "text" }] }], evidence_url: "retain" };
    const baseline = structuredClone(original);
    process.env.MEDIA_DELIVERY_MODE = "normal";
    assert.equal(mediaProjection(original), original);
		const dynamicResult = mediaProjection(original, true);
		assert.deepEqual(dynamicResult, { image_url: null, image: null, images: [], sections: [{ items: [{ image_url: null, title: "text" }] }], evidence_url: "retain" });
    process.env.MEDIA_DELIVERY_MODE = "default_only";
    const result = mediaProjection(original);
    assert.deepEqual(result, { image_url: null, image: null, images: [], sections: [{ items: [{ image_url: null, title: "text" }] }], evidence_url: "retain" });
    assert.deepEqual(original, baseline);
    assert.equal(mediaDefaultOnly("typo"), true);
  } finally {
    if (previous === undefined) delete process.env.MEDIA_DELIVERY_MODE;
    else process.env.MEDIA_DELIVERY_MODE = previous;
  }
});

test("stop CSP removes only external img access and keeps existing protections/Turnstile", () => {
  const normal = mediaContentSecurityPolicy(false);
  const stopped = mediaContentSecurityPolicy(true);
  assert.equal(normal.replace("img-src 'self' data: blob: https:", "img-src 'self' data: blob:"), stopped);
  for (const directive of ["frame-ancestors 'none'", "object-src 'none'", "form-action 'self'", "frame-src https://challenges.cloudflare.com"]) assert.ok(stopped.includes(directive));
});

test("dynamic runtime policy is strict, manual-first and fail-closed", async () => {
  const previousMode = process.env.MEDIA_DELIVERY_DYNAMIC_MODE;
  const previousFetch = globalThis.fetch;
  try {
    let calls = 0;
    globalThis.__manualMediaDefaultOnly = false;
    process.env.MEDIA_DELIVERY_DYNAMIC_MODE = "off";
    globalThis.fetch = async () => { calls += 1; throw new Error("must not fetch"); };
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: false, available: true });
    assert.equal(calls, 0);

    process.env.MEDIA_DELIVERY_DYNAMIC_MODE = "enforce";
    const response = (mode, headers = {}) => new Response(JSON.stringify({ mode }), { status: 200, headers: {
      "content-type": "application/json; charset=utf-8", "cache-control": "no-store", "x-media-delivery-mode": mode, ...headers,
    } });
    globalThis.fetch = async () => response("normal");
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: false, available: true });
    globalThis.fetch = async () => response("default_only");
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: true, available: true });

    globalThis.fetch = async () => response("normal", { "x-media-delivery-mode": "default_only" });
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: true, available: false });
    globalThis.fetch = async () => { throw new Error("platform API unavailable"); };
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: true, available: false });
    assert.equal(mediaDeliveryDynamicEnabled("typo"), true);

    globalThis.__manualMediaDefaultOnly = true;
    calls = 0;
    globalThis.fetch = async () => { calls += 1; return response("normal"); };
    assert.deepEqual(await readMediaRuntimePolicy(), { defaultOnly: true, available: true });
    assert.equal(calls, 0);
  } finally {
    delete globalThis.__manualMediaDefaultOnly;
    globalThis.fetch = previousFetch;
    if (previousMode === undefined) delete process.env.MEDIA_DELIVERY_DYNAMIC_MODE;
    else process.env.MEDIA_DELIVERY_DYNAMIC_MODE = previousMode;
  }
});
