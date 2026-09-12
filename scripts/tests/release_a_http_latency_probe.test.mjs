import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";

import {
  HTTP_LATENCY_THRESHOLDS,
  latencyGate,
  parseHTTPLatencyArgs,
  percentile,
  httpLatencyHelp,
  httpLatencyReport,
  resolveHTTPLatencyOutput,
  summarizeLatencySamples,
} from "../release_a_http_latency_probe.mjs";

test("HTTP latency probe defaults to low-concurrency local sampling", () => {
  const options = parseHTTPLatencyArgs([], new Date("2026-09-09T16:00:00Z"));
  assert.equal(options.displayURL, null);
  assert.equal(options.searchCode, "TEST-001");
  assert.equal(options.samples, 20);
  assert.equal(options.warmup, 3);
  assert.equal(options.build, false);
  assert.equal(options.date, "2026-09-10");
});

test("HTTP latency probe accepts explicit HTTPS target parameters", () => {
  const options = parseHTTPLatencyArgs([
    "--display-url", "https://display.example.test",
    "--search-code", "ABC-123",
    "--samples", "25",
    "--warmup", "5",
    "--timeout", "9000",
    "--report-only",
  ]);
  assert.equal(options.displayURL, "https://display.example.test");
  assert.equal(options.searchCode, "ABC-123");
  assert.equal(options.samples, 25);
  assert.equal(options.warmup, 5);
  assert.equal(options.reportOnly, true);
});

test("HTTP latency probe rejects unsafe targets, codes, and conflicting build mode", () => {
  assert.throws(() => parseHTTPLatencyArgs(["--display-url", "http://example.test", "--search-code", "ABC-1"]), /必须使用 HTTPS/);
  assert.throws(() => parseHTTPLatencyArgs(["--display-url", "https://example.test"]), /必须显式提供 --search-code/);
  assert.throws(() => parseHTTPLatencyArgs(["--display-url", "https://example.test", "--search-code", "ABC 1"]), /番号字符/);
  assert.throws(() => parseHTTPLatencyArgs(["--display-url", "https://example.test", "--search-code", "ABC-1", "--build"]), /不能与 --display-url/);
  assert.throws(() => parseHTTPLatencyArgs(["--samples", "4"]), /5-100/);
});

test("HTTP latency evidence output is constrained and separates target reports", () => {
  const root = path.resolve("http-latency-test-root");
  const local = resolveHTTPLatencyOutput({ date: "2026-09-10", displayURL: null, output: null }, root);
  const target = resolveHTTPLatencyOutput({ date: "2026-09-10", displayURL: "https://example.test", output: null }, root);
  assert.equal(local.output, path.join(root, "docs", "evidence", "release-a-http-latency-2026-09-10.json"));
  assert.equal(target.output, path.join(root, "docs", "evidence", "release-a-http-latency-target-2026-09-10.json"));
  assert.throws(() => resolveHTTPLatencyOutput({ date: "2026-09-10", output: "outside.json" }, root), /docs\/evidence/);
  assert.throws(() => resolveHTTPLatencyOutput({ date: "2026-09-10", output: "docs/evidence/nested/result.json" }, root), /直接 .json/);
});

test("HTTP latency summaries use nearest-rank P95", () => {
  const durations = Array.from({ length: 20 }, (_, index) => index + 1);
  assert.equal(percentile(durations, 0.95), 19);
  const summary = summarizeLatencySamples(durations.map((duration, index) => ({ sample: index + 1, duration_ms: duration, response_bytes: 100 + index })));
  assert.equal(summary.duration_ms.p50, 10);
  assert.equal(summary.duration_ms.p95, 19);
  assert.equal(summary.duration_ms.max, 20);
});

test("HTTP latency gates apply separate public and search P95 thresholds", () => {
  assert.deepEqual(latencyGate({ endpoint: { key: "public-home-api" }, summary: { duration_ms: { p95: HTTP_LATENCY_THRESHOLDS.publicApiP95Ms - 1 } } }), []);
  assert.equal(latencyGate({ endpoint: { key: "public-home-api" }, summary: { duration_ms: { p95: HTTP_LATENCY_THRESHOLDS.publicApiP95Ms } } }).length, 1);
  assert.deepEqual(latencyGate({ endpoint: { key: "search-api" }, summary: { duration_ms: { p95: HTTP_LATENCY_THRESHOLDS.searchP95Ms - 1 } } }), []);
});

test("HTTP latency report omits search code and response bodies", () => {
  const report = httpLatencyReport({
    date: "2026-09-10",
    generatedAt: "2026-09-10T00:00:00.000Z",
    mode: "local-synthetic",
    origin: "http://127.0.0.1:1234",
    options: { samples: 20, warmup: 3, searchCode: "SECRET-001" },
    results: [{ failures: [], status: "passed" }],
  });
  const serialized = JSON.stringify(report);
  assert.equal(report.privacy.search_code_stored, false);
  assert.equal(report.privacy.response_bodies_stored, false);
  assert.doesNotMatch(serialized, /SECRET-001/);
  assert.match(report.synthetic_boundary, /no Docker\/PostgreSQL\/Cloudflare/);
});

test("HTTP latency help documents low concurrency, privacy, thresholds, and HTTPS target", () => {
  const help = httpLatencyHelp();
  assert.match(help, /低并发、顺序采样/);
  assert.match(help, /不保存搜索番号或响应正文/);
  assert.match(help, /最终 HTTPS/);
  assert.match(help, /docs\/evidence/);
});
