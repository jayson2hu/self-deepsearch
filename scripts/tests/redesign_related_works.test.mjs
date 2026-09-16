import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { boundaryRequirements, makeAssociationWorks } from "../../tests/redesign/fixtures/catalog-baseline.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = (relativePath) => readFile(path.join(root, relativePath), "utf8");

test("DEV-10 keeps the 101/201 fixtures with unknown dates and unique public href sources", () => {
  const performer = makeAssociationWorks({ parentType: "performer", count: boundaryRequirements.performer_association_count });
  const studio = makeAssociationWorks({ parentType: "studio", count: boundaryRequirements.studio_association_count });
  assert.equal(performer.length, 101);
  assert.equal(studio.length, 201);
  assert.ok(performer.some((item) => item.release_date === null));
  assert.ok(studio.some((item) => item.release_date === null));
  assert.equal(new Set([...performer, ...studio].map((item) => `/works/${item.slug}`)).size, 302);
});

test("OpenAPI exposes both parent-bound pages without a total contract", async () => {
  const openapi = await source("packages/api-contracts/openapi.yaml");
  for (const entity of ["performers", "studios"]) {
    assert.match(openapi, new RegExp(`  /api/v1/${entity}/\\{slug\\}/works:`));
  }
  assert.match(openapi, /The response intentionally has no total count/);
  assert.match(openapi, /release date descending with unknown dates last, then publication time descending and work ID ascending/);
});

test("repository uses the complete tuple and validates the public parent", async () => {
  const catalog = await source("services/platform-api/internal/catalog/catalog.go");
  const store = await source("services/platform-api/internal/database/store.go");
  const pagination = await source("services/platform-api/internal/httpapi/pagination.go");
  assert.match(catalog, /PerformerWorksBySlug\(context\.Context, string, \*RelatedWorkCursor, int\)/);
  assert.match(catalog, /StudioWorksBySlug\(context\.Context, string, \*RelatedWorkCursor, int\)/);
  assert.match(store, /platform\.public_published_performers WHERE canonical_slug/);
  assert.match(store, /platform\.public_published_studios WHERE canonical_slug/);
  assert.match(store, /ORDER BY work\.release_date DESC NULLS LAST, work\.published_at DESC, work\.work_id ASC/);
  assert.match(store, /work\.published_at = \$5::timestamptz AND work\.work_id > \$6::uuid/);
  assert.match(pagination, /relatedWorksCursorScope/);
  assert.match(pagination, /ReleaseDate: releaseDate/);
  assert.match(pagination, /PublishedAt: last\.PublishedAt\.UTC\(\)\.Format/);
});

test("detail pages fetch related works separately and render direct next-page links", async () => {
  const [performer, studio, api] = await Promise.all([
    source("apps/display-web/app/performers/[slug]/page.tsx"),
    source("apps/display-web/app/studios/[slug]/page.tsx"),
    source("apps/display-web/lib/api.ts"),
  ]);
  assert.match(api, /\/api\/v1\/\$\{entityType\}\/\$\{encodeURIComponent\(slug\)\}\/works/);
  assert.match(performer, /getPerformerWorks\(slug, cursor\)/);
  assert.match(studio, /getStudioWorks\(slug, cursor\)/);
  for (const page of [performer, studio]) {
    assert.match(page, /next_cursor/);
    assert.match(page, />下一页<\/Link>/);
    assert.match(page, /分页地址已失效/);
    assert.doesNotMatch(page, /共 \{result\.data\.works\.length\}/);
  }
});
