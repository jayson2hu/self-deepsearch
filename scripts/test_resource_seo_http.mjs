#!/usr/bin/env node
// Real production Next HTTP responses, synthetic catalog only. No live DB/media.
import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import path from "node:path";
import { createCatalog, createIsolatedStandalone, removeIsolatedRuntime, startCatalog } from "./release_a_cache_contract.mjs";
import { findFreePort, REPOSITORY_ROOT, standalonePaths, startChild, stopChild, waitForHTTP } from "./release_a_frontend_smoke.mjs";

const profile = { indexable: true, sitemap_eligible: true, adult_rating: null, image_preview_mode: "standard" };
const cases = [
  { name: "approved-preview", preview: true },
  { name: "adult-preview-denied", seo: { ...profile, adult_rating: "adult" }, adult: true },
  { name: "unknown-preview-denied", seo: null, noindex: true },
  { name: "ineligible-preview-denied", seo: { ...profile, indexable: false }, noindex: true },
  { name: "staging-gate", environment: "test", noindex: true, sitemapStatus: 404 },
  { name: "ip-origin-gate", origin: "https://192.168.1.10", noindex: true, sitemapStatus: 404 },
  { name: "invalid-count", total: -1, indexStatus: 503, preview: true },
  { name: "excessive-index", total: 2_500_000_000, indexStatus: 503, preview: true },
  { name: "invalid-slug", slug: "../account?private=1", leafStatus: 503, preview: true },
];

function catalogFor(scenario) {
  const catalog = createCatalog();
  const original = catalog.respond;
  catalog.respond = (request, response) => {
    let status, headers;
    original(request, {
      writeHead(code, values) { status = code; headers = values; },
      end(raw) {
        const body = JSON.parse(raw);
        const url = new URL(request.url, "http://127.0.0.1");
        if (url.pathname === `/api/v1/works/${catalog.slug}` && status === 200) {
          body.seo = Object.hasOwn(scenario, "seo") ? scenario.seo : profile;
          // Deliberately inconsistent flag tests the page-level policy boundary.
          body.images = [{ url: "https://media.example.invalid/synthetic.webp", width: 960, height: 600,
            rendition: "w960", mime_type: "image/webp", external_preview_allowed: true }];
        }
        if (url.pathname === "/api/v1/site/sitemap") {
          body.page_size = Number(url.searchParams.get("page_size"));
          if (Object.hasOwn(scenario, "total")) body.total_items = scenario.total;
          if (scenario.slug && body.items.length) body.items[0].slug = scenario.slug;
        }
        response.writeHead(status, headers);
        response.end(JSON.stringify(body));
      },
    });
  };
  return catalog;
}

async function read(origin, pathname) {
  const response = await fetch(origin + pathname, { redirect: "manual", signal: AbortSignal.timeout(15_000) });
  return { status: response.status, headers: response.headers, body: await response.text() };
}

const source = standalonePaths("display-web");
await fs.access(source.server);
const results = [];
for (const scenario of cases) {
  let isolated, upstream, child;
  try {
    isolated = await createIsolatedStandalone(source);
    const catalog = catalogFor(scenario);
    upstream = await startCatalog(catalog);
    const port = await findFreePort();
    child = startChild({ name: `seo-${scenario.name}`, server: isolated.server, port,
      apiURL: `http://127.0.0.1:${upstream.port}`, repositoryRoot: REPOSITORY_ROOT,
      extraEnv: { APP_ENV: scenario.environment ?? "production", SITE_URL: scenario.origin ?? "https://seo.example.com",
        SEO_INDEXING_ENABLED: "true", SEO_CONTENT_POLICY_READY: "true", MEDIA_DELIVERY_DYNAMIC_MODE: "off", MEDIA_DELIVERY_MODE: "normal",
        TURNSTILE_BYPASS: "false", VISITOR_METRICS_ENABLED: "false" },
    });
    await waitForHTTP(child, "/robots.txt", 45_000);
    const origin = `http://127.0.0.1:${port}`;
    const work = await read(origin, `/works/${catalog.slug}`);
    assert.equal(work.status, 200, scenario.name);
    assert.ok(work.body.includes(`<meta name="description" content="TEST-CACHE ${catalog.before}`), "Rendered metadata must contain this record's title");
    const documents = [...work.body.matchAll(/<script type="application\/ld\+json">([\s\S]*?)<\/script>/g)].map((match) => JSON.parse(match[1]));
    const document = documents.find((item) => item["@type"] === "CreativeWork");
    assert.ok(document, "Real HTTP response must include valid CreativeWork JSON-LD");
    assert.equal(/<meta property="og:image"/.test(work.body), Boolean(scenario.preview));
    assert.equal(Boolean(document.image?.length), Boolean(scenario.preview));
    assert.equal(/<meta name="rating" content="adult"/.test(work.body), Boolean(scenario.adult));
    assert.ok(work.body.includes('src="https://media.example.invalid/synthetic.webp"'), "In-page image must remain independent of external-preview policy");
    if (scenario.noindex) assert.match(work.body, /<meta name="robots" content="noindex/);
    const index = await read(origin, "/sitemap.xml");
    const leaf = await read(origin, "/sitemaps/works/1");
    assert.equal(index.status, scenario.sitemapStatus ?? scenario.indexStatus ?? 200, "sitemap index status");
    assert.equal(leaf.status, scenario.sitemapStatus ?? scenario.leafStatus ?? 200, "sitemap leaf status");
    for (const response of [index, leaf]) {
      if (response.status >= 400) assert.equal(response.headers.get("cache-control"), "no-store");
      if (response.status === 503) assert.equal(response.headers.get("retry-after"), "60");
    }
    if (scenario.sitemapStatus === 404) {
      assert.match(work.headers.get("x-robots-tag"), /noindex/);
      assert.match((await read(origin, "/robots.txt")).body, /Disallow: \//);
    }
    results.push({ name: scenario.name, status: "passed" });
    console.log(`PASS ${scenario.name}`);
  } finally {
    if (child) await stopChild(child);
    if (upstream) {
      upstream.server.closeAllConnections();
      await new Promise((resolve) => upstream.server.close(resolve));
    }
    if (isolated) await removeIsolatedRuntime(isolated.runtimeRoot);
  }
}
const output = path.join(REPOSITORY_ROOT, ".cache/resource-audit/seo-http-result.json");
await fs.mkdir(path.dirname(output), { recursive: true });
await fs.writeFile(output, JSON.stringify({ status: "passed", completed: new Date().toISOString(), cases: results,
  scope: "Production Next.js HTTP, isolated copies and synthetic API; no public indexing or production data." }, null, 2) + "\n");
console.log(`Passed ${results.length} isolated SEO HTTP scenarios`);
