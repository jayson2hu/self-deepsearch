#!/usr/bin/env node

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { promises as fs } from "node:fs";
import http from "node:http";
import path from "node:path";
import { pathToFileURL } from "node:url";
import {
  ensureArtifacts, findFreePort, REPOSITORY_ROOT, SmokeFailure, standalonePaths, startChild, stopChild, waitForHTTP,
} from "./release_a_frontend_smoke.mjs";

const REQUEST_TIMEOUT_MS = 15_000;
const GO_TIMEOUT_MS = 60_000;
const CACHE_WINDOW_MS = 240_000; // Fail before the normal 300-second TTL could mask missing invalidation.
const BOUNDARY = "Real Go processor and production Next.js; synthetic in-memory catalog; no PostgreSQL, dispatcher, SMTP, S3 or Cloudflare";

export function parseCacheArgs(argv) {
  const options = { build: false, help: false, output: "docs/evidence/release-a-cache-contract-local.json" };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") options.build = true;
    else if (argument === "--help" || argument === "-h") options.help = true;
    else if (argument === "--output" && argv[index + 1] && !argv[index + 1].startsWith("--")) options.output = argv[++index];
    else throw new SmokeFailure(`未知或缺值参数：${argument}`);
  }
  return options;
}

export function cacheEvidencePath(output, repositoryRoot = REPOSITORY_ROOT) {
  const root = path.resolve(repositoryRoot, "docs", "evidence");
  const target = path.resolve(repositoryRoot, output);
  if (path.dirname(target) !== root || path.extname(target) !== ".json") {
    throw new SmokeFailure("缓存证据必须写入 docs/evidence 下的直接 .json 文件");
  }
  return target;
}

export function createCatalog() {
  const id = randomUUID();
  const slug = `cache-contract-${id}`;
  const before = `Cache-Before-${id}`;
  const after = `Cache-After-${id}`;
  const imageMarker = `https://media.example.test/cache-contract/${id}/`;
  const renditions = [320, 640, 960].map((width) => ({
    url: `${imageMarker}cover-w${width}.webp`, rendition: `w${width}`,
    width, height: width * 5 / 8, mime_type: "image/webp",
  }));
  const state = { title: before, hidden: false, hasImage: false, updatedAt: "2026-09-01T00:00:00Z" };
  const reads = { home: 0, work: 0, sitemap: 0 };
  const item = () => ({
    id, type: "work", code: "TEST-CACHE", title: state.title, subtitle: "Synthetic catalog",
    href: `/works/${slug}`, image_url: state.hasImage ? renditions[1].url : null,
    image: state.hasImage ? { ...renditions[1], renditions } : null,
  });
  function respond(request, response) {
    const url = new URL(request.url ?? "/", "http://127.0.0.1");
    let body;
    let status = 200;
    if (request.method !== "GET") {
      status = 405;
      body = { error: "method_not_allowed" };
    } else if (url.pathname === "/api/v1/site/home") {
      reads.home += 1;
      body = { generated_at: state.updatedAt, sections: [
        { key: "latest", title: "最新发行", items: state.hidden ? [] : [item()] },
      ] };
    } else if (url.pathname === `/api/v1/works/${slug}`) {
      reads.work += 1;
      status = state.hidden ? 404 : 200;
      body = state.hidden ? { error: "not_found" } : {
        id, code: "TEST-CACHE", title: state.title, title_original: null, release_date: "2026-09-01",
        studio_name: "Synthetic studio", summary: "Local cache contract only.", performers: [],
        images: state.hasImage ? [{ ...renditions[2], renditions }] : [], related_works: [],
      };
    } else if (url.pathname === "/api/v1/site/sitemap") {
      reads.sitemap += 1;
      body = { items: !state.hidden && url.searchParams.get("entity_type") === "work"
        ? [{ entity_type: "work", slug, updated_at: state.updatedAt }] : [] };
    } else {
      status = 404;
      body = { error: "unknown_fixture_route" };
    }
    response.writeHead(status, { "Content-Type": "application/json" });
    response.end(JSON.stringify(body));
  }
  return { id, slug, before, after, imageMarker, state, reads, respond };
}

export async function startCatalog(catalog) {
  const server = http.createServer(catalog.respond);
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  return { server, port: server.address().port };
}

