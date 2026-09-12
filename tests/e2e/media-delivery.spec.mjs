import { expect, test } from "@playwright/test";
import { createHmac, randomBytes, randomUUID } from "node:crypto";
import { createCatalog, createIsolatedStandalone, removeIsolatedRuntime, startCatalog } from "../../scripts/release_a_cache_contract.mjs";
import { ensureArtifacts, findFreePort, REPOSITORY_ROOT, startChild, stopChild, waitForHTTP } from "../../scripts/release_a_frontend_smoke.mjs";

const pixel = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=", "base64");

test("media stop suppresses runtime HTML and browser image requests, then recovers", async ({ browser }, info) => {
  test.setTimeout(120_000);
  const paths = await ensureArtifacts({ repositoryRoot: REPOSITORY_ROOT });
  let runtime, upstream, child;
  const contexts = [];
  try {
    runtime = await createIsolatedStandalone(paths.display);
    const catalog = createCatalog();
    catalog.state.hasImage = true;
    const catalogRespond = catalog.respond;
    catalog.respond = (request, response) => {
      if (request.url === "/api/v1/me") {
        response.writeHead(401, { "Content-Type": "application/json", "Cache-Control": "no-store" });
        return response.end(JSON.stringify({ error: { code: "AUTH_REQUIRED", message: "Synthetic anonymous visitor" } }));
      }
      if (request.url === "/api/v1/metrics/page-view" && request.method === "POST") {
        response.writeHead(204); return response.end();
      }
      catalogRespond(request, response);
    };
    upstream = await startCatalog(catalog);
    const port = await findFreePort({ avoid: new Set([upstream.port]) });
    const origin = `http://127.0.0.1:${port}`;
    const secret = randomBytes(32).toString("hex");
    const start = async (mode) => {
      child = startChild({ name: `media-delivery-${mode}`, server: runtime.server, port,
        apiURL: `http://127.0.0.1:${upstream.port}`, repositoryRoot: REPOSITORY_ROOT,
        extraEnv: { MEDIA_DELIVERY_MODE: mode, CACHE_HMAC_SECRET: secret, SITE_URL: origin } });
      await waitForHTTP(child, "/robots.txt", 30_000);
    };
    const purge = async () => {
      const id = randomUUID();
      const timestamp = String(Math.floor(Date.now() / 1000));
      const body = JSON.stringify({ event_id: id, event_type: "cache_purge", aggregate_type: "site", aggregate_id: catalog.id, payload: {} });
      const signature = createHmac("sha256", secret).update(`${timestamp}\n${id}\n${body}`).digest("hex");
      const result = await fetch(`${origin}/api/internal/revalidate`, { method: "POST", body,
        headers: { "content-type": "application/json", "x-sd-event-id": id, "x-sd-timestamp": timestamp, "x-sd-signature": `v1=${signature}` },
        signal: AbortSignal.timeout(5000) });
      expect(result.status).toBe(200);
      expect((await result.json()).revalidated).toBe(true);
    };
    const newPage = async () => {
      const context = await browser.newContext({ viewport: info.project.use.viewport, isMobile: info.project.use.isMobile, hasTouch: info.project.use.hasTouch });
      contexts.push(context);
      const page = await context.newPage();
      const remote = [];
      await page.route("https://media.example.test/**", async (route) => {
        remote.push(route.request().url());
        await route.fulfill({ status: 200, contentType: "image/png", body: pixel });
      });
      return { page, remote };
    };
    await start("normal");
    await purge();
    const normal = await newPage();
    await normal.page.goto(`${origin}/`);
    await expect(normal.page.getByRole("link", { name: "查看 TEST-CACHE" })).toBeVisible();
    await expect.poll(() => normal.remote.length).toBeGreaterThan(0);
    const before = await fetch(origin);
    expect(await before.text()).toContain(catalog.imageMarker);
    await normal.page.context().close();
    await stopChild(child); child = undefined;

    // Reuse the SAME isolated data cache from normal mode. The home route is
    // runtime-rendered, so a restart in default-only mode must project the
    // cached upstream DTO to a default image instead of preserving its URL.
    await start("default_only");
    const stale = await fetch(origin);
    expect(stale.headers.get("x-media-delivery-mode")).toBe("default_only");
    expect(stale.headers.get("cache-control")).toContain("no-store");
    expect(stale.headers.get("content-security-policy")).toContain("img-src 'self' data: blob:;");
    expect(stale.headers.get("content-security-policy")).toContain("frame-ancestors 'none'");
    const staleBody = await stale.text();
    expect(staleBody).not.toContain(catalog.imageMarker);
    expect(staleBody).toContain("/default-work.svg");
    const stopped = await newPage();
    await stopped.page.goto(origin);
    const image = stopped.page.locator(".work-card__visual img").first();
    await expect(image).toHaveAttribute("src", "/default-work.svg");
    await expect.poll(() => image.evaluate((img) => img.complete && img.naturalWidth > 0)).toBe(true);
    expect(stopped.remote).toEqual([]);

    await purge();
    const work = await fetch(`${origin}/works/${catalog.slug}`);
    const body = await work.text();
    expect(work.status).toBe(200);
    expect(body).toContain(catalog.before);
    expect(body).not.toContain(catalog.imageMarker); // Includes JSON-LD, OG and RSC payloads.
    await stopped.page.goto(`${origin}/works/${catalog.slug}`);
    await expect(stopped.page.getByRole("heading", { name: catalog.before, exact: true })).toBeVisible();
    await expect(stopped.page.getByRole("img", { name: "TEST-CACHE 作品图" })).toHaveAttribute("src", "/default-work.svg");
    await expect(stopped.page.getByRole("link", { name: "登录后收藏、隐藏或纠错" })).toBeVisible();
    expect(stopped.remote).toEqual([]);
    await stopped.page.screenshot({ path: info.outputPath("media-delivery-stopped.png"), fullPage: true });
    await stopped.page.context().close();
    const stoppedLogin = await newPage();
    await stoppedLogin.page.goto(`${origin}/login`);
    await expect(stoppedLogin.page.getByLabel("邮箱", { exact: true })).toBeVisible();
    expect(stoppedLogin.remote).toEqual([]);
    await stoppedLogin.page.context().close();
    await stopChild(child); child = undefined;

    await start("normal");
    await purge();
    const recovered = await newPage();
    await recovered.page.goto(`${origin}/works/${catalog.slug}`);
    await expect.poll(() => recovered.remote.length).toBeGreaterThan(0);
    await expect.poll(() => recovered.page.locator(".work-detail__visual img").evaluate((img) => img.naturalWidth > 0)).toBe(true);
  } finally {
    await Promise.all(contexts.map((context) => context.close()));
    await stopChild(child);
    if (upstream) {
      upstream.server.closeAllConnections?.();
      await new Promise((resolve) => upstream.server.close(resolve));
    }
    if (runtime) await removeIsolatedRuntime(runtime.runtimeRoot);
  }
});

