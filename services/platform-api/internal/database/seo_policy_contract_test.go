package database

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSEOPolicyPostgresFailsClosedAndAuditsPreviewPermission(t *testing.T) {
	h := newInvitationContractHarness(t)
	manifest := contractMediaManifest(t, h, "work", "published")
	manifest.ContentClassification = "non_explicit"
	manifest.ExternalPreviewPermission = "allow"
	if _, err := h.store.RegisterMediaManifest(h.ctx, manifest, h.actorID, "seo-policy-media", contractNow()); err != nil {
		t.Fatalf("register reviewed media: %v", err)
	}

	reviewerID := h.insertUser(t, h.newEmail("seo-reviewer"), "admin", "active")
	slug := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(manifest.AssetID[:8], "_", "-")))
	var revisionID string
	if err := h.store.pool.QueryRow(h.ctx, `
INSERT INTO platform.content_revisions (
  entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
  reason, submitted_at, reviewed_at
) VALUES ('work', $1::uuid, 1,
  '{"content_classification":"unknown","seo_quality_status":"unknown"}'::jsonb,
  'approved', $2::uuid, $3::uuid, 'DEV-14 SEO policy contract', now(), now())
RETURNING revision_id::text`, manifest.EntityID, h.actorID, reviewerID).Scan(&revisionID); err != nil {
		t.Fatalf("insert SEO revision: %v", err)
	}
	if _, err := h.store.pool.Exec(h.ctx, `
INSERT INTO platform.publications (
  entity_type, entity_id, current_revision_id, publication_status, canonical_slug, published_at
) VALUES ('work', $1::uuid, $2::uuid, 'published', $3, now())`, manifest.EntityID, revisionID, slug); err != nil {
		t.Fatalf("publish unknown SEO entity: %v", err)
	}

	unknown, err := h.store.WorkBySlug(h.ctx, slug)
	if err != nil || unknown.SEO.Indexable || unknown.SEO.SitemapEligible || unknown.SEO.ImagePreviewMode != "none" {
		t.Fatalf("unknown SEO policy did not fail closed: detail=%#v err=%v", unknown.SEO, err)
	}
	assertSitemapSlug(t, h, slug, false)

	if _, err := h.store.pool.Exec(h.ctx, `
UPDATE platform.publications
SET content_classification = 'non_explicit', seo_quality_status = 'eligible',
    seo_reviewed_at = now(), seo_reviewed_by = $2::uuid
WHERE entity_type = 'work' AND entity_id = $1::uuid`, manifest.EntityID, reviewerID); err != nil {
		t.Fatalf("review non-explicit entity: %v", err)
	}
	standard, err := h.store.WorkBySlug(h.ctx, slug)
	if err != nil || !standard.SEO.Indexable || standard.SEO.AdultRating || standard.SEO.ImagePreviewMode != "standard" || len(standard.Images) == 0 || !standard.Images[0].ExternalPreviewAllowed {
		t.Fatalf("reviewed non-explicit policy mismatch: detail=%#v images=%#v err=%v", standard.SEO, standard.Images, err)
	}
	assertSitemapSlug(t, h, slug, true)

	if _, err := h.store.pool.Exec(h.ctx, `
UPDATE platform.publications SET content_classification = 'explicit', updated_at = now()
WHERE entity_type = 'work' AND entity_id = $1::uuid`, manifest.EntityID); err != nil {
		t.Fatalf("review explicit entity: %v", err)
	}
	explicit, err := h.store.WorkBySlug(h.ctx, slug)
	if err != nil || !explicit.SEO.Indexable || !explicit.SEO.AdultRating || explicit.SEO.ImagePreviewMode != "none" {
		t.Fatalf("explicit policy mismatch: detail=%#v err=%v", explicit.SEO, err)
	}

	_, err = h.store.pool.Exec(h.ctx, `
UPDATE platform.media_assets SET content_classification = 'explicit'
WHERE asset_id = $1::uuid`, manifest.AssetID)
	var databaseError *pgconn.PgError
	if !asPgError(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatalf("explicit + allow media should violate check constraint: %v", err)
	}
}

func assertSitemapSlug(t *testing.T, h *invitationContractHarness, slug string, want bool) {
	t.Helper()
	items, err := h.store.SitemapEntries(h.ctx, "work", 0, 50000)
	if err != nil {
		t.Fatalf("read work sitemap: %v", err)
	}
	found := false
	for _, item := range items {
		if item.Slug == slug {
			found = true
			break
		}
	}
	if found != want {
		t.Fatalf("sitemap slug %q found=%v want=%v", slug, found, want)
	}
}

func asPgError(err error, target **pgconn.PgError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(*pgconn.PgError)
	if ok {
		*target = value
	}
	return ok
}