export async function removeIsolatedRuntime(runtimeRoot, repositoryRoot = REPOSITORY_ROOT) {
  const cacheRoot = await fs.realpath(path.join(repositoryRoot, ".cache"));
  const target = await fs.realpath(runtimeRoot);
  if (path.dirname(target) !== cacheRoot || !path.basename(target).startsWith("cache-contract-")) {
    throw new SmokeFailure("拒绝清理隔离缓存目录以外的路径");
  }
  await fs.rm(target, { recursive: true, force: true });
}

export async function createIsolatedStandalone(source, repositoryRoot = REPOSITORY_ROOT) {
  const cacheRoot = path.join(repositoryRoot, ".cache");
  await fs.mkdir(cacheRoot, { recursive: true });
  const runtimeRoot = await fs.mkdtemp(path.join(cacheRoot, "cache-contract-"));
  const target = standalonePaths("display-web", runtimeRoot);
  try {
    // Copy, never hard-link/symlink: Next rewrites .next/server and .next/cache during ISR.
    await fs.cp(path.join(source.appRoot, ".next", "standalone"), path.join(target.appRoot, ".next", "standalone"), {
      recursive: true, dereference: true,
    });
    // The source standalone is also used by local preview/smoke commands, so
    // it may contain request-time fetch cache from an earlier synthetic API.
    // An isolated runtime must start from build artifacts only; otherwise a
    // restart can resurrect unrelated catalog data even after the first
    // process refreshed its in-memory tags.
    await fs.rm(path.join(path.dirname(target.server), ".next", "cache"), { recursive: true, force: true });
    await fs.cp(source.sourceStatic, target.sourceStatic, { recursive: true, dereference: true });
    await fs.cp(source.sourcePublic, target.sourcePublic, { recursive: true, dereference: true });
    return { runtimeRoot, server: target.server };
  } catch (error) {
    await removeIsolatedRuntime(runtimeRoot, repositoryRoot);
    throw error;
  }
}

function runGo(args, env, repositoryRoot) {
  return new Promise((resolve, reject) => {
    const child = spawn("go", args, { cwd: repositoryRoot, env: { ...process.env, ...env }, windowsHide: true, stdio: ["ignore", "pipe", "pipe"] });
    let output = "";
    let timedOut = false;
    const collect = (chunk) => { output = (output + chunk.toString()).slice(-12_000); };
    child.stdout.on("data", collect);
    child.stderr.on("data", collect);
    const timeout = setTimeout(() => { timedOut = true; child.kill("SIGKILL"); }, GO_TIMEOUT_MS);
    child.once("error", () => { clearTimeout(timeout); reject(new SmokeFailure("无法启动 Go 合同；请确认 Go 和已缓存依赖可用")); });
    child.once("close", (code) => {
      clearTimeout(timeout);
      if (timedOut || code !== 0) reject(new SmokeFailure(timedOut ? "Go 缓存合同超时" : "Go 缓存合同失败", output));
      else if (!output.includes("PASS: TestRevalidationNextContract") || output.includes("SKIP: TestRevalidationNextContract")) {
        reject(new SmokeFailure("真实 Next 合同没有执行，不能把跳过当作通过"));
      } else resolve();
    });
  });
}

async function readPage(origin, pathname) {
  const response = await fetch(`${origin}${pathname}`, {
    redirect: "manual", signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS),
  });
  return { status: response.status, body: await response.text() };
}

export function assertVisible(pages, catalog, title, updatedAt) {
  for (const [name, page] of Object.entries(pages)) assert.equal(page.status, 200, `${name}: expected HTTP 200`);
  assert.ok(pages.home.body.includes(title), "home must contain the expected title");
  assert.ok(pages.work.body.includes(`<h1>${title}</h1>`), "work must render the expected heading, not just metadata");
  assert.ok(pages.sitemap.body.includes(`/works/${catalog.slug}</loc>`), "sitemap must contain the work URL");
  assert.ok(pages.sitemap.body.includes(`<lastmod>${updatedAt}</lastmod>`), "sitemap must contain the expected revision date");
  const unexpected = title === catalog.before ? catalog.after : catalog.before;
  assert.ok(!pages.home.body.includes(unexpected) && !pages.work.body.includes(unexpected), "old and new titles must not be mixed");
}

