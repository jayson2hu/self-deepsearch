import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import { workMetadataDescription } from "../../apps/display-web/lib/seo-content.ts";
import { externalImagePreviewAllowed, resolvePublicSEOProfile, seoReleaseEnabled } from "../../apps/display-web/lib/seo-policy.ts";
import { sitemapEntityPath, sitemapPageSize, sitemapShardPaths, sitemapWithinLimits } from "../../apps/display-web/lib/sitemap-policy.ts";

const production = { APP_ENV: "production", SEO_INDEXING_ENABLED: "true", SEO_CONTENT_POLICY_READY: "true", SITE_URL: "https://catalog.example.com" };

test("SEO gate rejects every IP representation and internal origin", () => {
  for (const host of ["127.1", "2130706433", "0x7f000001", "10.0.0.1", "192.168.1.2", "169.254.169.254", "8.8.8.8", "[::1]", "[2001:db8::1]", "localhost", "catalog.localhost", "catalog.local", "catalog.internal", "catalog.test", "catalog.invalid", "catalog.example", "catalog.home.arpa", "catalog", "catalog.example.com.", "-bad.example.com"]) {
    assert.equal(seoReleaseEnabled({ ...production, SITE_URL: `https://${host}` }), false, host);
  }
  assert.equal(seoReleaseEnabled(production), true);
  assert.equal(seoReleaseEnabled({ ...production, SITE_URL: "https://home.arpa" }), false);
  for (const origin of ["https://user:secret@catalog.example.com", "https://catalog.example.com/a", "https://catalog.example.com/?token=x", "https://catalog.example.com/#x"]) {
    assert.equal(seoReleaseEnabled({ ...production, SITE_URL: origin }), false);
  }
});

test("inconsistent public profiles cannot enable a sitemap or an adult image preview", () => {
  const base = { indexable: true, sitemap_eligible: true, adult_rating: null, image_preview_mode: "standard" };
  assert.equal(resolvePublicSEOProfile({ ...base, adult_rating: "adult" }).image_preview_mode, "none");
  const blocked = resolvePublicSEOProfile({ ...base, indexable: false });
  assert.equal(blocked.sitemap_eligible, false);
  assert.equal(blocked.image_preview_mode, "none");
  assert.deepEqual(resolvePublicSEOProfile(base), base);
});

test("social and structured image previews require both the production gate and reviewed policy", () => {
  const previous = Object.fromEntries(Object.keys(production).map((key) => [key, process.env[key]]));
  const profile = { indexable: true, sitemap_eligible: true, adult_rating: null, image_preview_mode: "standard" };
  try {
    Object.assign(process.env, production);
    assert.equal(externalImagePreviewAllowed(profile), true);
    assert.equal(externalImagePreviewAllowed({ ...profile, adult_rating: "adult" }), false);
    assert.equal(externalImagePreviewAllowed({ ...profile, indexable: false }), false);
    assert.equal(externalImagePreviewAllowed({ ...profile, image_preview_mode: "none" }), false);
    process.env.APP_ENV = "test";
    assert.equal(externalImagePreviewAllowed(profile), false);
  } finally {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete process.env[key]; else process.env[key] = value;
    }
  }
});

test("sitemap counts and sizes reject nonfinite, negative, fractional and unsafe numbers before allocation", () => {
  for (const number of [NaN, Infinity, -Infinity, -1, 1.5, Number.MAX_SAFE_INTEGER + 1, "1"]) {
    assert.throws(() => sitemapShardPaths({ works: number, performers: 0, studios: 0 }), RangeError);
    assert.equal(sitemapWithinLimits(number, 1), false);
    if (typeof number === "number") assert.equal(sitemapWithinLimits(1, number), false);
  }
  assert.equal(sitemapWithinLimits(0, ""), true);
  assert.equal(sitemapWithinLimits(1, "汉字"), true);
  assert.equal(sitemapShardPaths({ works: (sitemapPageSize - 1) * sitemapPageSize, performers: 0, studios: 0 }).length, sitemapPageSize);
  assert.throws(() => sitemapShardPaths({ works: sitemapPageSize * sitemapPageSize, performers: 0, studios: 0 }), RangeError);
  assert.throws(() => sitemapShardPaths({ works: (sitemapPageSize - 1) * sitemapPageSize, performers: 1, studios: 0 }), RangeError);
});

test("sitemap paths cannot accept URL, parent path, query or fragment injection", () => {
  assert.equal(sitemapEntityPath("performers", "sample-person-1"), "/performers/sample-person-1");
  for (const slug of ["", "../private", "../studios/a", "a?token=x", "a#x", "//external.example/a", "a/b", "a%2fb", "a&b", undefined, null, 123]) {
    assert.throws(() => sitemapEntityPath("works", slug), TypeError);
  }
});

test("work descriptions are unique, bounded, factual and never invent missing fields", () => {
  const source = { code: "SD-001", title: "A fictional record", release_date: "2026-01-01", studio_name: "Synthetic studio", performers: [{ name: "Sample performer" }] };
  const description = workMetadataDescription(source);
  for (const fact of [source.code, source.title, source.release_date, source.studio_name, source.performers[0].name]) assert.ok(description.includes(fact));
  assert.notEqual(description, workMetadataDescription({ ...source, title: "Another record" }));
  const missing = workMetadataDescription({ code: "SD-002", title: "No other facts", performers: [] });
  assert.doesNotMatch(missing, /undefined|null|待补充|发行日期|厂牌|人物/);
  const long = workMetadataDescription({ ...source, title: "😀".repeat(200) });
  assert.equal(Array.from(long).length, 180);
  assert.ok(long.endsWith("…"));
});

test("both detail templates gate Open Graph and JSON-LD images, without removing in-page images", async () => {
  for (const entity of ["works", "performers"]) {
    const source = await readFile(new URL(`../../apps/display-web/app/${entity}/[slug]/page.tsx`, import.meta.url), "utf8");
    assert.ok((source.match(/externalImagePreviewAllowed\(seo\)/g) ?? []).length >= 2);
    assert.match(source, /<SafeImage/);
  }
  const sitemap = await readFile(new URL("../../apps/display-web/app/sitemap.xml/route.ts", import.meta.url), "utf8");
  assert.match(sitemap, /sitemapWithinLimits\(paths.length, xml\)/);
  assert.match(sitemap, /Invalid sitemap source counts/);
});
