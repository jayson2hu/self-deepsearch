package database

import (
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/operations"
)

func TestPublishEntityPostgresUnknownSEOContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	now := contractNow()
	reviewerID := h.insertUser(t, h.newEmail("publish-reviewer"), "admin", "active")
	code := "PUB-" + strings.ToUpper(h.newHash()[:12])
	slug := "publish-contract-" + h.newHash()[:12]
	title := "Synthetic publication SQL contract"
	summary := "Verifies that unknown SEO policy can be published without a PostgreSQL parameter type conflict."
	sourceTitle := "Synthetic publication contract source"

	entity, err := h.store.CreateWork(h.ctx, operations.WorkInput{
		Code: code, Title: title, ReleaseDate: &now, Summary: &summary, PerformerIDs: []string{},
		Sources: []operations.SourceEvidenceInput{{SourceType: "other", SourceTitle: &sourceTitle, CheckedAt: now}},
		Reason:  "Synthetic publication SQL contract",
	}, h.actorID, "publish-create-"+h.newHash()[:24], now)
	if err != nil {
		t.Fatalf("create publication contract work: %v", err)
	}

	var taskID string
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT review_task_id::text FROM collector.review_tasks
WHERE entity_type = 'work' AND entity_id = $1::uuid AND status = 'pending'`, entity.ID).Scan(&taskID); err != nil {
		t.Fatalf("find publication review task: %v", err)
	}
	if _, err := h.store.ClaimReviewTask(h.ctx, taskID, reviewerID, "publish-claim-"+h.newHash()[:24], now); err != nil {
		t.Fatalf("claim publication review: %v", err)
	}
	if _, err := h.store.DecideReviewTask(h.ctx, taskID, reviewerID, true, "Approve synthetic publication", "publish-approve-"+h.newHash()[:24], now); err != nil {
		t.Fatalf("approve publication review: %v", err)
	}

	published, err := h.store.PublishEntity(h.ctx, entity.ID, operations.PublishInput{
		EntityType: "work", RevisionID: entity.RevisionID, CanonicalSlug: slug, Reason: "Publish synthetic contract",
	}, reviewerID, "publish-final-"+h.newHash()[:24], now)
	if err != nil {
		t.Fatalf("publish unknown SEO policy: %v", err)
	}
	if published.Status != "published" || published.CanonicalSlug != slug {
		t.Fatalf("unexpected published entity: %#v", published)
	}

	var classification, quality string
	var reviewedAt, reviewedBy *string
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT content_classification, seo_quality_status, seo_reviewed_at::text, seo_reviewed_by::text
FROM platform.publications WHERE entity_type = 'work' AND entity_id = $1::uuid`, entity.ID).
		Scan(&classification, &quality, &reviewedAt, &reviewedBy); err != nil {
		t.Fatalf("read publication SEO state: %v", err)
	}
	if classification != "unknown" || quality != "unknown" || reviewedAt != nil || reviewedBy != nil {
		t.Fatalf("unknown SEO policy did not fail closed: classification=%q quality=%q reviewed_at=%v reviewed_by=%v", classification, quality, reviewedAt, reviewedBy)
	}
}