export function assertHidden(pages, catalog) {
  assert.equal(pages.home.status, 200);
  assert.equal(pages.sitemap.status, 200);
  // Next can stream a 200 status before notFound(); require the noindex 404 UI as well.
  assert.ok([200, 404].includes(pages.work.status));
  assert.match(pages.work.body, /<meta name="robots" content="noindex"\s*\/?\s*>/);
  assert.ok(pages.work.body.includes("This page could not be found"), "work must render the not-found UI");
  for (const [name, page] of Object.entries(pages)) {
    assert.ok(!page.body.includes(catalog.before) && !page.body.includes(catalog.after), `${name}: hidden title remains exposed`);
    assert.ok(!page.body.includes(catalog.imageMarker), `${name}: hidden image remains exposed`);
  }
  assert.ok(!pages.home.body.includes(`/works/${catalog.slug}`), "hidden work link remains on the home page");
  assert.ok(!pages.sitemap.body.includes(`/works/${catalog.slug}</loc>`), "hidden work remains in sitemap");
}

export function assertMediaState(pages, catalog, visible) {
  for (const name of ["home", "work"]) {
    assert.equal(pages[name].status, 200);
    const body = pages[name].body;
    if (visible) {
      // A URL in JSON/metadata alone does not prove that users see an image.
      const tags = body.match(/<img\b[^>]*>/g) ?? [];
      assert.ok(tags.some((tag) => tag.includes(`src="${catalog.imageMarker}`)), `${name}: new media is not rendered as an image`);
    } else {
      assert.ok(!body.includes(catalog.imageMarker), `${name}: media appeared before cache invalidation`);
    }
  }
}