test("dynamic policy stops and restores cached pages without restarting Next", async ({ browser }, info) => {
  test.setTimeout(120_000);
  const paths = await ensureArtifacts({ repositoryRoot: REPOSITORY_ROOT });
  let runtime, upstream, child;
  const contexts = [];
  try {
    runtime = await createIsolatedStandalone(paths.display);
    const catalog = createCatalog();
    catalog.state.hasImage = true;
    let policyMode = "normal";
    const catalogRespond = catalog.respond;
    catalog.respond = (request, response) => {
      if (request.url === "/internal/v1/media-delivery-policy") {
        const body = JSON.stringify({ mode: policyMode });
        response.writeHead(200, {
          "Content-Type": "application/json; charset=utf-8",
          "Content-Length": Buffer.byteLength(body),
          "Cache-Control": "no-store",
          "X-Media-Delivery-Mode": policyMode,
        });
        return response.end(body);
      }
      if (request.url === "/api/v1/me") {
        response.writeHead(401, { "Content-Type": "application/json", "Cache-Control": "no-store" });
        return response.end(JSON.stringify({ error: { code: "AUTH_REQUIRED", message: "Synthetic anonymous visitor" } }));
      }
      if (request.url === "/api/v1/metrics/page-view" && request.method === "POST") {
        response.writeHead(204); return response.end();
      }
      catalogRespond(request, response);
    };
    upstream = await startCatalog(catalog);
    const port = await findFreePort({ avoid: new Set([upstream.port]) });
    const origin = `http://127.0.0.1:${port}`;
    const secret = randomBytes(32).toString("hex");
    child = startChild({ name: "media-delivery-dynamic", server: runtime.server, port,
      apiURL: `http://127.0.0.1:${upstream.port}`, repositoryRoot: REPOSITORY_ROOT,
      extraEnv: { MEDIA_DELIVERY_MODE: "normal", MEDIA_DELIVERY_DYNAMIC_MODE: "enforce", CACHE_HMAC_SECRET: secret, SITE_URL: origin } });
    await waitForHTTP(child, "/robots.txt", 30_000);

    const purge = async () => {
      const id = randomUUID();
      const timestamp = String(Math.floor(Date.now() / 1000));
      const body = JSON.stringify({ event_id: id, event_type: "cache_purge", aggregate_type: "site", aggregate_id: catalog.id, payload: {} });
      const signature = createHmac("sha256", secret).update(`${timestamp}\n${id}\n${body}`).digest("hex");
      const result = await fetch(`${origin}/api/internal/revalidate`, { method: "POST", body,
        headers: { "content-type": "application/json", "x-sd-event-id": id, "x-sd-timestamp": timestamp, "x-sd-signature": `v1=${signature}` },
        signal: AbortSignal.timeout(5000) });
      expect(result.status).toBe(200);
    };
    const newPage = async () => {
      const context = await browser.newContext({ viewport: info.project.use.viewport, isMobile: info.project.use.isMobile, hasTouch: info.project.use.hasTouch });
      contexts.push(context);
      const page = await context.newPage();
      const remote = [];
      await page.route("https://media.example.test/**", async (route) => {
        remote.push(route.request().url());
        await route.fulfill({ status: 200, contentType: "image/png", body: pixel });
      });
      return { page, remote };
    };

    await purge();
    const warm = await fetch(origin);
    expect(warm.headers.get("x-media-delivery-mode")).toBe("normal");
    expect(await warm.text()).toContain(catalog.imageMarker);
    const normal = await newPage();
    await normal.page.goto(origin);
    await expect.poll(() => normal.remote.length).toBeGreaterThan(0);
    await normal.page.context().close();

    policyMode = "default_only";
    const stoppedResponse = await fetch(origin);
    expect(stoppedResponse.headers.get("x-media-delivery-mode")).toBe("default_only");
    expect(stoppedResponse.headers.get("cache-control")).toContain("no-store");
    expect(stoppedResponse.headers.get("content-security-policy")).toContain("img-src 'self' data: blob:;");
    expect(await stoppedResponse.text()).toContain(catalog.imageMarker); // same warm ISR artifact
    const stopped = await newPage();
    await stopped.page.goto(origin);
    await expect(stopped.page.locator(".work-card__visual img").first()).toHaveAttribute("src", "/default-work.svg");
    expect(stopped.remote).toEqual([]);
    await stopped.page.context().close();

    policyMode = "normal";
    const recoveredResponse = await fetch(origin);
    expect(recoveredResponse.headers.get("x-media-delivery-mode")).toBe("normal");
    const recovered = await newPage();
    await recovered.page.goto(origin);
    await expect.poll(() => recovered.remote.length).toBeGreaterThan(0);
  } finally {
    await Promise.all(contexts.map((context) => context.close()));
    await stopChild(child);
    if (upstream) {
      upstream.server.closeAllConnections?.();
      await new Promise((resolve) => upstream.server.close(resolve));
    }
    if (runtime) await removeIsolatedRuntime(runtime.runtimeRoot);
  }
});
