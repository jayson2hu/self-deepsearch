package database

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func insertEntitySearchPerformer(t *testing.T, h *invitationContractHarness, reviewerID, name, original, romanized string, aliases []string, status string) string {
	t.Helper()
	slug := "rd-performer-" + h.newHash()[:16]
	if status == "draft" {
		var id string
		if err := h.store.pool.QueryRow(h.ctx, `
INSERT INTO platform.performers (
  display_name, name_original, romanized_name, canonical_slug, adult_status, activity_status, publication_status
) VALUES ($1, nullif($2, ''), nullif($3, ''), $4, 'verified', 'active', 'draft')
RETURNING performer_id::text`, name, original, romanized, slug).Scan(&id); err != nil {
			t.Fatalf("insert draft performer %q: %v", name, err)
		}
		return id
	}
	var id, publicationID string
	err := h.store.pool.QueryRow(h.ctx, `
WITH entity AS (
  INSERT INTO platform.performers (
    display_name, name_original, romanized_name, canonical_slug, adult_status, activity_status, publication_status
  ) VALUES ($1, nullif($2, ''), nullif($3, ''), $4, 'verified', 'active', $5)
  RETURNING performer_id
), revision AS (
  INSERT INTO platform.content_revisions (
    entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
    reason, submitted_at, reviewed_at
  ) SELECT 'performer', performer_id, 1, jsonb_build_object('display_name', $1::text),
           'approved', $6::uuid, $7::uuid, 'Synthetic DEV-09 performer search contract', now(), now()
    FROM entity RETURNING revision_id, entity_id
), publication AS (
  INSERT INTO platform.publications (
    entity_type, entity_id, current_revision_id, publication_status, canonical_slug,
    published_at, hidden_at, takedown_at
  ) SELECT 'performer', entity_id, revision_id, $5, $4,
           CASE WHEN $5 = 'published' THEN now() ELSE NULL END,
           CASE WHEN $5 = 'hidden' THEN now() ELSE NULL END,
           CASE WHEN $5 = 'takedown' THEN now() ELSE NULL END
    FROM revision RETURNING publication_id, entity_id
)
SELECT entity_id::text, publication_id::text FROM publication`, name, original, romanized, slug, status, h.actorID, reviewerID).Scan(&id, &publicationID)
	if err != nil {
		t.Fatalf("insert %s performer %q: %v", status, name, err)
	}
	for index, alias := range aliases {
		if _, err := h.store.pool.Exec(h.ctx, `
INSERT INTO platform.performer_aliases (performer_id, alias_type, raw_value, normalized_value)
VALUES ($1::uuid, $2, $3, lower($3::text))`, id, []string{"stage_name", "romanized", "translation"}[index%3], alias); err != nil {
			t.Fatalf("insert performer alias %q: %v", alias, err)
		}
	}
	if _, err := h.store.pool.Exec(h.ctx, `
INSERT INTO platform.search_documents (
  entity_type, entity_id, title, names, aliases, publication_id
) VALUES ('performer', $1::uuid, $2::text, concat_ws(' ', $2::text, $3::text, $4::text), $5::text, $6::uuid)`,
		id, name, original, romanized, strings.Join(aliases, " "), publicationID); err != nil {
		t.Fatalf("insert performer search document %q: %v", name, err)
	}
	return id
}

