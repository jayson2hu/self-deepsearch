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

test("SEO report import is owner-only, bounded and keeps real data separate from demos", async () => {
  const [handler, migration, manager, proxy] = await Promise.all([
    readFile(new URL("../../services/platform-api/internal/httpapi/seo_report_handlers.go", import.meta.url), "utf8"),
    readFile(new URL("../../db/migrations/00026_seo_report_imports.sql", import.meta.url), "utf8"),
    readFile(new URL("../../apps/ops-web/components/seo-report-manager.tsx", import.meta.url), "utf8"),
    readFile(new URL("../../apps/ops-web/app/admin/v1/[...path]/route.ts", import.meta.url), "utf8"),
  ]);
  assert.match(handler, /requireAdminRole\(w, r, "owner"\)/);
  assert.match(handler, /maxSEOReportBytes = 5 \* 1024 \* 1024/);
  assert.match(handler, /maxSEOReportRows = 10000/);
  assert.match(handler, /strings\.ContainsRune\("=\+-@"/);
  assert.match(migration, /dataset_kind text NOT NULL CHECK \(dataset_kind IN \('real', 'demo'\)\)/);
  assert.match(migration, /UNIQUE \(report_type, dataset_kind, property_uri, file_sha256\)/);
  assert.match(manager, /真实授权导出/);
  assert.match(manager, /演示数据/);
  assert.match(manager, /尚未导入真实报表/);
  assert.match(proxy, /5 \* 1024 \* 1024 \+ 128 \* 1024/);
});
