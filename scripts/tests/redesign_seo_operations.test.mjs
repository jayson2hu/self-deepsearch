import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("SEO center separates technical eligibility from search-engine indexing claims", async () => {
  const page = await readFile(new URL("../../apps/ops-web/app/seo/page.tsx", import.meta.url), "utf8");
  assert.match(page, /技术可索引/);
  assert.match(page, /不代表已被搜索引擎收录/);
  assert.match(page, /进入修订/);
  assert.match(page, /canonical/);
  assert.match(page, /robots/);
});

test("SEO inventory reuses publication policy and fails unknown classifications closed", async () => {
  const store = await readFile(new URL("../../services/platform-api/internal/database/seo_inventory.go", import.meta.url), "utf8");
  assert.match(store, /content_classification = 'unknown' THEN 'classification_unknown'/);
  assert.match(store, /seo_quality_status = 'unknown' THEN 'quality_unknown'/);
  assert.match(store, /publication_status='published' AND content_classification <> 'unknown' AND seo_quality_status='eligible'/);
  assert.match(store, /platform\.entity_media/);
});

test("SEO management routes require operations roles and do not expose a public inventory", async () => {
  const [routes, handler] = await Promise.all([
    readFile(new URL("../../services/platform-api/internal/httpapi/server.go", import.meta.url), "utf8"),
    readFile(new URL("../../services/platform-api/internal/httpapi/seo_handlers.go", import.meta.url), "utf8"),
  ]);
  assert.match(routes, /\/admin\/v1\/seo\/summary/);
  assert.match(routes, /\/admin\/v1\/seo\/pages/);
  assert.doesNotMatch(routes, /\/api\/v1\/site\/seo/);
  assert.match(handler, /requireAdminRole\(w, r, "admin", "owner"\)/);
});

test("SEO issue workflow is bounded, deduplicated and requires successful retest before close", async () => {
  const [migration, store, manager] = await Promise.all([
    readFile(new URL("../../db/migrations/00025_seo_issue_workflow.sql", import.meta.url), "utf8"),
    readFile(new URL("../../services/platform-api/internal/database/seo_issues.go", import.meta.url), "utf8"),
    readFile(new URL("../../apps/ops-web/components/seo-issue-manager.tsx", import.meta.url), "utf8"),
  ]);
  assert.match(migration, /seo_issues_active_rule_url_idx/);
  assert.match(migration, /WHERE status <> 'closed'/);
  assert.match(store, /LIMIT 1001/);
  assert.match(store, /if len\(targets\) > 1000/);
  assert.match(store, /retest_failed/);
  assert.match(store, /publication_status='published' AND content_classification<>'unknown' AND seo_quality_status='eligible'/);
  assert.match(manager, /复测未通过/);
});
