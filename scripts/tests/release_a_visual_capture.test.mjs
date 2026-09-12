import assert from "node:assert/strict";
import { promises as fs } from "node:fs";
import path from "node:path";
import test from "node:test";

import {
  browserLaunchOptions,
  parseVisualArgs,
  plannedCaptures,
  resolveEvidenceOutput,
  shanghaiDate,
  visualHelp,
  visualManifest,
} from "../release_a_visual_capture.mjs";

test("visual capture defaults to local Chrome, current Shanghai date, and no rebuild", () => {
  const now = new Date("2026-09-09T16:30:00Z");
  const options = parseVisualArgs([], now);
  assert.equal(options.build, false);
  assert.equal(options.browserChannel, "chrome");
  assert.equal(options.date, "2026-09-10");
  assert.equal(options.outputDir, null);
});

test("visual capture accepts explicit safe capture options", () => {
  const options = parseVisualArgs([
    "--build",
    "--browser-channel", "chromium",
    "--date", "2026-09-08",
    "--output-dir", "docs/evidence/manual-release-a",
    "--timeout", "60000",
  ]);
  assert.equal(options.build, true);
  assert.equal(options.browserChannel, "chromium");
  assert.equal(options.date, "2026-09-08");
  assert.equal(options.timeoutMs, 60000);
});

test("visual capture rejects invalid, missing, and unknown options", () => {
  assert.throws(() => parseVisualArgs(["--browser-channel", "firefox"]), /chrome 或 chromium/);
  assert.throws(() => parseVisualArgs(["--date", "2026-02-30"]), /不是有效日期/);
  assert.throws(() => parseVisualArgs(["--output-dir"]), /缺少参数值/);
  assert.throws(() => parseVisualArgs(["--timeout", "1000"]), /至少 5000/);
  assert.throws(() => parseVisualArgs(["--unknown"]), /未知参数/);
});

test("visual output is constrained to a dedicated docs evidence child directory", () => {
  const root = path.resolve("visual-capture-test-root");
  const safe = resolveEvidenceOutput({ date: "2026-09-10", outputDir: null }, root);
  assert.equal(safe.output, path.join(root, "docs", "evidence", "release-a-visual-2026-09-10"));
  assert.throws(
    () => resolveEvidenceOutput({ date: "2026-09-10", outputDir: "../outside" }, root),
    /docs\/evidence/,
  );
  assert.throws(
    () => resolveEvidenceOutput({ date: "2026-09-10", outputDir: "docs/evidence" }, root),
    /独立子目录/,
  );
  assert.throws(
    () => resolveEvidenceOutput({ date: "2026-09-10", outputDir: "docs/evidence/team/manual" }, root),
    /独立子目录/,
  );
});

test("visual capture plan covers public, detail, anonymous ops, and owner ops pages at both viewports", () => {
  const captures = plannedCaptures();
  assert.equal(captures.length, 12);
  assert.equal(new Set(captures.map((capture) => capture.filename)).size, captures.length);
  assert.deepEqual(new Set(captures.map((capture) => capture.viewportKey)), new Set(["desktop", "mobile"]));
  assert.ok(captures.some((capture) => capture.key === "work-detail" && capture.route.includes("test-001")));
  assert.ok(captures.some((capture) => capture.key === "ops-login" && capture.auth === "anonymous"));
  assert.ok(captures.some((capture) => capture.key === "ops-dashboard" && capture.auth === "owner"));
  assert.ok(captures.some((capture) => capture.key === "ops-users" && capture.auth === "owner"));
  assert.ok(captures.some((capture) => capture.key === "ops-audit" && capture.route.includes("request_id=") && capture.auth === "owner"));
});

test("visual manifest records integrity metadata and explicit synthetic boundaries", () => {
  const manifest = visualManifest({
    date: "2026-09-10",
    generatedAt: "2026-09-10T00:00:00.000Z",
    files: [{ file: "display-home-desktop.png", bytes: 42, sha256: "a".repeat(64) }],
  });
  assert.equal(manifest.schema_version, 1);
  assert.equal(manifest.synthetic_data.persistence, "memory-only; cleared when capture exits");
  assert.ok(manifest.excluded_dependencies.includes("PostgreSQL"));
  assert.ok(manifest.excluded_dependencies.includes("ad network scripts"));
  assert.equal(manifest.files[0].bytes, 42);
});

test("visual browser launcher uses Chrome channel or bundled Chromium safely", () => {
  assert.deepEqual(browserLaunchOptions("chrome"), { headless: true, channel: "chrome" });
  assert.deepEqual(browserLaunchOptions("chromium"), { headless: true, channel: undefined });
});

test("visual help documents local-only dependencies and constrained output", () => {
  const help = visualHelp();
  assert.match(help, /随机回环端口/);
  assert.match(help, /不会启动 Docker、PostgreSQL、邮件、对象存储、自动采集或广告联盟/);
  assert.match(help, /必须位于 docs\/evidence 内/);
  assert.equal(shanghaiDate(new Date("2026-09-09T15:59:59Z")), "2026-09-09");
  assert.equal(shanghaiDate(new Date("2026-09-09T16:00:00Z")), "2026-09-10");
});

test("mobile user management uses labeled cards instead of narrow fixed table columns", async () => {
  const component = await fs.readFile(new URL("../../apps/ops-web/components/user-manager.tsx", import.meta.url), "utf8");
  const styles = await fs.readFile(new URL("../../apps/ops-web/app/globals.css", import.meta.url), "utf8");
  assert.match(component, /className="table-wrap user-table"/);
  for (const label of ["邮箱", "状态", "当前角色", "创建时间"]) {
    assert.match(component, new RegExp(`data-label="${label}"`));
  }
  assert.match(styles, /\.user-table tbody \{ display: grid;/);
  assert.match(styles, /content: attr\(data-label\)/);
});
