import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import {
  assertHidden, assertMediaState, assertVisible, cacheEvidencePath, createCatalog, createIsolatedStandalone,
  parseCacheArgs, removeIsolatedRuntime, startCatalog,
} from "../release_a_cache_contract.mjs";
import { standalonePaths } from "../release_a_frontend_smoke.mjs";

test("isolated standalone uses independent writable copies and guarded cleanup", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "cache-isolation-test-"));
  let isolated;
  try {
    const source = standalonePaths("display-web", root);
    await fs.mkdir(path.dirname(source.server), { recursive: true });
    await fs.mkdir(source.sourceStatic, { recursive: true });
    await fs.mkdir(source.sourcePublic, { recursive: true });
    await fs.writeFile(source.server, "original server");
    await fs.writeFile(path.join(source.sourceStatic, "test.css"), "original css");
    await fs.writeFile(path.join(source.sourcePublic, "default.svg"), "original image");
    const cachedPage = path.join(path.dirname(source.server), ".next", "server", "index.html");
    const cachedFetch = path.join(path.dirname(source.server), ".next", "cache", "fetch-cache", "poison");
    await fs.mkdir(path.dirname(cachedPage), { recursive: true });
    await fs.mkdir(path.dirname(cachedFetch), { recursive: true });
    await fs.writeFile(cachedPage, "original cached page");
    await fs.writeFile(cachedFetch, "stale synthetic catalog");
    isolated = await createIsolatedStandalone(source, root);
    const isolatedPage = path.join(path.dirname(isolated.server), ".next", "server", "index.html");
    const isolatedFetch = path.join(path.dirname(isolated.server), ".next", "cache", "fetch-cache", "poison");
    await fs.writeFile(isolatedPage, "mutated test page");
    assert.equal(await fs.readFile(cachedPage, "utf8"), "original cached page");
    assert.equal(await fs.readFile(cachedFetch, "utf8"), "stale synthetic catalog");
    await assert.rejects(fs.access(isolatedFetch));
    assert.equal(await fs.readFile(isolated.server, "utf8"), "original server");
    await assert.rejects(removeIsolatedRuntime(source.appRoot, root), /拒绝清理/);
    await removeIsolatedRuntime(isolated.runtimeRoot, root);
    await assert.rejects(fs.access(isolated.server));
    isolated = null;
  } finally {
    if (isolated) await removeIsolatedRuntime(isolated.runtimeRoot, root);
    await fs.rm(root, { recursive: true, force: true });
  }
});

test("cache runner rejects unsupported or incomplete arguments", () => {
  assert.equal(parseCacheArgs([]).build, false);
  assert.deepEqual(parseCacheArgs(["--build", "--output", "docs/evidence/test.json"]), {
    build: true, help: false, output: "docs/evidence/test.json",
  });
  for (const args of [["--output"], ["--output", "--build"], ["--url", "https://example.test"], ["--skip-go"]]) {
    assert.throws(() => parseCacheArgs(args));
  }
});

test("cache evidence stays within a direct JSON file under docs/evidence", () => {
  const root = path.resolve("cache-contract-test-root");
  assert.equal(cacheEvidencePath("docs/evidence/cache.json", root), path.join(root, "docs", "evidence", "cache.json"));
  for (const output of ["outside.json", "docs/evidence/../escape.json", "docs/evidence/nested/cache.json", "docs/evidence/cache.md"]) {
    assert.throws(() => cacheEvidencePath(output, root));
  }
});

function visiblePages(catalog, title = catalog.before, date = catalog.state.updatedAt) {
  return {
    home: { status: 200, body: `<h2>${title}</h2>` },
    work: { status: 200, body: `<h1>${title}</h1>` },
    sitemap: { status: 200, body: `<loc>http://127.0.0.1/works/${catalog.slug}</loc><lastmod>${date}</lastmod>` },
  };
}

test("visible assertions require HTML heading, revision and URL, not just successful HTTP", () => {
  const catalog = createCatalog();
  const pages = visiblePages(catalog);
  assertVisible(pages, catalog, catalog.before, catalog.state.updatedAt);
  assert.throws(() => assertVisible(pages, catalog, catalog.after, catalog.state.updatedAt));
  assert.throws(() => assertVisible({ ...pages, work: { status: 200, body: `<title>${catalog.before}</title>` } }, catalog, catalog.before, catalog.state.updatedAt));
  assert.throws(() => assertVisible({ ...pages, sitemap: { status: 200, body: "<urlset/>" } }, catalog, catalog.before, catalog.state.updatedAt));
  assert.throws(() => assertVisible({ ...pages, home: { status: 503, body: catalog.before } }, catalog, catalog.before, catalog.state.updatedAt));
});

test("visible assertions reject mixed cached and updated revisions", () => {
  const catalog = createCatalog();
  const pages = visiblePages(catalog, catalog.after);
  pages.home.body += catalog.before;
  assert.throws(() => assertVisible(pages, catalog, catalog.after, catalog.state.updatedAt));
});

