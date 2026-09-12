import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import path from "node:path";
import test from "node:test";

import {
  LOCAL_WORK_PATH,
  createSameOriginRequestDrain,
  median,
  parsePerformanceArgs,
  performanceGates,
  performanceHelp,
  performanceReport,
  resolvePerformanceOutput,
  shouldDrainRequest,
  summarizeSamples,
} from "../release_a_performance_probe.mjs";

test("performance probe defaults to local warm-cache sampling without rebuilding", () => {
  const options = parsePerformanceArgs([], new Date("2026-09-09T16:00:00Z"));
  assert.equal(options.build, false);
  assert.equal(options.displayURL, null);
  assert.equal(options.workPath, LOCAL_WORK_PATH);
  assert.equal(options.samples, 3);
  assert.equal(options.date, "2026-09-10");
  assert.equal(options.browserChannel, "chrome");
});

test("performance probe accepts an explicit HTTPS target and work path", () => {
  const options = parsePerformanceArgs([
    "--display-url", "https://display.example.test",
    "--work-path", "/works/ABC-123-00000001",
    "--samples", "5",
    "--browser-channel", "chromium",
    "--output", "docs/evidence/release-a-performance-target.json",
    "--report-only",
  ]);
  assert.equal(options.displayURL, "https://display.example.test");
  assert.equal(options.workPath, "/works/ABC-123-00000001");
  assert.equal(options.samples, 5);
  assert.equal(options.reportOnly, true);
});

test("performance probe rejects unsafe targets and conflicting modes", () => {
  assert.throws(() => parsePerformanceArgs(["--display-url", "http://display.example.test", "--work-path", "/works/a"]), /必须使用 HTTPS/);
  assert.throws(() => parsePerformanceArgs(["--display-url", "https://user:pass@example.test", "--work-path", "/works/a"]), /用户名或密码/);
  assert.throws(() => parsePerformanceArgs(["--display-url", "https://example.test/path", "--work-path", "/works/a"]), /只能提供 origin/);
  assert.throws(() => parsePerformanceArgs(["--display-url", "https://example.test"]), /必须显式提供 --work-path/);
  assert.throws(() => parsePerformanceArgs(["--display-url", "https://example.test", "--work-path", "/works/a", "--build"]), /不能与 --display-url/);
  assert.throws(() => parsePerformanceArgs(["--work-path", "/works/../admin"]), /路径穿越/);
});

test("performance evidence output is constrained to a direct JSON file", () => {
  const root = path.resolve("performance-probe-test-root");
  const safe = resolvePerformanceOutput({ date: "2026-09-10", output: null }, root);
  assert.equal(safe.output, path.join(root, "docs", "evidence", "release-a-performance-2026-09-10.json"));
  const remote = resolvePerformanceOutput({ date: "2026-09-10", output: null, displayURL: "https://example.test" }, root);
  assert.equal(remote.output, path.join(root, "docs", "evidence", "release-a-performance-target-2026-09-10.json"));
  assert.throws(() => resolvePerformanceOutput({ date: "2026-09-10", output: "outside.json" }, root), /docs\/evidence/);
  assert.throws(() => resolvePerformanceOutput({ date: "2026-09-10", output: "docs/evidence/team/result.json" }, root), /直接 .json/);
  assert.throws(() => resolvePerformanceOutput({ date: "2026-09-10", output: "docs/evidence/result.txt" }, root), /直接 .json/);
});

test("sample summaries use medians and preserve worst-case values", () => {
  assert.equal(median([9, 1, 5]), 5);
  assert.equal(median([2, 4]), 3);
  const summary = summarizeSamples([
    { lcp_ms: 900, cls: 0.01, ttfb_ms: 20, dom_content_loaded_ms: 50, load_ms: 80, transfer_bytes: 100, encoded_body_bytes: 90, resource_count: 5 },
    { lcp_ms: 1100, cls: 0.03, ttfb_ms: 30, dom_content_loaded_ms: 60, load_ms: 90, transfer_bytes: 120, encoded_body_bytes: 100, resource_count: 6 },
    { lcp_ms: 1000, cls: 0.02, ttfb_ms: 25, dom_content_loaded_ms: 55, load_ms: 85, transfer_bytes: 110, encoded_body_bytes: 95, resource_count: 5 },
  ]);
  assert.equal(summary.lcp_ms.median, 1000);
  assert.equal(summary.cls.max, 0.03);
  assert.equal(summarizeSamples([{ cls: 0.00123 }]).cls.max, 0.00123);
});

