import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { canonicalWithCursor, seoReleaseEnabled } from "../../apps/display-web/lib/seo-policy.ts";
import { maximumSitemapBytes, sitemapPageSize, sitemapShardPaths, sitemapWithinLimits } from "../../apps/display-web/lib/sitemap-policy.ts";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const read = (relativePath) => readFile(path.join(repositoryRoot, relativePath), "utf8");

test("SEO release gate is fail-closed and requires the formal HTTPS origin", () => {
  const ready = { APP_ENV: "production", SEO_INDEXING_ENABLED: "true", SEO_CONTENT_POLICY_READY: "true", SITE_URL: "https://display.example.com" };
  assert.equal(seoReleaseEnabled(ready), true);
  for (const environment of [
    { ...ready, APP_ENV: "test" },
    { ...ready, SEO_INDEXING_ENABLED: "false" },
    { ...ready, SEO_CONTENT_POLICY_READY: "false" },
    { ...ready, SITE_URL: "http://display.example.com" },
    { ...ready, SITE_URL: "https://127.0.0.1" },
    { ...ready, SITE_URL: "https://display.example.com/path" },
    { ...ready, SITE_URL: "" },
  ]) assert.equal(seoReleaseEnabled(environment), false);
});

test("legal cursor pages keep a distinct canonical", () => {
  assert.equal(canonicalWithCursor("/studios", ""), "/studios");
  assert.equal(canonicalWithCursor("/studios", "next+/="), "/studios?cursor=next%2B%2F%3D");
});

test("50,001 sitemap records split without truncating the second shard", () => {
  assert.equal(sitemapPageSize, 50000);
  assert.deepEqual(sitemapShardPaths({ works: 50001, performers: 0, studios: 1 }), [
    "/sitemaps/pages", "/sitemaps/works/1", "/sitemaps/works/2", "/sitemaps/studios/1",
  ]);
  assert.equal(sitemapWithinLimits(50000, maximumSitemapBytes), true);
  assert.equal(sitemapWithinLimits(50001, maximumSitemapBytes), false);
  assert.equal(sitemapWithinLimits(1, maximumSitemapBytes + 1), false);
});

test("robots, HTTP preflight, metadata origin, and sitemap routes share fail-closed mechanics", async () => {
  const [layout, robots, proxy, sitemapIndex, sitemapResponse, route] = await Promise.all([
    read("apps/display-web/app/layout.tsx"),
    read("apps/display-web/app/robots.ts"),
    read("apps/display-web/proxy.ts"),
    read("apps/display-web/app/sitemap.xml/route.ts"),
    read("apps/display-web/lib/sitemap-response.ts"),
    read("apps/display-web/app/sitemaps/[entity]/[page]/route.ts"),
  ]);
  assert.match(layout, /metadataBase: siteURL\(\)/);
  assert.match(robots, /disallow: "\/"/);
  assert.doesNotMatch(robots, /disallow: \[[^\]]*\/search/);
  assert.match(proxy, /status: 503/);
  assert.match(proxy, /status: 400/);
  assert.match(proxy, /X-Robots-Tag/);
  assert.match(proxy, /\/api\/v1\/works\/latest/);
  assert.match(sitemapIndex, /seoReleaseEnabled/);
  assert.match(sitemapResponse, /response\.total_pages/);
  assert.match(sitemapResponse, /home\.degraded \|\| latest\.degraded/);
  assert.match(route, /entitySitemapResponse/);
});

test("visible breadcrumbs and structured BreadcrumbList are delivered together", async () => {
  const component = await read("apps/display-web/components/breadcrumbs.tsx");
  assert.match(component, /"@type": "BreadcrumbList"/);
  assert.match(component, /aria-label="面包屑"/);
  for (const page of ["works/[slug]", "performers/[slug]", "studios/[slug]"]) {
    assert.match(await read(`apps/display-web/app/${page}/page.tsx`), /<Breadcrumbs items=/);
  }
});

test("sitemap API contract exposes deterministic pagination totals", async () => {
  const [openapi, store] = await Promise.all([
    read("packages/api-contracts/openapi.yaml"),
    read("services/platform-api/internal/database/store.go"),
  ]);
  for (const field of ["page", "page_size", "total_items", "total_pages"]) assert.match(openapi, new RegExp(field));
  assert.match(store, /ORDER BY entity_type, canonical_slug/);
  assert.match(store, /OFFSET \$2 LIMIT \$3/);
  assert.match(store, /public_published_works/);
});
