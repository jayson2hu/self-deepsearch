import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const read = (relative) => readFileSync(path.join(root, relative), "utf8");

test("DEV-15 freezes every prior DEV commit and keeps external release gates explicit", () => {
  const handoff = read("docs/redesign/DEV15_HANDOFF.md");
  const commits = [
    "c53ab83", "e6aac9d", "0c57f60", "0af611a", "3f6c13c", "875bde5", "15936bf",
    "9393aad", "24c852c", "863039d", "9f219f4", "53377b5", "5d48116", "0bb7837",
  ];
  for (const commit of commits) {
    assert.match(handoff, new RegExp(`\\b${commit}\\b`));
    execFileSync("git", ["cat-file", "-e", `${commit}^{commit}`], { cwd: root, stdio: "ignore" });
  }
  assert.match(handoff, /SEO_INDEXING_ENABLED=false/);
  assert.match(handoff, /SEO_CONTENT_POLICY_READY=false/);
  assert.match(handoff, /未获授权上线/);
  assert.match(handoff, /Down 会删除分类\/ 预览结论|Down 会删除分类\/预览结论/);
});

test("progress closes all fifteen work-package records without inflating acceptance", () => {
  const progress = read("docs/redesign/PROGRESS.md");
  const rows = [...progress.matchAll(/^\| (DEV-\d{2}) \| ([^|]+) \|/gm)];
  assert.equal(rows.length, 15);
  assert.deepEqual(rows.map((match) => match[1]), Array.from({ length: 15 }, (_, index) => `DEV-${String(index + 1).padStart(2, "0")}`));
  const statuses = rows.map((match) => match[2].trim());
  assert.equal(statuses.filter((status) => status === "已验收").length, 1);
  assert.equal(statuses.filter((status) => status === "待验收").length, 13);
  assert.equal(statuses.filter((status) => status === "待决策").length, 1);
  assert.equal(statuses.filter((status) => status === "未开始").length, 0);
});

test("handoff evidence records current failures and target-environment ownership", () => {
  const evidence = read("docs/evidence/redesign-2026-09-16-dev-15/README.md");
  for (const marker of [
    "325 passed", "348 subtests passed", "98 条", "/opt/google/chrome/chrome", "无法启动 Go 合同",
    "AC-20/21", "Search Console", "不可写成“可直接上线”",
  ]) assert.ok(evidence.includes(marker), `missing evidence marker: ${marker}`);
});

test("acceptance and redesign entry points link to the final handoff", () => {
  const acceptance = read("docs/redesign/ACCEPTANCE_PLAN.md");
  const entry = read("docs/redesign/README.md");
  const rootReadme = read("README.md");
  assert.match(acceptance, /DEV15_HANDOFF\.md/);
  assert.match(entry, /DEV15_HANDOFF\.md/);
  assert.match(rootReadme, /DEV15_HANDOFF\.md/);
});