func insertEntitySearchStudio(t *testing.T, h *invitationContractHarness, reviewerID, name, status string) string {
	t.Helper()
	token := h.newHash()[:16]
	slug := "rd-studio-" + token
	normalized := strings.ToLower(name) + "-" + token
	if status == "draft" {
		var id string
		if err := h.store.pool.QueryRow(h.ctx, `
INSERT INTO platform.studios (name, normalized_name, canonical_slug, publication_status)
VALUES ($1, $2, $3, 'draft') RETURNING studio_id::text`, name, normalized, slug).Scan(&id); err != nil {
			t.Fatalf("insert draft studio %q: %v", name, err)
		}
		return id
	}
	var id, publicationID string
	err := h.store.pool.QueryRow(h.ctx, `
WITH entity AS (
  INSERT INTO platform.studios (name, normalized_name, canonical_slug, publication_status)
  VALUES ($1, $2, $3, $4) RETURNING studio_id
), revision AS (
  INSERT INTO platform.content_revisions (
    entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
    reason, submitted_at, reviewed_at
  ) SELECT 'studio', studio_id, 1, jsonb_build_object('name', $1::text),
           'approved', $5::uuid, $6::uuid, 'Synthetic DEV-09 studio search contract', now(), now()
    FROM entity RETURNING revision_id, entity_id
), publication AS (
  INSERT INTO platform.publications (
    entity_type, entity_id, current_revision_id, publication_status, canonical_slug,
    published_at, hidden_at, takedown_at
  ) SELECT 'studio', entity_id, revision_id, $4, $3,
           CASE WHEN $4 = 'published' THEN now() ELSE NULL END,
           CASE WHEN $4 = 'hidden' THEN now() ELSE NULL END,
           CASE WHEN $4 = 'takedown' THEN now() ELSE NULL END
    FROM revision RETURNING publication_id, entity_id
)
SELECT entity_id::text, publication_id::text FROM publication`, name, normalized, slug, status, h.actorID, reviewerID).Scan(&id, &publicationID)
	if err != nil {
		t.Fatalf("insert %s studio %q: %v", status, name, err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `
INSERT INTO platform.search_documents (entity_type, entity_id, title, studio_name, publication_id)
VALUES ('studio', $1::uuid, $2, $2, $3::uuid)`, id, name, publicationID); err != nil {
		t.Fatalf("insert studio search document %q: %v", name, err)
	}
	return id
}

func performerSearchIDs(items []catalog.PerformerSearchResult) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func studioSearchIDs(items []catalog.StudioSearchResult) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func sortedIDs(values ...string) []string {
	sort.Strings(values)
	return values
}

func TestEntitySearchStageBQueriesPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	reviewerID := h.insertUser(t, h.newEmail("entity-search-reviewer"), "admin", "active")
	haruno := insertEntitySearchPerformer(t, h, reviewerID, "春野澪", "春野みお", "Mio Haruno", []string{"小澪"}, "published")
	aoiA := insertEntitySearchPerformer(t, h, reviewerID, "星野葵", "", "Aoi Hoshino", nil, "published")
	aoiB := insertEntitySearchPerformer(t, h, reviewerID, "星野葵", "", "Aoi Hoshino II", []string{"葵二号"}, "published")
	rei := insertEntitySearchPerformer(t, h, reviewerID, "水無月レイ", "", "Rei Minazuki", []string{"水无月玲"}, "published")
	hiddenPerformer := insertEntitySearchPerformer(t, h, reviewerID, "隐藏人物", "", "", nil, "hidden")
	takedownPerformer := insertEntitySearchPerformer(t, h, reviewerID, "权利下架人物", "", "", nil, "takedown")

	studioA := insertEntitySearchStudio(t, h, reviewerID, "青空制作", "published")
	studioB := insertEntitySearchStudio(t, h, reviewerID, "青空制作", "published")
	draftStudio := insertEntitySearchStudio(t, h, reviewerID, "草稿厂牌", "draft")
	hiddenStudio := insertEntitySearchStudio(t, h, reviewerID, "隐藏厂牌", "hidden")
	takedownStudio := insertEntitySearchStudio(t, h, reviewerID, "权利下架厂牌", "takedown")

	performerCases := []struct {
		query string
		want  []string
	}{
		{"春野澪", []string{haruno}},
		{"Mio Haruno", []string{haruno}},
		{"星野葵", sortedIDs(aoiA, aoiB)},
		{"水無月レイ", []string{rei}},
		{"Rei Minazuki", []string{rei}},
		{"隐藏人物", []string{}},
		{"权利下架人物", []string{}},
	}
	for _, testCase := range performerCases {
		items, err := h.store.SearchPerformers(h.ctx, testCase.query, nil, 100)
		if err != nil {
			t.Fatalf("search performers %q: %v", testCase.query, err)
		}
		got := performerSearchIDs(items)
		if testCase.query == "星野葵" {
			sort.Strings(got)
		}
		if fmt.Sprint(got) != fmt.Sprint(testCase.want) {
			t.Fatalf("performer search %q IDs = %v, want %v", testCase.query, got, testCase.want)
		}
	}

	studioCases := []struct {
		query string
		want  []string
	}{
		{"青空制作", sortedIDs(studioA, studioB)},
		{"草稿厂牌", []string{}},
		{"隐藏厂牌", []string{}},
		{"权利下架厂牌", []string{}},
		{"没有这个厂牌", []string{}},
	}
	for _, testCase := range studioCases {
		items, err := h.store.SearchStudios(h.ctx, testCase.query, nil, 100)
		if err != nil {
			t.Fatalf("search studios %q: %v", testCase.query, err)
		}
		got := studioSearchIDs(items)
		sort.Strings(got)
		if fmt.Sprint(got) != fmt.Sprint(testCase.want) {
			t.Fatalf("studio search %q IDs = %v, want %v", testCase.query, got, testCase.want)
		}
	}
	for _, id := range []string{hiddenPerformer, takedownPerformer, draftStudio, hiddenStudio, takedownStudio} {
		if id == "" {
			t.Fatal("visibility fixture did not receive an ID")
		}
	}
}

