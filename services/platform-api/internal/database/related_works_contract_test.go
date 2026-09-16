package database

import (
	"errors"
	"fmt"
	"testing"

	"self-deepsearch/services/platform-api/internal/catalog"
)

func relatedWorkIDs(items []catalog.RelatedWorkResult) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func collectRelatedWorks(t *testing.T, query func(*catalog.RelatedWorkCursor, int) ([]catalog.RelatedWorkResult, error)) []catalog.RelatedWorkResult {
	t.Helper()
	items := make([]catalog.RelatedWorkResult, 0)
	var after *catalog.RelatedWorkCursor
	for pageNumber := 1; pageNumber <= 20; pageNumber++ {
		page, err := query(after, 24)
		if err != nil {
			t.Fatalf("collect related works page %d: %v", pageNumber, err)
		}
		items = append(items, page...)
		if len(page) < 24 {
			return items
		}
		cursor := page[len(page)-1].Cursor
		after = &cursor
	}
	t.Fatal("related works pagination did not terminate")
	return nil
}

func TestRelatedWorksBoundaryPaginationPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	reviewerID := h.insertUser(t, h.newEmail("related-reviewer"), "admin", "active")
	performerID := insertEntitySearchPerformer(t, h, reviewerID, "DEV10 分页人物", "", "", nil, "published")
	studioID := insertEntitySearchStudio(t, h, reviewerID, "DEV10 分页厂牌", "published")
	var performerSlug, studioSlug string
	if err := h.store.pool.QueryRow(h.ctx, `SELECT canonical_slug FROM platform.performers WHERE performer_id = $1::uuid`, performerID).Scan(&performerSlug); err != nil {
		t.Fatalf("read performer slug: %v", err)
	}
	if err := h.store.pool.QueryRow(h.ctx, `SELECT canonical_slug FROM platform.studios WHERE studio_id = $1::uuid`, studioID).Scan(&studioSlug); err != nil {
		t.Fatalf("read studio slug: %v", err)
	}
	token := h.newHash()[:16]
	var workCount, publicationCount, linkCount int
	err := h.store.pool.QueryRow(h.ctx, `
WITH input AS (
  SELECT sequence,
         'RD10-' || upper($1::text) || '-' || lpad(sequence::text, 3, '0') AS code,
         'RD10' || upper($1::text) || lpad(sequence::text, 3, '0') AS compact,
         'DEV10 关联作品 ' || lpad(sequence::text, 3, '0') AS title,
         CASE WHEN sequence > 190 THEN NULL ELSE DATE '2026-09-01' - ((sequence - 1) / 3)::integer END AS release_date,
         'rd10-' || lower($1::text) || '-' || lpad(sequence::text, 3, '0') AS slug
  FROM generate_series(1, 201) AS sequence
), inserted_works AS (
  INSERT INTO platform.works (
    canonical_code, compact_code, title, release_date, studio_id, publication_status
  )
  SELECT code, compact, title, release_date, $2::uuid, 'published' FROM input
  RETURNING work_id, canonical_code
), joined AS (
  SELECT input.*, work.work_id
  FROM input JOIN inserted_works AS work ON work.canonical_code = input.code
), revisions AS (
  INSERT INTO platform.content_revisions (
    entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
    reason, submitted_at, reviewed_at
  )
  SELECT 'work', work_id, 1, jsonb_build_object('canonical_code', code, 'title', title),
         'approved', $3::uuid, $4::uuid, 'Synthetic DEV-10 related works contract', now(), now()
  FROM joined
  RETURNING revision_id, entity_id
), publications AS (
  INSERT INTO platform.publications (
    entity_type, entity_id, current_revision_id, publication_status, canonical_slug, published_at
  )
  SELECT 'work', joined.work_id, revisions.revision_id, 'published', joined.slug,
         TIMESTAMPTZ '2026-09-16 12:00:00+00'
  FROM joined JOIN revisions ON revisions.entity_id = joined.work_id
  RETURNING entity_id
), links AS (
  INSERT INTO platform.work_performers (work_id, performer_id, role, position)
  SELECT work_id, $5::uuid, 'performer', 0 FROM joined WHERE sequence <= 101
  RETURNING work_id
)
SELECT (SELECT count(*) FROM inserted_works), (SELECT count(*) FROM publications), (SELECT count(*) FROM links)`,
		token, studioID, h.actorID, reviewerID, performerID).Scan(&workCount, &publicationCount, &linkCount)
	if err != nil || workCount != 201 || publicationCount != 201 || linkCount != 101 {
		t.Fatalf("insert DEV-10 fixtures: works=%d publications=%d links=%d err=%v", workCount, publicationCount, linkCount, err)
	}

	allPerformer, err := h.store.PerformerWorksBySlug(h.ctx, performerSlug, nil, 500)
	if err != nil || len(allPerformer) != 101 {
		t.Fatalf("performer boundary set: count=%d err=%v", len(allPerformer), err)
	}
	pagedPerformer := collectRelatedWorks(t, func(after *catalog.RelatedWorkCursor, limit int) ([]catalog.RelatedWorkResult, error) {
		return h.store.PerformerWorksBySlug(h.ctx, performerSlug, after, limit)
	})
	if fmt.Sprint(relatedWorkIDs(pagedPerformer)) != fmt.Sprint(relatedWorkIDs(allPerformer)) {
		t.Fatal("performer pagination has duplicates, gaps or unstable order")
	}

	allStudio, err := h.store.StudioWorksBySlug(h.ctx, studioSlug, nil, 500)
	if err != nil || len(allStudio) != 201 {
		t.Fatalf("studio boundary set: count=%d err=%v", len(allStudio), err)
	}
	pagedStudio := collectRelatedWorks(t, func(after *catalog.RelatedWorkCursor, limit int) ([]catalog.RelatedWorkResult, error) {
		return h.store.StudioWorksBySlug(h.ctx, studioSlug, after, limit)
	})
	if fmt.Sprint(relatedWorkIDs(pagedStudio)) != fmt.Sprint(relatedWorkIDs(allStudio)) {
		t.Fatal("studio pagination has duplicates, gaps or unstable order")
	}
	unknownDates := 0
	for _, item := range allStudio {
		if item.ReleaseDate == nil {
			unknownDates++
		}
		if item.StudioName != "DEV10 分页厂牌" || item.Slug == "" {
			t.Fatalf("related work lost real studio or href source: %#v", item.WorkSummary)
		}
	}
	if unknownDates != 11 || allStudio[len(allStudio)-1].ReleaseDate != nil {
		t.Fatalf("unknown release dates should sort last, count=%d", unknownDates)
	}

	anchor := allPerformer[23].Cursor
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.works SET publication_status = 'hidden' WHERE work_id = $1::uuid`, anchor.ID); err != nil {
		t.Fatalf("hide cursor anchor work: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now() WHERE entity_type = 'work' AND entity_id = $1::uuid`, anchor.ID); err != nil {
		t.Fatalf("hide cursor anchor publication: %v", err)
	}
	continued, err := h.store.PerformerWorksBySlug(h.ctx, performerSlug, &anchor, 500)
	if err != nil || fmt.Sprint(relatedWorkIDs(continued)) != fmt.Sprint(relatedWorkIDs(allPerformer[24:])) {
		t.Fatalf("continuation after anchor disappeared = %v, want %v, err=%v", relatedWorkIDs(continued), relatedWorkIDs(allPerformer[24:]), err)
	}

	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.performers SET publication_status = 'hidden' WHERE performer_id = $1::uuid`, performerID); err != nil {
		t.Fatalf("hide parent performer: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now() WHERE entity_type = 'performer' AND entity_id = $1::uuid`, performerID); err != nil {
		t.Fatalf("hide parent performer publication: %v", err)
	}
	if _, err := h.store.PerformerWorksBySlug(h.ctx, performerSlug, nil, 24); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("hidden parent error = %v, want catalog.ErrNotFound", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.studios SET publication_status = 'hidden' WHERE studio_id = $1::uuid`, studioID); err != nil {
		t.Fatalf("hide parent studio: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), updated_at = now() WHERE entity_type = 'studio' AND entity_id = $1::uuid`, studioID); err != nil {
		t.Fatalf("hide parent studio publication: %v", err)
	}
	if _, err := h.store.StudioWorksBySlug(h.ctx, studioSlug, nil, 24); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("hidden studio error = %v, want catalog.ErrNotFound", err)
	}
}