test("media assertions require real image markup on both surfaces, not metadata alone", () => {
  const catalog = createCatalog();
  const pages = visiblePages(catalog);
  assertMediaState(pages, catalog, false);
  assert.throws(() => assertMediaState(pages, catalog, true));
  const tag = `<img src="${catalog.imageMarker}cover-w640.webp" alt="synthetic"/>`;
  pages.home.body += tag;
  assert.throws(() => assertMediaState(pages, catalog, true));
  pages.work.body += `<meta content="${catalog.imageMarker}cover-w960.webp"/>`;
  assert.throws(() => assertMediaState(pages, catalog, true));
  pages.work.body += tag;
  assertMediaState(pages, catalog, true);
  assert.throws(() => assertMediaState(pages, catalog, false));
});

test("hidden assertions support streamed notFound but reject stale data, URL or unavailable state", () => {
  const catalog = createCatalog();
  const pages = {
    home: { status: 200, body: "empty" },
    work: { status: 404, body: '<meta name="robots" content="noindex"/><h2>This page could not be found.</h2>' },
    sitemap: { status: 200, body: "<urlset/>" },
  };
  assertHidden(pages, catalog);
  assertHidden({ ...pages, work: { ...pages.work, status: 200 } }, catalog);
  assert.throws(() => assertHidden({ ...pages, work: { status: 200, body: "temporarily unavailable" } }, catalog));
  assert.throws(() => assertHidden({ ...pages, work: { ...pages.work, body: pages.work.body + catalog.after } }, catalog));
  assert.throws(() => assertHidden({ ...pages, sitemap: visiblePages(catalog).sitemap }, catalog));
  assert.throws(() => assertHidden({ ...pages, home: { status: 200, body: `<a href="/works/${catalog.slug}">old link</a>` } }, catalog));
  assert.throws(() => assertHidden({ ...pages, home: { status: 200, body: `<img src="${catalog.imageMarker}cover-w640.webp"/>` } }, catalog));
});

test("media-only synthetic mutation preserves text and revision and supplies responsive renditions", async () => {
  const catalog = createCatalog();
  const upstream = await startCatalog(catalog);
  const get = async (route) => (await fetch(`http://127.0.0.1:${upstream.port}${route}`, { signal: AbortSignal.timeout(2000) })).json();
  try {
    const before = await get(`/api/v1/works/${catalog.slug}`);
    assert.deepEqual(before.images, []);
    catalog.state.hasImage = true;
    const home = await get("/api/v1/site/home");
    const after = await get(`/api/v1/works/${catalog.slug}`);
    const sitemap = await get("/api/v1/site/sitemap?entity_type=work");
    assert.equal(after.title, before.title);
    assert.equal(sitemap.items[0].updated_at, "2026-09-01T00:00:00Z");
    assert.equal(home.sections[0].items[0].image.rendition, "w640");
    assert.equal(after.images[0].rendition, "w960");
    assert.deepEqual(after.images[0].renditions.map((item) => item.width), [320, 640, 960]);
    assert.ok(after.images[0].url.startsWith(catalog.imageMarker));
  } finally {
    upstream.server.closeAllConnections();
    await new Promise((resolve) => upstream.server.close(resolve));
  }
});

test("synthetic catalog exposes only read-only routes and counts actual upstream reads", async () => {
  const catalog = createCatalog();
  const upstream = await startCatalog(catalog);
  const origin = `http://127.0.0.1:${upstream.port}`;
  const get = (route, options) => fetch(origin + route, { ...options, signal: AbortSignal.timeout(2000) });
  try {
    const home = await (await get("/api/v1/site/home")).json();
    assert.equal(home.sections[0].items[0].title, catalog.before);
    catalog.state.title = catalog.after;
    assert.equal((await (await get(`/api/v1/works/${catalog.slug}`)).json()).title, catalog.after);
    assert.equal((await (await get("/api/v1/site/sitemap?entity_type=work")).json()).items.length, 1);
    assert.deepEqual(catalog.reads, { home: 1, work: 1, sitemap: 1 });
    catalog.state.hidden = true;
    const hidden = await get(`/api/v1/works/${catalog.slug}`);
    assert.equal(hidden.status, 404);
    await hidden.text();
    assert.equal((await (await get("/api/v1/site/sitemap?entity_type=work")).json()).items.length, 0);
    const denied = await get("/api/v1/site/home", { method: "POST" });
    assert.equal(denied.status, 405);
    await denied.text();
    const unknown = await get("/__test/mutate");
    assert.equal(unknown.status, 404);
    await unknown.text();
    assert.equal(catalog.reads.home, 1);
  } finally {
    upstream.server.closeAllConnections();
    await new Promise((resolve) => upstream.server.close(resolve));
  }
});
