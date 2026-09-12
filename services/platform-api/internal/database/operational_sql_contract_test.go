package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/identity"
	"self-deepsearch/services/platform-api/internal/operations"
)

// These repository contracts use the restricted, disposable PostgreSQL
// harness. Shared settings are restored; synthetic rows and audit evidence
// remain in the test database. Do not run these tests in parallel.
func TestPostgresOperationalEmailFailureCircuitContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	var previousFailures int
	var previousUntil *time.Time
	var previousUpdated time.Time
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT consecutive_failures, circuit_open_until, updated_at
FROM platform.email_delivery_state WHERE singleton`).Scan(&previousFailures, &previousUntil, &previousUpdated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := h.store.pool.Exec(ctx, `
UPDATE platform.email_delivery_state
SET consecutive_failures = $1, circuit_open_until = $2, updated_at = $3
WHERE singleton`, previousFailures, previousUntil, previousUpdated); err != nil {
			t.Errorf("restore email circuit: %v", err)
		}
	})
	if _, err := h.store.pool.Exec(h.ctx, `
UPDATE platform.email_delivery_state SET consecutive_failures = 0, circuit_open_until = NULL WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	// A future date separates this synthetic quota bucket from live core E2E.
	now := time.Date(contractNow().Year()+80, 1, 1, 12, 0, 0, 0, time.UTC)
	before, err := h.store.EmailDeliveryMetrics(h.ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	request := identity.EmailDeliveryReservation{
		NotificationType: "signup_code", UserID: &h.actorID,
		ReservedAt: now, DailyLimit: int(before.DailyReserved) + 3,
	}
	ticket, err := h.store.ReserveEmailDelivery(h.ctx, request)
	if err != nil {
		t.Fatalf("reserve failed-delivery ticket: %v", err)
	}
	completion := identity.EmailDeliveryCompletion{
		Ticket: ticket, ErrorClass: "provider", CompletedAt: now.Add(time.Second),
		FailureThreshold: 1, CircuitCooldown: 90 * time.Second,
	}
	if err := h.store.CompleteEmailDelivery(h.ctx, completion); err != nil {
		t.Fatalf("record failure and open circuit: %v", err)
	}
	if err := h.store.CompleteEmailDelivery(h.ctx, completion); err != nil {
		t.Fatalf("retry completed failure: %v", err)
	}
	var until time.Time
	var failures int
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT consecutive_failures, circuit_open_until FROM platform.email_delivery_state WHERE singleton`).Scan(&failures, &until); err != nil {
		t.Fatal(err)
	}
	if failures != 1 || !until.Equal(completion.CompletedAt.Add(completion.CircuitCooldown)) {
		t.Fatalf("incorrect circuit deadline/failures: %s/%d", until, failures)
	}
	request.ReservedAt = now.Add(2 * time.Second)
	if _, err := h.store.ReserveEmailDelivery(h.ctx, request); !errors.Is(err, identity.ErrEmailCircuitOpen) {
		t.Fatalf("open circuit reservation = %v, want circuit rejection", err)
	}
	request.ReservedAt = until
	ticket, err = h.store.ReserveEmailDelivery(h.ctx, request)
	if err != nil {
		t.Fatalf("reserve after cooldown: %v", err)
	}
	completion.Ticket, completion.Succeeded = ticket, true
	completion.CompletedAt = until.Add(time.Second)
	if err := h.store.CompleteEmailDelivery(h.ctx, completion); err != nil {
		t.Fatalf("complete successful recovery: %v", err)
	}
	if err := h.store.CompleteEmailDelivery(h.ctx, completion); err != nil {
		t.Fatalf("retry successful completion: %v", err)
	}
	after, err := h.store.EmailDeliveryMetrics(h.ctx, completion.CompletedAt)
	if err != nil {
		t.Fatal(err)
	}
	if after.DailyReserved != before.DailyReserved+2 || after.DailyFailed != before.DailyFailed+1 ||
		after.DailySent != before.DailySent+1 || after.ConsecutiveFailure != 0 || after.CircuitOpen {
		t.Fatalf("completion retry or suppression changed quota/circuit: before=%+v after=%+v", before, after)
	}
	var failed, sent, suppressed int
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT count(*) FILTER (WHERE delivery_status = 'failed' AND error_class = 'provider'),
       count(*) FILTER (WHERE delivery_status = 'sent' AND error_class IS NULL),
       count(*) FILTER (WHERE delivery_status = 'suppressed' AND error_class = 'circuit_open')
FROM audit.notification_deliveries WHERE user_id = $1::uuid`, h.actorID).Scan(&failed, &sent, &suppressed); err != nil {
		t.Fatal(err)
	}
	if failed != 1 || sent != 1 || suppressed != 1 {
		t.Fatalf("unexpected durable delivery outcomes: failed=%d sent=%d suppressed=%d", failed, sent, suppressed)
	}
}

func TestPostgresOperationalDiscoveryRuleContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	var previousJSON []byte
	var previousEnabled bool
	var previousActor *string
	var previousUpdated time.Time
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT value, enabled, updated_by::text, updated_at
FROM platform.site_content_rules WHERE rule_key = 'home.discovery_mix'`).Scan(
		&previousJSON, &previousEnabled, &previousActor, &previousUpdated); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := h.store.pool.Exec(ctx, `
UPDATE platform.site_content_rules SET value = $1::jsonb, enabled = $2, updated_by = $3::uuid, updated_at = $4
WHERE rule_key = 'home.discovery_mix'`, previousJSON, previousEnabled, previousActor, previousUpdated); err != nil {
			t.Errorf("restore discovery rule: %v", err)
		}
	})
	now := contractNow()
	requestID := "discovery-contract-" + h.newHash()[:24]
	input := operations.DiscoveryMixRuleInput{WorkSlots: 6, PerformerSlots: 2, RepeatWindow: 25, Enabled: false, Reason: "Synthetic discovery rule contract"}
	if _, err := h.store.UpdateDiscoveryMixRule(h.ctx, input, h.actorID, requestID, now); err != nil {
		t.Fatalf("update discovery rule: %v", err)
	}
	actual, err := h.store.GetDiscoveryMixRule(h.ctx)
	if err != nil || actual.WorkSlots != input.WorkSlots || actual.PerformerSlots != input.PerformerSlots ||
		actual.RepeatWindow != input.RepeatWindow || actual.Enabled || !actual.UpdatedAt.Equal(now) {
		t.Fatalf("discovery rule did not roundtrip: %+v, %v", actual, err)
	}
	var numeric bool
	var audits, events int
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT jsonb_typeof(value->'work_slots') = 'number'
   AND jsonb_typeof(value->'performer_slots') = 'number'
   AND jsonb_typeof(value->'repeat_window') = 'number'
FROM platform.site_content_rules WHERE rule_key = 'home.discovery_mix'`).Scan(&numeric); err != nil || !numeric {
		t.Fatalf("discovery JSON values are not numbers: %v", err)
	}
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT
 (SELECT count(*) FROM audit.audit_logs WHERE request_id = $1 AND actor_id = $2::uuid
  AND action = 'site_content_rule.update' AND after_version->>'WorkSlots' = '6'),
 (SELECT count(*) FROM platform.outbox_events WHERE aggregate_id = $3::uuid
  AND event_type = 'cache_purge' AND payload->>'reason' = 'discovery_mix_changed'
  AND dedupe_key = $3::text || ':' || $4::text)`,
		requestID, h.actorID, discoveryMixRuleAuditID, now.UTC().Format(time.RFC3339Nano)).Scan(&audits, &events); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || events != 1 {
		t.Fatalf("discovery mutation missing audit/cache event: %d/%d", audits, events)
	}
}

func TestPostgresOperationalEditorialLifecycleContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	label := "editorial-" + h.newHash()[:24]
	reviewerID := h.insertUser(t, h.newEmail("editorial-reviewer"), "admin", "active")
	var workID string
	if err := h.store.pool.QueryRow(h.ctx, `
