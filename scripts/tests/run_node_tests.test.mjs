import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync, readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { discoverTests, runTests } from "../run_node_tests.mjs";

test("CI and the unified preflight run the auto-discovered suite", () => {
  const manifest = JSON.parse(readFileSync(new URL("../../package.json", import.meta.url), "utf8"));
  assert.equal(manifest.scripts["test:unit"], "node scripts/run_node_tests.mjs");
  const ci = readFileSync(new URL("../../.github/workflows/ci.yml", import.meta.url), "utf8");
  const preflight = readFileSync(new URL("../release_a_preflight.mjs", import.meta.url), "utf8");
  assert.match(ci, /run: npm run test:unit/);
  assert.match(preflight, /runNpm\("All Node unit and contract tests", \["run", "test:unit"\]\)/);
});

test("test discovery includes newly added and nested tests, ignoring non-tests", (t) => {
  const directory = mkdtempSync(path.join(os.tmpdir(), "self-deepsearch-node-discovery-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  mkdirSync(path.join(directory, "nested"));
  writeFileSync(path.join(directory, "z.test.mjs"), "");
  writeFileSync(path.join(directory, "nested", "a.test.mjs"), "");
  writeFileSync(path.join(directory, "helper.mjs"), "");
  assert.deepEqual(discoverTests(directory), [path.join(directory, "nested", "a.test.mjs"), path.join(directory, "z.test.mjs")]);
  assert.ok(discoverTests().some((file) => file.endsWith("platform_proxy.test.mjs")));
  assert.ok(discoverTests().some((file) => file.endsWith("ops_reauth_client.test.mjs")));
  assert.ok(discoverTests().some((file) => file.endsWith("visitor_metrics.test.mjs")));
});

test("runner fails on empty suites and propagates real test failures", (t) => {
  const directory = mkdtempSync(path.join(os.tmpdir(), "self-deepsearch-node-runner-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  assert.throws(() => runTests(directory, "pipe"), /No Node test files/);
  const fixture = path.join(directory, "result.test.mjs");
  writeFileSync(fixture, 'import test from "node:test"; test("passing", () => {});');
  assert.equal(runTests(directory, "pipe"), 0);
  writeFileSync(fixture, 'import test from "node:test"; test("failing", () => { throw new Error("expected failure"); });');
  assert.equal(runTests(directory, "pipe"), 1);
});