async function writeEvidence(output, report) {
  const parent = await fs.lstat(path.dirname(output));
  if (!parent.isDirectory() || parent.isSymbolicLink()) throw new SmokeFailure("证据目录必须是真实目录");
  try {
    const entry = await fs.lstat(output);
    if (!entry.isFile() || entry.isSymbolicLink()) throw new SmokeFailure("证据输出必须是普通文件");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  await fs.writeFile(output, `${JSON.stringify(report, null, 2)}\n`);
}

export async function runCacheContract(options, repositoryRoot = REPOSITORY_ROOT) {
  const output = cacheEvidencePath(options.output, repositoryRoot);
  const report = { schema_version: 1, status: "failed", generated_at: new Date().toISOString(), boundary: BOUNDARY, checks: [] };
  let upstream;
  let display;
  let isolated;
  let failure;
  try {
    const paths = await ensureArtifacts({ build: options.build, repositoryRoot });
    isolated = await createIsolatedStandalone(paths.display, repositoryRoot);
    const catalog = createCatalog();
    upstream = await startCatalog(catalog);
    const port = await findFreePort({ avoid: new Set([upstream.port]) });
    const origin = `http://127.0.0.1:${port}`;
    const secret = randomBytes(32).toString("hex");
    display = startChild({
      name: "display-cache-contract", server: isolated.server, port,
      apiURL: `http://127.0.0.1:${upstream.port}`, repositoryRoot,
      extraEnv: { CACHE_HMAC_SECRET: secret, SITE_URL: origin },
    });
    await waitForHTTP(display, "/robots.txt", 45_000);
    const invoke = (eventType, mode = "valid", eventID = randomUUID()) => runGo([
      "test", "./services/platform-worker/internal/outbox", "-run", "^TestRevalidationNextContract$", "-count=1", "-v", "-timeout=20s",
    ], {
      NEXT_CONTRACT_URL: `${origin}/api/internal/revalidate`, NEXT_CONTRACT_SECRET: secret,
      NEXT_CONTRACT_MODE: mode, NEXT_CONTRACT_EVENT_TYPE: eventType, NEXT_CONTRACT_EVENT_ID: eventID,
      NEXT_CONTRACT_WORK_ID: catalog.id,
    }, repositoryRoot);
    let pageStatuses = {};
    const pages = async () => {
      const result = {
        home: await readPage(origin, "/"), work: await readPage(origin, `/works/${catalog.slug}`),
        sitemap: await readPage(origin, "/sitemaps/works"),
      };
      pageStatuses = Object.fromEntries(Object.entries(result).map(([name, page]) => [name, page.status]));
      return result;
    };
    const check = async (name, action) => {
      const started = performance.now();
      try {
        await action();
        report.checks.push({
          name, status: "passed", duration_ms: Math.round(performance.now() - started),
          upstream_reads: { ...catalog.reads }, page_statuses: { ...pageStatuses },
        });
        console.log(`PASS ${name}`);
      } catch (error) {
        report.checks.push({ name, status: "failed" });
        throw error;
      }
    };
    // Invalidate prebuilt fallback home HTML; unique slug avoids earlier detail data.
    await check("prime-real-next-cache", async () => {
      await invoke("cache_purge");
      assertVisible(await pages(), catalog, catalog.before, catalog.state.updatedAt);
    });
    const baselineTime = performance.now();
    const baselineReads = { ...catalog.reads };
    for (const count of Object.values(baselineReads)) assert.ok(count > 0, "every surface must reach the synthetic upstream");
    const beforeDate = catalog.state.updatedAt;
    catalog.state.title = catalog.after;
    catalog.state.updatedAt = "2026-09-02T00:00:00Z";
    const stillCached = async () => {
      assertVisible(await pages(), catalog, catalog.before, beforeDate);
      assert.deepEqual(catalog.reads, baselineReads, "negative controls must not refetch the catalog");
    };
    await check("unchanged-without-invalidation", stillCached);
    await check("wrong-signature-does-not-refresh", async () => { await invoke("publication_changed", "wrong-secret"); await stillCached(); });
    await check("expired-signature-does-not-refresh", async () => { await invoke("publication_changed", "stale"); await stillCached(); });
    await check("publication-refreshes-html-and-sitemap", async () => {
      await invoke("publication_changed");
      assertVisible(await pages(), catalog, catalog.after, catalog.state.updatedAt);
      assert.ok(performance.now() - baselineTime < CACHE_WINDOW_MS, "test exceeded TTL safety window");
      for (const key of Object.keys(baselineReads)) assert.ok(catalog.reads[key] > baselineReads[key], `${key}: no refetch after valid invalidation`);
    });
    const mediaStart = performance.now();
    const mediaBaselineReads = { ...catalog.reads };
    // Media registration does not change the text revision or sitemap date.
    // HTTP HTML requests never fetch these synthetic .test image URLs.
    catalog.state.hasImage = true;
    await check("media-only-change-remains-cached-without-event", async () => {
      const result = await pages();
      assertVisible(result, catalog, catalog.after, catalog.state.updatedAt);
      assertMediaState(result, catalog, false);
      assert.deepEqual(catalog.reads, mediaBaselineReads, "media negative control unexpectedly refetched");
    });
    await check("media-cache-purge-refreshes-home-and-detail", async () => {
      await invoke("cache_purge");
      const result = await pages();
      assertVisible(result, catalog, catalog.after, catalog.state.updatedAt);
      assertMediaState(result, catalog, true);
      assert.ok(performance.now() - mediaStart < CACHE_WINDOW_MS, "media test exceeded TTL safety window");
      for (const key of ["home", "work"]) assert.ok(catalog.reads[key] > mediaBaselineReads[key], `${key}: media was not refetched`);
    });
    const hiddenStart = performance.now();
    catalog.state.hidden = true;
    const hiddenEventID = randomUUID();
    await check("hidden-content-removed-from-all-surfaces", async () => {
      assertVisible(await pages(), catalog, catalog.after, catalog.state.updatedAt);
      await invoke("publication_hidden", "valid", hiddenEventID);
      assertHidden(await pages(), catalog);
      assert.ok(performance.now() - hiddenStart < CACHE_WINDOW_MS, "hide test exceeded TTL safety window");
    });
    await check("duplicate-event-remains-safe", async () => {
      await invoke("publication_hidden", "valid", hiddenEventID);
      assertHidden(await pages(), catalog);
    });
    report.status = "passed";
  } catch (error) {
    failure = error;
    report.failure = "Contract failed; inspect the local test output. No response bodies or credentials are stored.";
  } finally {
    await stopChild(display);
    if (upstream) {
      upstream.server.closeAllConnections();
      await new Promise((resolve) => upstream.server.close(resolve));
    }
    if (isolated) await removeIsolatedRuntime(isolated.runtimeRoot, repositoryRoot);
  }
  await writeEvidence(output, report);
  if (failure) throw failure;
  return { output, report };
}

export async function main(argv = process.argv.slice(2)) {
  try {
    const options = parseCacheArgs(argv);
    if (options.help) {
      console.log("node scripts/release_a_cache_contract.mjs [--build] [--output docs/evidence/<name>.json]\nRequires current Next standalone artifacts and Go with cached dependencies. Local synthetic catalog only; does not start Docker.");
      return 0;
    }
    const result = await runCacheContract(options);
    console.log(JSON.stringify({ status: result.report.status, output: result.output, boundary: BOUNDARY }, null, 2));
    return 0;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    return 1;
  }
}

if (import.meta.url === (process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "")) process.exitCode = await main();
