import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { labelledQueries } from "../../tests/redesign/fixtures/catalog-baseline.mjs";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = (relativePath) => readFile(path.join(repositoryRoot, relativePath), "utf8");

test("DEV-01 carries the complete stage B performer and studio query set", () => {
  const performers = labelledQueries.filter((query) => query.type === "performer");
  const studios = labelledQueries.filter((query) => query.type === "studio");
  assert.equal(performers.length, 7);
  assert.equal(studios.length, 5);
  assert.deepEqual(performers.map((query) => query.purpose), [
    "canonical_name", "romanized_alias", "same_name_entities_not_merged", "japanese_name",
    "latin_alias", "hidden_performer_excluded", "takedown_performer_excluded",
  ]);
  assert.deepEqual(studios.map((query) => query.purpose), [
    "same_name_studios_not_merged", "draft_studio_excluded", "hidden_studio_excluded",
    "takedown_studio_excluded", "studio_no_result",
  ]);
});

test("OpenAPI publishes real performer and studio search contracts before UI tabs", async () => {
  const openapi = await source("packages/api-contracts/openapi.yaml");
  const generated = await source("packages/api-contracts/src/generated.ts");
  for (const entity of ["performers", "studios"]) {
    assert.match(openapi, new RegExp(`  /api/v1/search/${entity}:`));
  }
  assert.match(openapi, /PerformerSearchResponse:/);
  assert.match(openapi, /StudioSearchResponse:/);
  assert.match(openapi, /Versioned ranked cursor bound to this entity type and normalized query/);
  assert.match(openapi, /stably ordered by match class, normalized display name, and UUID/);
  assert.match(openapi, /stably ordered by match class, normalized name, and UUID/);
  assert.match(generated, /export type PerformerSearchResponse/);
  assert.match(generated, /export type StudioSearchResponse/);
});

test("repository ranks public entities and continues from cursor tuples without anchor lookup", async () => {
  const catalog = await source("services/platform-api/internal/catalog/catalog.go");
  const store = await source("services/platform-api/internal/database/store.go");
  const pagination = await source("services/platform-api/internal/httpapi/pagination.go");

  assert.match(catalog, /SearchPerformers\(context\.Context, string, \*SearchCursor, int\)/);
  assert.match(catalog, /SearchStudios\(context\.Context, string, \*SearchCursor, int\)/);
  assert.match(store, /FROM platform\.public_published_performers/);
  assert.match(store, /FROM platform\.public_published_studios/);
  assert.match(store, /JOIN platform\.public_search_documents/);
  assert.match(store, /ORDER BY performer\.search_rank, performer\.search_name, performer\.performer_id/);
  assert.match(store, /ORDER BY studio\.search_rank, studio\.search_name, studio\.studio_id/);
  assert.match(store, /\(performer\.search_rank, performer\.search_name, performer\.performer_id\) >/);
  assert.match(store, /\(studio\.search_rank, studio\.search_name, studio\.studio_id\) >/);
  assert.doesNotMatch(store.match(/func \(store \*Store\) SearchPerformers[\s\S]*?func \(store \*Store\) SearchStudios/)?.[0] ?? "", /cursor_anchor/);
  assert.match(pagination, /Version: 1, Scope: scope, Rank: last\.Rank, Name: last\.Name, ID: last\.ID/);
  assert.match(pagination, /cursor\.Scope != scope/);
});

test("search page validates type before dispatch and preserves type across form, retry, and pagination", async () => {
  const page = await source("apps/display-web/app/search/page.tsx");
  const api = await source("apps/display-web/lib/api.ts");
  assert.match(page, /const validType = searchTypes\.some/);
  assert.match(page, /if \(!validType\) result = \{ status: "invalid_type"/);
  assert.match(page, /else if \(activeType === "performer"\) result = await searchPerformers/);
  assert.match(page, /else if \(activeType === "studio"\) result = await searchStudios/);
  assert.match(page, /aria-label="搜索类型"/);
  assert.match(page, /name="type" value=\{activeType\}/);
  assert.match(page, /searchHref\(displayedQuery, nextCursor, activeType\)/);
  assert.match(page, /没有向未知搜索端点发送请求/);
  assert.match(api, /"\/api\/v1\/search\/performers"/);
  assert.match(api, /"\/api\/v1\/search\/studios"/);
});

test("new entity handlers keep their metrics separate from work zero-result metrics", async () => {
  const handlers = await source("services/platform-api/internal/httpapi/handlers.go");
  for (const functionName of ["searchPerformers", "searchStudios"]) {
    const block = handlers.match(new RegExp(`func \\(s \\*Server\\) ${functionName}[\\s\\S]*?(?=\\nfunc \\(s \\*Server\\))`))?.[0] ?? "";
    assert.match(block, /readRankedSearchCursor/);
    assert.doesNotMatch(block, /recordSearchOutcome/);
  }
});
