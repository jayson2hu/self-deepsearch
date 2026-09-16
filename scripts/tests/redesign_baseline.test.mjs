import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  baselineMetadata,
  boundaryRequirements,
  coreCatalog,
  labelledQueries,
  makeAssociationWorks,
  makeSitemapEntries,
  performers,
  studios,
  works,
} from "../../tests/redesign/fixtures/catalog-baseline.mjs";

const repositoryRoot = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, repositoryRoot), "utf8");

test("DEV-01 core fixture is synthetic, representative, and within the agreed size", () => {
  assert.equal(baselineMetadata.data_classification, "synthetic");
  assert.equal(baselineMetadata.contains_real_accounts_or_history, false);
  assert.ok(coreCatalog.length >= 30 && coreCatalog.length <= 50, `core fixture count ${coreCatalog.length}`);
  assert.deepEqual(new Set(coreCatalog.map((item) => item.entity_type)), new Set(["work", "performer", "studio"]));
  assert.equal(new Set(coreCatalog.map((item) => item.id)).size, coreCatalog.length);
  assert.equal(works.length, 24);
  assert.equal(performers.length, 7);
  assert.equal(studios.length, 5);
});

test("DEV-01 fixture covers required publication states and edge cases", () => {
  const statuses = new Set(coreCatalog.map((item) => item.publication_status));
  assert.deepEqual(statuses, new Set(["published", "draft", "hidden", "takedown"]));
  for (const entityType of ["work", "performer", "studio"]) {
    const entityStatuses = new Set(coreCatalog.filter((item) => item.entity_type === entityType).map((item) => item.publication_status));
    assert.deepEqual(entityStatuses, new Set(["published", "draft", "hidden", "takedown"]), entityType);
  }
  const edgeCases = new Set(coreCatalog.flatMap((item) => item.edge_cases ?? []));
  for (const required of [
    "long_title", "same_name", "same_code", "no_image", "unknown_release_date",
    "multilingual", "legacy_missing_public_updated_at", "classification_unknown",
  ]) assert.ok(edgeCases.has(required), `missing edge case ${required}`);
  assert.equal(works.filter((item) => item.code === "SAME-004").length, 2);
  assert.ok(works.some((item) => item.release_date === null));
  assert.ok(coreCatalog.some((item) => item.image_url === null));
});

test("DEV-01 labelled query set has stable answers across all planned entity types", () => {
  assert.ok(labelledQueries.length >= 20 && labelledQueries.length <= 30, `query count ${labelledQueries.length}`);
  assert.deepEqual(new Set(labelledQueries.map((item) => item.type)), new Set(["work", "performer", "studio"]));
  const ids = new Set(coreCatalog.map((item) => item.id));
  for (const query of labelledQueries) {
    assert.ok(query.query.length >= 2);
    assert.ok(query.purpose.length > 0);
    for (const id of [...query.expected_ids, ...query.excluded_ids]) assert.ok(ids.has(id), `${query.query}: unknown id ${id}`);
    for (const id of query.expected_ids) {
      const entity = coreCatalog.find((item) => item.id === id);
      assert.equal(entity.entity_type, query.type, `${query.query}: wrong entity type`);
      assert.equal(entity.publication_status, "published", `${query.query}: non-public expected result`);
    }
    for (const id of query.excluded_ids) {
      const entity = coreCatalog.find((item) => item.id === id);
      assert.notEqual(entity.publication_status, "published", `${query.query}: excluded item is public`);
    }
  }
});