func TestEntitySearchRankedCursorContinuesAfterAnchorDisappearsPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	reviewerID := h.insertUser(t, h.newEmail("entity-page-reviewer"), "admin", "active")
	for index := 1; index <= 26; index++ {
		insertEntitySearchPerformer(t, h, reviewerID, fmt.Sprintf("页测人物 %02d", index), "", "", nil, "published")
		insertEntitySearchStudio(t, h, reviewerID, fmt.Sprintf("页测厂牌 %02d", index), "published")
	}

	allPerformers, err := h.store.SearchPerformers(h.ctx, "页测", nil, 100)
	if err != nil || len(allPerformers) != 26 {
		t.Fatalf("load full performer search set: count=%d err=%v", len(allPerformers), err)
	}
	performerAfter := allPerformers[23].Cursor
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.performers SET publication_status = 'hidden' WHERE performer_id = $1::uuid`, performerAfter.ID); err != nil {
		t.Fatalf("hide performer cursor anchor: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `
UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now()
WHERE entity_type = 'performer' AND entity_id = $1::uuid`, performerAfter.ID); err != nil {
		t.Fatalf("hide performer cursor publication: %v", err)
	}
	continuedPerformers, err := h.store.SearchPerformers(h.ctx, "页测", &performerAfter, 100)
	if err != nil || fmt.Sprint(performerSearchIDs(continuedPerformers)) != fmt.Sprint(performerSearchIDs(allPerformers[24:])) {
		t.Fatalf("performer continuation after anchor disappeared = %v, want %v, err=%v", performerSearchIDs(continuedPerformers), performerSearchIDs(allPerformers[24:]), err)
	}

	allStudios, err := h.store.SearchStudios(h.ctx, "页测", nil, 100)
	if err != nil || len(allStudios) != 26 {
		t.Fatalf("load full studio search set: count=%d err=%v", len(allStudios), err)
	}
	studioAfter := allStudios[23].Cursor
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.studios SET publication_status = 'hidden' WHERE studio_id = $1::uuid`, studioAfter.ID); err != nil {
		t.Fatalf("hide studio cursor anchor: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `
UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now()
WHERE entity_type = 'studio' AND entity_id = $1::uuid`, studioAfter.ID); err != nil {
		t.Fatalf("hide studio cursor publication: %v", err)
	}
	continuedStudios, err := h.store.SearchStudios(h.ctx, "页测", &studioAfter, 100)
	if err != nil || fmt.Sprint(studioSearchIDs(continuedStudios)) != fmt.Sprint(studioSearchIDs(allStudios[24:])) {
		t.Fatalf("studio continuation after anchor disappeared = %v, want %v, err=%v", studioSearchIDs(continuedStudios), studioSearchIDs(allStudios[24:]), err)
	}
}
