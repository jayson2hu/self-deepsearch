import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";
import vm from "node:vm";

const source = await readFile(new URL("../../apps/display-web/lib/ad-policy.ts", import.meta.url), "utf8");
const output = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const module = { exports: {} };
vm.runInNewContext(output, { module, exports: module.exports, URL, Date, process: { env: {} } }, { filename: "ad-policy.ts" });
const policy = module.exports;

const now = new Date("2026-09-16T08:00:00Z");
function creative(overrides = {}) {
  return {
    slot: "banner:正文后横幅",
    placement: "banner",
    title: "合规广告素材",
    sponsor: "示例广告主",
    image_url: "https://assets.ads.example/banner.webp",
    image_alt: "示例广告横幅",
    target_url: "https://offer.ads.example/campaign",
    width: 960,
    height: 90,
    starts_at: "2026-09-16T00:00:00Z",
    ends_at: "2026-09-17T00:00:00Z",
    ...overrides,
  };
}
function environment(entries = [creative()], overrides = {}) {
  return {
    NODE_ENV: "test",
    DISPLAY_ADS_ENABLED: "true",
    DISPLAY_AD_ASSET_HOSTS: "assets.ads.example",
    DISPLAY_AD_TARGET_HOSTS: "offer.ads.example",
    DISPLAY_AD_SLOTS_JSON: JSON.stringify(entries),
    ...overrides,
  };
}

test("ads default to empty and disabled configuration never renders a shell", () => {
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", {}, now).state, "empty");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment(), now).state, "ready");
  assert.equal(policy.resolveAdSlot("rail", "左侧展示位", environment(), now).state, "empty");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([], { DISPLAY_ADS_ENABLED: "false" }), now).state, "empty");
});

test("ready requires one active, exact-size creative and allowlisted HTTPS URLs", () => {
  const ready = policy.resolveAdSlot("banner", "正文后横幅", environment(), now);
  assert.equal(ready.state, "ready");
  assert.equal(ready.creative.targetUrl, "https://offer.ads.example/campaign");
  assert.equal(ready.creative.width, 960);

  const invalidEntries = [
    creative({ width: 959 }),
    creative({ starts_at: "2026-09-16", ends_at: "2026-09-17" }),
    creative({ image_url: "javascript:alert(1)" }),
    creative({ image_url: "https://evil.example/banner.webp" }),
    creative({ target_url: "http://offer.ads.example/campaign" }),
    creative({ target_url: "https://user:pass@offer.ads.example/campaign" }),
    creative({ title: "<script>alert(1)</script>" }),
    creative({ image_alt: "unsafe\nalt" }),
    creative({ tracking_script: "https://evil.example/tracker.js" }),
    creative({ ends_at: "2026-09-16T08:00:00Z" }),
  ];
  for (const entry of invalidEntries) assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([entry]), now).state, "error");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([creative(), creative()]), now).state, "error");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([], { DISPLAY_AD_SLOTS_JSON: "{" }), now).state, "error");
});

test("reviewed local assets are allowed only under the dedicated ads path", () => {
  const local = creative({ image_url: "/ads/reviewed-banner.webp" });
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([local], { DISPLAY_AD_ASSET_HOSTS: "" }), now).state, "ready");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([creative({ image_url: "/default-work.svg" })]), now).state, "error");
  assert.equal(policy.resolveAdSlot("banner", "正文后横幅", environment([creative({ image_url: "/ads/../default-work.svg" })]), now).state, "error");
});

test("preview fixture requires explicit non-production protection", () => {
  assert.equal(policy.resolveAdSlot("rail", "左侧展示位", { NODE_ENV: "test" }, now, true).state, "error");
  const preview = policy.resolveAdSlot("rail", "左侧展示位", { NODE_ENV: "test", DISPLAY_AD_PREVIEW_FIXTURE: "enabled" }, now, true);
  assert.equal(preview.state, "preview");
  assert.equal(preview.creative.targetUrl, null);
  assert.equal(policy.resolveAdSlot("rail", "左侧展示位", { NODE_ENV: "production", DISPLAY_AD_PREVIEW_FIXTURE: "enabled" }, now, true).state, "error");
});

test("AdSlot has no auth split, script injection or arbitrary HTML path", async () => {
  const component = await readFile(new URL("../../apps/display-web/components/ad-slot.tsx", import.meta.url), "utf8");
  assert.doesNotMatch(component, /session|authenticated|cookie/i);
  assert.doesNotMatch(component, /dangerouslySetInnerHTML|<script/i);
  assert.match(component, /rel="noopener noreferrer sponsored"/);
  assert.match(component, /!\("creative" in result\)/);
});