test("DEV-01 boundary generators exceed legacy association limits and sitemap split boundary", () => {
  const performerWorks = makeAssociationWorks({ parentType: "performer", count: boundaryRequirements.performer_association_count });
  const studioWorks = makeAssociationWorks({ parentType: "studio", count: boundaryRequirements.studio_association_count });
  assert.equal(performerWorks.length, 101);
  assert.equal(studioWorks.length, 201);
  assert.ok(performerWorks.some((item) => item.release_date === null));
  assert.ok(studioWorks.some((item) => item.release_date === null));
  assert.equal(new Set(performerWorks.map((item) => item.slug)).size, performerWorks.length);
  assert.equal(new Set(studioWorks.map((item) => item.slug)).size, studioWorks.length);

  let count = 0;
  let first;
  let last;
  const slugs = new Set();
  for (const entry of makeSitemapEntries()) {
    first ??= entry;
    last = entry;
    slugs.add(entry.slug);
    count += 1;
  }
  assert.equal(count, 50_001);
  assert.equal(slugs.size, count);
  assert.equal(first.slug, "rd-sitemap-000001");
  assert.equal(last.slug, "rd-sitemap-050001");
  assert.ok(count > boundaryRequirements.sitemap_max_urls_per_file);
});

test("DEV-01 contract snapshot matches current OpenAPI and records planned additions separately", async () => {
  const [openapi, contractText] = await Promise.all([
    read("packages/api-contracts/openapi.yaml"),
    read("tests/redesign/contracts/rd-v1-contract-baseline.json"),
  ]);
  const contract = JSON.parse(contractText);
  assert.equal(contract.review_base_commit, baselineMetadata.review_base_commit);
  assert.equal(contract.schema_version_at_review, baselineMetadata.schema_version_at_review);
  for (const path of contract.current.paths) assert.ok(openapi.includes(`  ${path}:`), `missing current path ${path}`);
  for (const schema of Object.keys(contract.current.detail_schemas)) assert.ok(openapi.includes(`    ${schema}:`), `missing schema ${schema}`);
  for (const path of contract.planned.implemented_new_paths) assert.ok(openapi.includes(`  ${path}:`), `missing implemented additive path ${path}`);
  for (const path of contract.planned.new_paths) assert.ok(!openapi.includes(`  ${path}:`), `planned path already exists without baseline update: ${path}`);
  assert.ok(openapi.includes("public_updated_at:"), "DEV-03 must expose public_updated_at in the contract");
  assert.ok(openapi.includes("studio:"), "DEV-03 must expose the linked studio object");
  assert.ok(contract.planned.compatibility.includes("keep /api/v1/search/works"));
  assert.deepEqual(contract.planned.implemented_auth_continuation.intent_kinds, ["favorite", "follow"]);
  assert.equal(contract.planned.implemented_auth_continuation.intent_ttl_seconds, 900);
  assert.equal(contract.planned.implemented_auth_continuation.automatic_mutation_after_auth, false);
  assert.deepEqual(contract.planned.implemented_ad_states.states, ["empty", "ready", "error", "preview"]);
  assert.equal(contract.planned.implemented_ad_states.default_state, "empty");
  assert.equal(contract.planned.implemented_ad_states.preview_allowed_in_production, false);
  assert.equal(contract.planned.implemented_ad_states.visibility_depends_on_authentication, false);
});

test("AC-29 documentation keeps every DEV and AC identifier traceable", async () => {
  const [decisions, development, acceptance, progress] = await Promise.all([
    read("docs/redesign/DECISIONS.md"),
    read("docs/redesign/DEVELOPMENT_PLAN.md"),
    read("docs/redesign/ACCEPTANCE_PLAN.md"),
    read("docs/redesign/PROGRESS.md"),
  ]);
  for (let index = 1; index <= 15; index += 1) {
    const id = `DEV-${String(index).padStart(2, "0")}`;
    assert.ok(development.includes(id), `development plan missing ${id}`);
    assert.ok(progress.includes(`| ${id} |`), `progress missing ${id}`);
  }
  for (let index = 1; index <= 30; index += 1) {
    const id = `AC-${String(index).padStart(2, "0")}`;
    assert.ok(acceptance.includes(`| ${id} |`), `acceptance plan missing ${id}`);
  }
  for (let index = 1; index <= 9; index += 1) {
    const id = `D-${String(index).padStart(2, "0")}`;
    assert.ok(decisions.includes(`| ${id} |`), `decisions missing ${id}`);
  }
});
