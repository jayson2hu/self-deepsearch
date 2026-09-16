package database

import (
	"testing"
	"time"
)

func TestFeedbackOwnerVisibilityAndInternalReasonPostgres(t *testing.T) {
	h := newInvitationContractHarness(t)
	otherUserID := h.insertUser(t, h.newEmail("feedback-other"), "user", "active")
	reviewerID := h.insertUser(t, h.newEmail("feedback-reviewer"), "admin", "active")
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	ownerItem, err := h.store.CreateFeedback(h.ctx, h.actorID, nil, nil, "correction", "仅本人可见的反馈", nil, now)
	if err != nil {
		t.Fatalf("create owner feedback: %v", err)
	}
	otherItem, err := h.store.CreateFeedback(h.ctx, otherUserID, nil, nil, "other", "其他用户反馈", nil, now.Add(time.Second))
	if err != nil {
		t.Fatalf("create other feedback: %v", err)
	}
	if _, err := h.store.ReviewFeedback(h.ctx, ownerItem.ID, "accepted", reviewerID, "内部审核理由不得下发给用户", "dev11-feedback-review-"+h.newHash()[:16], now.Add(time.Minute)); err != nil {
		t.Fatalf("review owner feedback: %v", err)
	}

	ownerItems, err := h.store.ListFeedback(h.ctx, h.actorID, 100)
	if err != nil || len(ownerItems) != 1 || ownerItems[0].ID != ownerItem.ID || ownerItems[0].ReviewStatus != "accepted" {
		t.Fatalf("owner feedback list = %#v err=%v", ownerItems, err)
	}
	if ownerItems[0].Reviewer != nil || ownerItems[0].ReviewedAt != nil || ownerItems[0].Submitter != "" {
		t.Fatalf("owner list leaked review-only fields: %#v", ownerItems[0])
	}
	otherItems, err := h.store.ListFeedback(h.ctx, otherUserID, 100)
	if err != nil || len(otherItems) != 1 || otherItems[0].ID != otherItem.ID {
		t.Fatalf("other feedback list = %#v err=%v", otherItems, err)
	}

	var internalReason string
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT reason FROM audit.audit_logs
WHERE action = 'feedback.review' AND object_id = $1::uuid
ORDER BY audit_id DESC LIMIT 1`, ownerItem.ID).Scan(&internalReason); err != nil {
		t.Fatalf("read internal feedback audit reason: %v", err)
	}
	if internalReason != "内部审核理由不得下发给用户" {
		t.Fatalf("unexpected internal reason: %q", internalReason)
	}
}
