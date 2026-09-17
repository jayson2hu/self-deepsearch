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

test("stable slot codes separate page placement from translated labels", () => {
  const entry = creative({ slot: "home.after_latest" });
  assert.equal(policy.resolveAdSlot("banner", "最新内容后", environment([entry]), now, false, "home.after_latest").state, "ready");
  assert.equal(policy.resolveAdSlot("banner", "最新内容后", environment([entry]), now, false, "work.after_summary").state, "empty");
  assert.equal(policy.resolveAdSlot("banner", "最新内容后", environment([entry]), now, false, "<unsafe>").state, "error");
});

test("AdSlot has no auth split, script injection or arbitrary HTML path", async () => {
  const component = `${await readFile(new URL("../../apps/display-web/components/ad-slot.tsx", import.meta.url), "utf8")}\n${await readFile(new URL("../../apps/display-web/components/live-ad-slot.tsx", import.meta.url), "utf8")}`;
  assert.doesNotMatch(component, /session|authenticated|cookie/i);
  assert.doesNotMatch(component, /dangerouslySetInnerHTML|<script/i);
  assert.match(component, /rel="noopener noreferrer sponsored"/);
  assert.match(component, /data-ad-slot/);
  assert.match(component, /setInterval\(refresh, 30_000\)/);
  assert.match(component, /setCreative\(null\)/);
});

test("backend creatives allow no-click ads and enforce device scope", () => {
  const noClick = creative({ target_url: null, device_scope: "mobile" });
  const result = policy.resolveAdCreative(noClick, "banner", environment(), now, noClick.slot);
  assert.equal(result.state, "ready");
  assert.equal(result.creative.targetUrl, null);
  assert.equal(result.creative.deviceScope, "mobile");
  assert.equal(policy.resolveAdCreative(creative({ device_scope: "television" }), "banner", environment(), now).state, "error");
});

test("page governance keeps search and account-wide footer free of ads", async () => {
  const [search, footer, home, work, css] = await Promise.all([
    readFile(new URL("../../apps/display-web/app/search/page.tsx", import.meta.url), "utf8"),
    readFile(new URL("../../apps/display-web/components/site-footer.tsx", import.meta.url), "utf8"),
    readFile(new URL("../../apps/display-web/app/page.tsx", import.meta.url), "utf8"),
    readFile(new URL("../../apps/display-web/app/works/[slug]/page.tsx", import.meta.url), "utf8"),
    readFile(new URL("../../apps/display-web/app/globals.css", import.meta.url), "utf8"),
  ]);
  assert.doesNotMatch(search, /AdSlot|顶部横幅|左侧展示位|右侧展示位/);
  assert.doesNotMatch(footer, /AdSlot|footer-ad/);
  assert.match(home, /slot="home\.after_latest"/);
  assert.doesNotMatch(home, /ad-rail--left|信息流展示位/);
  assert.match(work, /slot="work\.after_summary"/);
  assert.doesNotMatch(work, /ad-rail--left|顶部横幅/);
  assert.match(css, /@media \(max-width: 1199px\)[\s\S]*?\.ad-rail \{ display: none; \}/);
});

test("catalog lists request one in-feed ad after eight natural items", async () => {
  const paths = ["latest", "recent", "popular", "performers", "studios"];
  for (const path of paths) {
    const source = await readFile(new URL(`../../apps/display-web/app/${path}/page.tsx`, import.meta.url), "utf8");
    assert.match(source, /length >= 8 \? <AdSlot/);
    assert.match(source, new RegExp(`slot="${path}\\.after8"`));
    assert.match(source, new RegExp(`slot="${path}\\.desktop_right"`));
    assert.equal((source.match(/placement="infeed"/g) ?? []).length, 1);
  }
});

test("list navigation never reports a detail page view", async () => {
  const listPaths = ["latest", "recent", "popular", "performers", "studios", "search"];
  for (const path of listPaths) {
    const source = await readFile(new URL(`../../apps/display-web/app/${path}/page.tsx`, import.meta.url), "utf8");
    assert.doesNotMatch(source, /PageViewReporter|metrics\/page-view/);
  }
  const details = ["works/[slug]", "performers/[slug]", "studios/[slug]"];
  for (const path of details) {
    const source = await readFile(new URL(`../../apps/display-web/app/${path}/page.tsx`, import.meta.url), "utf8");
    assert.match(source, /<PageViewReporter contentType=/);
  }
});
