import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = (relativePath) => readFile(path.join(root, relativePath), "utf8");

test("DEV-12 remains decision-gated and provides every required approval field", async () => {
  const [gate, decisions, progress] = await Promise.all([
    source("docs/redesign/DEV12_DECISION_GATE.md"),
    source("docs/redesign/DECISIONS.md"),
    source("docs/redesign/PROGRESS.md"),
  ]);
  for (const phrase of ["新用户 history_sync_enabled 默认值", "旧用户迁移规则", "未清除在线记录期限", "已清除在线记录期限", "归档对象期限", "备份期限", "恢复后防复活规则"]) {
    assert.match(gate, new RegExp(phrase));
  }
  assert.match(decisions, /\| Q-01 \| 历史默认开关、新旧用户迁移、在线与归档期限、删除与备份恢复边界/);
  assert.match(progress, /\| DEV-12 \| 待决策 \|/);
});

test("current product does not claim or implement an approved history preference", async () => {
  const [facts, technical, settings, history, accountStore] = await Promise.all([
    source("docs/redesign/PRIVACY_FACTS.md"),
    source("docs/redesign/TECHNICAL_DESIGN.md"),
    source("apps/display-web/app/account/settings/page.tsx"),
    source("apps/display-web/app/account/history/page.tsx"),
    source("services/platform-api/internal/database/account_store.go"),
  ]);
  assert.match(facts, /没有 `history_sync_enabled` 用户开关/);
  assert.match(technical, /Q-01 定稿前只做说明，不改变默认记录或删除已有数据/);
  assert.doesNotMatch(settings, /history_sync_enabled|永久删除历史|保留 30 天/);
  assert.match(history, /不会物理删除底层记录/);
  assert.match(accountStore, /UPDATE platform\.view_history SET is_deleted = true/);
});
