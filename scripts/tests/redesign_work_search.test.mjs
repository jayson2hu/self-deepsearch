import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { labelledQueries } from "../../tests/redesign/fixtures/catalog-baseline.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = (relativePath) => readFile(path.join(repositoryRoot, relativePath), "utf8");

test("stage A retains every labelled work-query purpose from the DEV-01 baseline", () => {
  const workQueries = labelledQueries.filter((query) => query.type === "work");
  assert.equal(workQueries.length, 12);
  assert.deepEqual(workQueries.map((query) => query.purpose), [
    "exact_code",
    "normalized_same_code_keeps_versions",
    "performer_name_finds_related_works",
    "performer_alias_finds_related_works",
    "same_name_studios_are_not_merged",
    "japanese_title",
    "no_result",
    "hidden_is_excluded",
    "takedown_is_excluded",
    "draft_is_excluded",
    "localized_alias",
    "query_preserved_after_retry",
  ]);
});

test("display search distinguishes query, cursor, empty result, and dependency states", async () => {
  const api = await source("apps/display-web/lib/api.ts");
  const page = await source("apps/display-web/app/search/page.tsx");
  const loading = await source("apps/display-web/app/search/loading.tsx");

  for (const status of ["ok", "invalid_query", "invalid_cursor", "unavailable"]) {
    assert.match(api, new RegExp(`status: "${status}"`));
  }
  assert.match(page, /当前阶段仅返回已发布作品/);
  assert.match(page, /人物名称、人物别名或厂牌名称会返回关联作品，不会在本页冒充人物或厂牌结果/);
  assert.match(page, /没有找到相关已发布作品/);
  assert.match(page, /这不是“没有结果”；可按原查询条件重试/);
  assert.match(page, /searchHref\(query, cursor\)/);
  assert.match(page, /searchHref\(query\)\}>返回第一页/);
  assert.match(page, /method="get"/);
  assert.doesNotMatch(page, /name="cursor"/);
  assert.match(loading, /正在查询作品资料/);
});

test("search API contract stays work-only and binds validation to query and cursor", async () => {
  const openapi = await source("packages/api-contracts/openapi.yaml");
  const block = openapi.match(/  \/api\/v1\/search\/works:[\s\S]*?(?=\n  \/api\/v1\/works\/popular:)/)?.[0] ?? "";
  assert.match(block, /Searches published work documents/);
  assert.match(block, /performer names and aliases, and studio name/);
  assert.match(block, /Invalid query or query-bound cursor/);
  assert.doesNotMatch(block, /match_reason|total_count|entity_type/);
});