test("performance gates enforce LCP everywhere and CLS on mobile", () => {
  const passing = {
    viewport: { key: "mobile" },
    summary: { lcp_ms: { median: 1200 }, cls: { max: 0.05 } },
  };
  assert.deepEqual(performanceGates(passing), []);
  assert.equal(performanceGates({
    viewport: { key: "mobile" },
    summary: { lcp_ms: { median: 2500 }, cls: { max: 0.1 } },
  }).length, 2);
  assert.deepEqual(performanceGates({
    viewport: { key: "desktop" },
    summary: { lcp_ms: { median: 1200 }, cls: { max: 0.5 } },
  }), []);
});

test("performance report clearly separates local lab evidence from target validation", () => {
  const result = {
    route: { key: "display-home", path: "/" },
    viewport: { key: "mobile" },
    failures: [],
    status: "passed",
    samples: [],
    summary: {},
  };
  const local = performanceReport({
    date: "2026-09-10",
    generatedAt: "2026-09-10T00:00:00.000Z",
    mode: "local-synthetic",
    origin: "http://127.0.0.1:1234",
    options: { browserChannel: "chrome", samples: 3 },
    results: [result],
  });
  assert.equal(local.status, "passed");
  assert.equal(local.target_origin, "local-random-loopback");
  assert.equal(local.measurement.field_data, false);
  assert.match(local.measurement.post_capture_cleanup, /after metrics are captured/);
  assert.match(local.synthetic_boundary, /no Docker\/PostgreSQL\/SMTP\/S3\/R2\/Cloudflare/);
});

test("performance help documents warm-up, target HTTPS, thresholds, and report-only mode", () => {
  const help = performanceHelp();
  assert.match(help, /预热后/);
  assert.match(help, /最终 HTTPS 站点/);
  assert.match(help, /report-only/);
  assert.match(help, /docs\/evidence/);
});

test("performance probe waits only for images intersecting the initial viewport", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_performance_probe.mjs", import.meta.url), "utf8"));
  assert.match(source, /rect\.bottom > 0/);
  assert.match(source, /rect\.top < window\.innerHeight/);
  assert.doesNotMatch(source, /waitForLoadState\("networkidle"/);
});

test("performance probe drains only same-origin render streams after metric capture", async () => {
  const request = (url, resourceType) => ({ resourceType: () => resourceType, url: () => url });
  assert.equal(shouldDrainRequest(request("https://display.example.test/works/a?_rsc=1", "fetch"), "https://display.example.test"), true);
  assert.equal(shouldDrainRequest(request("https://display.example.test/works/a", "document"), "https://display.example.test"), true);
  assert.equal(shouldDrainRequest(request("https://display.example.test/image.jpg", "image"), "https://display.example.test"), false);
  assert.equal(shouldDrainRequest(request("https://api.example.test/works/a", "fetch"), "https://display.example.test"), false);

  const page = new EventEmitter();
  const drain = createSameOriginRequestDrain(page, "https://display.example.test");
  const inFlight = request("https://display.example.test/works/a?_rsc=1", "fetch");
  page.emit("request", inFlight);
  assert.equal(drain.pendingCount(), 1);
  setTimeout(() => page.emit("requestfinished", inFlight), 10);
  assert.equal(await drain.wait({ timeoutMs: 200, quietMs: 20, pollMs: 5 }), true);
  assert.equal(drain.pendingCount(), 0);
  drain.dispose();
});
