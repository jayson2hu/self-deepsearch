import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

async function source(relativePath) {
  return readFile(path.join(repositoryRoot, relativePath), "utf8");
}

test("recommendation and catalog pages declare the product context explicitly", async () => {
  const expectations = new Map([
    ["apps/display-web/app/page.tsx", 'context="recommendation"'],
    ["apps/display-web/app/popular/page.tsx", 'context="recommendation"'],
    ["apps/display-web/app/works/[slug]/page.tsx", 'context="recommendation"'],
    ["apps/display-web/app/search/page.tsx", 'context="catalog"'],
    ["apps/display-web/app/latest/page.tsx", 'context="catalog"'],
    ["apps/display-web/app/recent/page.tsx", 'context="catalog"'],
    ["apps/display-web/components/followed-works-section.tsx", 'context="catalog"'],
  ]);

  for (const [relativePath, signal] of expectations) {
    assert.match(await source(relativePath), new RegExp(signal.replaceAll('"', '\\"')));
  }
});

test("work grids only read and apply private preferences in recommendation context", async () => {
  for (const relativePath of [
    "apps/display-web/components/personalized-work-grid.tsx",
    "apps/display-web/components/discovery-grid.tsx",
  ]) {
    const content = await source(relativePath);
    assert.match(content, /context: WorkGridContext/);
    assert.match(content, /if \(context !== "recommendation"\) return/);
    assert.match(content, /cache: "no-store"/);
    assert.match(content, /context === "recommendation" \? items\.filter/);
    assert.match(content, /context === "recommendation" && preferenceState === "ready" \? hide : undefined/);
    assert.match(content, /已减少这部作品的推荐，仍可搜索找到/);
    assert.match(content, /onClick=\{undoHide\}/);
    assert.match(content, /当前展示完整列表/);
  }
});

test("explicit followed works ignore recommendation preference while admin visibility remains authoritative", async () => {
  const store = await source("services/platform-api/internal/database/account_store.go");
  const followedWorks = store.match(/func \(store \*Store\) ListFollowedWorks[\s\S]*?\n}\n\nfunc \(store \*Store\) SetHiddenWork/);
  assert.ok(followedWorks, "ListFollowedWorks source block is present");
  assert.doesNotMatch(followedWorks[0], /hidden_preferences/);
  assert.match(followedWorks[0], /public_published_works/);

  const openapi = await source("packages/api-contracts/openapi.yaml");
  assert.match(openapi, /Recommendation preferences do not remove works from this explicit list/);
  assert.doesNotMatch(openapi, /followed performers, excluding the user's active hidden preferences/);

  const mock = await source("scripts/release_a_frontend_smoke.mjs");
  const followedMock = mock.match(/url\.pathname === "\/api\/v1\/me\/followed-works"[\s\S]*?next_cursor: null,[\s\S]*?\n\s*}\);/);
  assert.ok(followedMock, "followed works mock block is present");
  assert.match(followedMock[0], /accountState\.followed \? \[MOCK_WORK_ITEM\] : \[\]/);
  assert.doesNotMatch(followedMock[0], /hiddenWorkID/);
});

test("recommendation preference copy describes recoverable recommendation behavior", async () => {
  const card = await source("apps/display-web/components/work-card.tsx");
  const detailButton = await source("apps/display-web/components/hidden-work-button.tsx");
  const manager = await source("apps/display-web/app/account/hidden/page.tsx");
  const guestActions = await source("apps/display-web/components/work-actions.tsx");

  assert.match(card, />不再推荐<\/button>/);
  assert.match(detailButton, /恢复推荐/);
  assert.match(detailButton, /已减少这部作品的推荐，仍可搜索找到/);
  assert.match(manager, /仍可通过搜索、目录、人物或厂牌资料找到/);
  assert.match(guestActions, /登录后设置推荐偏好或纠错/);
});