WITH work AS (
 INSERT INTO platform.works (canonical_code, compact_code, title, publication_status)
 VALUES ($1, $2, 'Synthetic editorial contract', 'published') RETURNING work_id
), revision AS (
 INSERT INTO platform.content_revisions
  (entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id, reason, submitted_at, reviewed_at)
 SELECT 'work', work_id, 1, '{}'::jsonb, 'approved', $3::uuid, $4::uuid,
        'Synthetic editorial contract', now(), now() FROM work
 RETURNING revision_id, entity_id
)
INSERT INTO platform.publications
 (entity_type, entity_id, current_revision_id, publication_status, canonical_slug, published_at)
SELECT 'work', entity_id, revision_id, 'published', $1, now() FROM revision RETURNING entity_id::text`,
		label, strings.ToUpper(strings.ReplaceAll(label, "-", "")), h.actorID, reviewerID).Scan(&workID); err != nil {
		t.Fatalf("publish synthetic editorial work: %v", err)
	}
	now := contractNow()
	input := operations.EditorialRecommendationInput{
		WorkID: workID, Position: 7, StartsAt: now.Add(-time.Minute),
		Reason: "Synthetic editorial recommendation", ChangeReason: "Synthetic repository contract", Status: "active",
	}
	requestID := "editorial-contract-" + h.newHash()[:24]
	created, err := h.store.CreateEditorialRecommendation(h.ctx, input, h.actorID, requestID+"-create", now)
	if err != nil || created.WorkID != workID || created.Status != "active" || created.WorkSlug == nil || *created.WorkSlug != label {
		t.Fatalf("create recommendation: %+v, %v", created, err)
	}
	input.Position, input.Status = 8, "paused"
	updated, err := h.store.UpdateEditorialRecommendation(h.ctx, created.ID, input, h.actorID, requestID+"-update", now.Add(time.Second))
	if err != nil || updated.Position != 8 || updated.Status != "paused" {
		t.Fatalf("update recommendation: %+v, %v", updated, err)
	}
	removed, err := h.store.RemoveEditorialRecommendation(h.ctx, created.ID, h.actorID, "Synthetic recommendation cleanup", requestID+"-remove", now.Add(2*time.Second))
	if err != nil || removed.Status != "removed" {
		t.Fatalf("remove recommendation: %+v, %v", removed, err)
	}
	if _, err := h.store.RemoveEditorialRecommendation(h.ctx, created.ID, h.actorID, "Repeated cleanup", requestID+"-retry", now.Add(3*time.Second)); !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("removed recommendation accepted another mutation: %v", err)
	}
	for _, action := range []string{"created", "updated", "removed"} {
		var events int
		if err := h.store.pool.QueryRow(h.ctx, `
SELECT count(*) FROM platform.outbox_events
WHERE aggregate_type = 'editorial_recommendation' AND aggregate_id = $1::uuid AND event_type = 'cache_purge'
  AND payload->>'reason' = 'editorial_recommendation_changed'
  AND payload->>'work_id' = $2::text AND payload->>'action' = $3::text`, created.ID, workID, action).Scan(&events); err != nil || events != 1 {
			t.Fatalf("editorial %s cache event: count=%d error=%v", action, events, err)
		}
	}
	var audits int
	if err := h.store.pool.QueryRow(h.ctx, `
SELECT count(*) FROM audit.audit_logs WHERE object_id = $1::uuid AND actor_id = $2::uuid
AND action IN ('editorial_recommendation.create', 'editorial_recommendation.update', 'editorial_recommendation.remove')`,
		created.ID, h.actorID).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("editorial lifecycle audit count=%d error=%v", audits, err)
	}
}

func TestPostgresOperationalSystemHealthContract(t *testing.T) {
	h := newInvitationContractHarness(t)
	now := contractNow()
	actual, err := h.store.SystemHealth(h.ctx, now)
	if err != nil || actual.SchemaVersion != 21 || !actual.CheckedAt.Equal(now) || actual.SearchZeroResults24h > actual.SearchRequests24h {
		t.Fatalf("read operational system health: %+v, %v", actual, err)
	}
}
