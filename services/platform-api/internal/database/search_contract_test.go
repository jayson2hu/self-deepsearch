package database

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

func insertSearchContractWork(t *testing.T, harness *invitationContractHarness, code, title, names, aliases, studioName, status string) string {
	t.Helper()
	compact := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToUpper(code))
	if status == "draft" {
		var id string
		if err := harness.store.pool.QueryRow(harness.ctx, `
INSERT INTO platform.works (canonical_code, compact_code, title, publication_status)
VALUES ($1, $2, $3, 'draft') RETURNING work_id::text`, code, compact, title).Scan(&id); err != nil {
			t.Fatalf("insert draft search work %s: %v", code, err)
		}
		return id
	}
	reviewerID := harness.insertUser(t, harness.newEmail("search-reviewer"), "admin", "active")
	slug := fmt.Sprintf("rd-search-%s-%s", strings.ToLower(strings.ReplaceAll(code, "_", "-")), harness.newHash()[:12])
	var id string
	err := harness.store.pool.QueryRow(harness.ctx, `
WITH work AS (
  INSERT INTO platform.works (canonical_code, compact_code, title, publication_status)
  VALUES ($1, $2, $3, $7) RETURNING work_id
), revision AS (
  INSERT INTO platform.content_revisions (
    entity_type, entity_id, version, payload, revision_status, author_id,
    reviewer_id, reason, submitted_at, reviewed_at
  ) SELECT 'work', work_id, 1, jsonb_build_object('canonical_code', $1::text, 'title', $3::text),
           'approved', $8::uuid, $9::uuid, 'Synthetic DEV-08 search contract', now(), now()
    FROM work RETURNING revision_id, entity_id
), publication AS (
  INSERT INTO platform.publications (
    entity_type, entity_id, current_revision_id, publication_status, canonical_slug,
    published_at, hidden_at, takedown_at
  ) SELECT 'work', entity_id, revision_id, $7, $10,
           CASE WHEN $7 = 'published' THEN now() ELSE NULL END,
           CASE WHEN $7 = 'hidden' THEN now() ELSE NULL END,
           CASE WHEN $7 = 'takedown' THEN now() ELSE NULL END
    FROM revision RETURNING publication_id, entity_id
), document AS (
  INSERT INTO platform.search_documents (
    entity_type, entity_id, canonical_code, compact_code, title, names, aliases,
    studio_name, publication_id
  ) SELECT 'work', entity_id, $1, $2, $3, $4, $5, $6, publication_id
    FROM publication RETURNING entity_id
)
SELECT entity_id::text FROM document`, code, compact, title, names, aliases, studioName, status, harness.actorID, reviewerID, slug).Scan(&id)
	if err != nil {
		t.Fatalf("insert %s search work %s: %v", status, code, err)
	}
	return id
}

func searchContractIDs(t *testing.T, harness *invitationContractHarness, raw, compact string) []string {
	t.Helper()
	items, err := harness.store.SearchWorks(harness.ctx, raw, compact, "", 50)
	if err != nil {
		t.Fatalf("search %q: %v", raw, err)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	sort.Strings(ids)
	return ids
}

func sortedSearchIDs(ids ...string) []string {
	sort.Strings(ids)
	return ids
}

func TestSearchWorksStageAQueriesPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	long := insertSearchContractWork(t, h, "LONG-001", "春日海边摄影计划特别增补版", "春野澪", "小澪", "其他制作", "published")
	alias := insertSearchContractWork(t, h, "ALIAS-007", "别名关联查询样本", "春野澪", "小澪", "其他制作", "published")
	multi := insertSearchContractWork(t, h, "SEARCH-021", "青空人物关联作品", "春野澪", "小澪", "青空制作", "published")
	versionA := insertSearchContractWork(t, h, "SAME-004", "同番号版本 A", "其他人物", "", "其他制作", "published")
	versionB := insertSearchContractWork(t, h, "SAME-004", "同番号版本 B", "其他人物", "", "其他制作", "published")
	japanese := insertSearchContractWork(t, h, "JP-006", "夏の記録", "水無月レイ", "水无月玲", "其他制作", "published")
	localized := insertSearchContractWork(t, h, "SEARCH-022", "水无月别名命中作品", "水無月レイ", "水无月玲", "其他制作", "published")
	studioA := insertSearchContractWork(t, h, "STUDIO-011", "同名厂牌版本一", "其他人物", "", "青空制作", "published")
	studioB := insertSearchContractWork(t, h, "STUDIO-012", "同名厂牌版本二", "其他人物", "", "青空制作", "published")
	hidden := insertSearchContractWork(t, h, "HIDDEN-009", "管理员隐藏作品", "", "", "", "hidden")
	takedown := insertSearchContractWork(t, h, "TAKE-010", "权利下架作品", "", "", "", "takedown")
	draft := insertSearchContractWork(t, h, "DRAFT-008", "未发布草稿作品", "", "", "", "draft")

	cases := []struct {
		name, raw, compact string
		want               []string
	}{
		{"exact code", "LONG-001", "LONG001", []string{long}},
		{"normalized code keeps versions", "same 004", "SAME004", []string{versionA, versionB}},
		{"performer name finds works", "春野澪", "春野澪", []string{long, alias, multi}},
		{"performer alias finds works", "小澪", "小澪", []string{long, alias, multi}},
		{"studio name keeps distinct works", "青空制作", "青空制作", []string{multi, studioA, studioB}},
		{"Japanese title", "夏の記録", "夏の記録", []string{japanese}},
		{"localized alias", "水无月玲", "水无月玲", []string{japanese, localized}},
		{"no result", "不存在的番号", "不存在的番号", []string{}},
		{"hidden excluded", "HIDDEN-009", "HIDDEN009", []string{}},
		{"takedown excluded", "TAKE-010", "TAKE010", []string{}},
		{"draft excluded", "DRAFT-008", "DRAFT008", []string{}},
		{"normalized code-like query does not fuzzy-match unrelated published codes", "draft 008", "DRAFT008", []string{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := searchContractIDs(t, h, testCase.raw, testCase.compact)
			want := sortedSearchIDs(testCase.want...)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("search %q IDs = %v, want %v", testCase.raw, got, want)
			}
		})
	}
	for _, excluded := range []string{hidden, takedown, draft} {
		if excluded == "" {
			t.Fatal("visibility fixture did not receive an ID")
		}
	}
}
