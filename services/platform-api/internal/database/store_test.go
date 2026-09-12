package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/operations"
)

func TestIntervalLiteral(t *testing.T) {
	if got := intervalLiteral(7 * 24 * time.Hour); got != "168 hours" {
		t.Fatalf("intervalLiteral() = %q", got)
	}
	if got := intervalLiteral(0); got != "1 hours" {
		t.Fatalf("intervalLiteral() minimum = %q", got)
	}
}

func TestImageFromJSONPreservesAvailableRenditions(t *testing.T) {
	image, err := imageFromJSON([]byte(`[
		{"url":"https://media.example.test/cover-w320.webp","rendition":"w320","width":320,"height":200,"mime_type":"image/webp"},
		{"url":"https://media.example.test/cover-w640.webp","rendition":"w640","width":640,"height":400,"mime_type":"image/webp"},
		{"url":"https://media.example.test/cover-w960.webp","rendition":"w960","width":960,"height":600,"mime_type":"image/webp"}
	]`))
	if err != nil {
		t.Fatalf("imageFromJSON() error = %v", err)
	}
	if image == nil || image.URL != "https://media.example.test/cover-w640.webp" || image.Rendition != "w640" {
		t.Fatalf("unexpected preferred compatibility rendition: %#v", image)
	}
	if len(image.Renditions) != 3 || image.Renditions[0].Width != 320 || image.Renditions[2].Width != 960 {
		t.Fatalf("expected all API-addressable renditions, got %#v", image.Renditions)
	}
	performerURL := imageURLForRendition(image, "w320")
	if performerURL == nil || *performerURL != "https://media.example.test/cover-w320.webp" {
		t.Fatalf("expected legacy performer URL to keep the w320 preference, got %v", performerURL)
	}
}

func TestImageFromRenditionsSupportsSingleLegacyDerivative(t *testing.T) {
	image := imageFromRenditions([]catalog.ImageRendition{{
		URL: "https://media.example.test/legacy.webp", Rendition: "w960",
		Width: 960, Height: 600, MimeType: "image/webp",
	}})
	if image == nil || image.URL != "https://media.example.test/legacy.webp" || len(image.Renditions) != 1 {
		t.Fatalf("single derivative should remain displayable: %#v", image)
	}
	if empty, err := imageFromJSON(nil); err != nil || empty != nil {
		t.Fatalf("missing media should decode to nil, got image=%#v err=%v", empty, err)
	}
}

func TestReassignReviewTaskRoundTrip(t *testing.T) {
	harness := newInvitationContractHarness(t)
	store, ctx := harness.store, harness.ctx

	const taskID = "40000000-0000-4000-8000-000000000001"
	actorEmail := harness.newEmail("reassign-actor")
	assigneeEmail := harness.newEmail("reassign-target")
	actorID := harness.insertUser(t, actorEmail, "admin", "active")
	nextAssigneeID := harness.insertUser(t, assigneeEmail, "owner", "active")
	requestID := "review-reassign-" + harness.newHash()[:24]
	now := time.Date(2026, 9, 7, 9, 8, 7, 0, time.UTC)
	command, err := store.pool.Exec(ctx, `
UPDATE collector.review_tasks
SET status = 'claimed', assignee_id = $2::uuid, claimed_at = $3, completed_at = NULL
WHERE review_task_id = $1::uuid`, taskID, actorID, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("prepare claimed review task: %v", err)
	}
	if command.RowsAffected() != 1 {
		t.Fatal("review reassignment contract requires the development fixture seed")
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `
UPDATE collector.review_tasks
SET status = 'pending', assignee_id = NULL, claimed_at = NULL, completed_at = NULL
WHERE review_task_id = $1::uuid`, taskID)
	})

	result, err := store.ReassignReviewTask(ctx, taskID, nextAssigneeID, actorID, "reviewer handoff", requestID, now)
	if err != nil {
		t.Fatalf("reassign review task: %v", err)
	}
	if result.AssigneeID == nil || *result.AssigneeID != nextAssigneeID || result.Assignee == nil || *result.Assignee != assigneeEmail || result.ClaimedAt == nil || !result.ClaimedAt.Equal(now) {
		t.Fatalf("unexpected reassigned task: %#v", result)
	}

	var beforeID, afterID, reason, auditActor string
	if err := store.pool.QueryRow(ctx, `
SELECT before_version->>'assignee_id', after_version->>'assignee_id', reason, actor_id::text
FROM audit.audit_logs
WHERE action = 'review_task.reassign' AND object_id = $1::uuid AND request_id = $2
ORDER BY audit_id DESC LIMIT 1`, taskID, requestID).Scan(&beforeID, &afterID, &reason, &auditActor); err != nil {
		t.Fatalf("read reassignment audit: %v", err)
	}
	if beforeID != actorID || afterID != nextAssigneeID || reason != "reviewer handoff" || auditActor != actorID {
		t.Fatalf("unexpected reassignment audit before=%q after=%q reason=%q actor=%q", beforeID, afterID, reason, auditActor)
	}
	timeline, err := store.ListAuditLogs(ctx, requestID, 201)
	if err != nil {
		t.Fatalf("list audit timeline: %v", err)
	}
	if len(timeline) != 1 || timeline[0].RequestID != requestID || timeline[0].Action != "review_task.reassign" ||
		timeline[0].Actor == nil || *timeline[0].Actor != actorEmail {
		t.Fatalf("unexpected minimal audit timeline: %#v", timeline)
	}
	if _, err := store.ReassignReviewTask(ctx, taskID, nextAssigneeID, actorID, "repeat handoff", requestID+"-repeat", now.Add(time.Second)); !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("same-assignee reassignment error = %v, want conflict", err)
	}
}

func TestMergeWorkRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("PLATFORM_API_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PLATFORM_API_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer store.Close()

	const sourceID = "10000000-0000-4000-8000-000000000001"
	const targetID = "10000000-0000-4000-8000-000000000002"
	const actorID = "90000000-0000-4000-8000-000000000001"
	now := time.Date(2026, 8, 20, 12, 34, 56, 0, time.UTC)
	hour := now.Truncate(time.Hour)

	fixtures := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO platform.tags (tag_id, display_name, slug) VALUES
           ('50000000-0000-4000-8000-000000000099', '合并测试', 'merge-test')
           ON CONFLICT (tag_id) DO NOTHING`, nil},
		{`INSERT INTO platform.work_tags (work_id, tag_id) VALUES
           ($1::uuid, '50000000-0000-4000-8000-000000000099') ON CONFLICT DO NOTHING`, []any{sourceID}},
		{`INSERT INTO platform.work_aliases (work_alias_id, work_id, alias_type, raw_value, normalized_value) VALUES
           ('51000000-0000-4000-8000-000000000099', $1::uuid, 'code', 'MERGE-ALIAS', 'MERGEALIAS')
           ON CONFLICT (work_alias_id) DO NOTHING`, []any{sourceID}},
		{`INSERT INTO platform.favorites (user_id, work_id, is_deleted, deleted_at) VALUES
           ($1::uuid, $2::uuid, false, NULL), ($1::uuid, $3::uuid, true, $4)
           ON CONFLICT (user_id, work_id) DO UPDATE SET is_deleted = EXCLUDED.is_deleted, deleted_at = EXCLUDED.deleted_at`, []any{actorID, sourceID, targetID, now}},
		{`INSERT INTO platform.hidden_preferences (user_id, work_id, status) VALUES
           ($1::uuid, $2::uuid, 'active'), ($1::uuid, $3::uuid, 'removed')
           ON CONFLICT (user_id, work_id) DO UPDATE SET status = EXCLUDED.status`, []any{actorID, sourceID, targetID}},
		{`INSERT INTO platform.content_metrics_hourly (content_type, content_id, hour_bucket, page_views) VALUES
           ('work', $1::uuid, $3, 3), ('work', $2::uuid, $3, 5)
           ON CONFLICT (content_type, content_id, hour_bucket) DO UPDATE SET page_views = EXCLUDED.page_views`, []any{sourceID, targetID, hour}},
		{`INSERT INTO platform.editorial_recommendations
           (recommendation_id, work_id, position, starts_at, reason, status, created_by) VALUES
           ('52000000-0000-4000-8000-000000000099', $1::uuid, 99, $2, 'merge roundtrip', 'active', $3::uuid)
           ON CONFLICT (recommendation_id) DO UPDATE SET status = 'active'`, []any{sourceID, now, actorID}},
	}
	for _, fixture := range fixtures {
		if _, err := store.pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatalf("prepare merge fixtures: %v", err)
		}
	}

	result, err := store.MergeEntity(ctx, sourceID, operations.MergeEntityInput{
		EntityType: "work", TargetEntityID: targetID, Reason: "duplicate fixture record",
	}, actorID, "merge-roundtrip", now)
	if err != nil {
		t.Fatalf("merge work: %v", err)
	}
	if result.SourcePath != "/works/test-001" || result.TargetPath != "/works/test-002" {
		t.Fatalf("unexpected redirect paths: %#v", result)
	}

	assertText := func(query, want string, args ...any) {
		t.Helper()
		var got string
		if err := store.pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
			t.Fatalf("query merge result: %v", err)
		}
		if got != want {
			t.Fatalf("merge result = %q, want %q", got, want)
		}
	}
	assertText(`SELECT publication_status FROM platform.works WHERE work_id = $1::uuid`, "merged", sourceID)
	assertText(`SELECT publication_status FROM platform.publications WHERE entity_type = 'work' AND entity_id = $1::uuid`, "hidden", sourceID)
	assertText(`SELECT target_path FROM platform.redirects WHERE source_path = '/works/test-001' AND status_code = 301 AND active`, "/works/test-002")
	assertText(`SELECT status FROM platform.hidden_preferences WHERE user_id = $1::uuid AND work_id = $2::uuid`, "active", actorID, targetID)
	assertText(`SELECT status FROM platform.editorial_recommendations WHERE recommendation_id = '52000000-0000-4000-8000-000000000099'`, "removed")
	assertText(`SELECT page_views::text FROM platform.content_metrics_hourly WHERE content_type = 'work' AND content_id = $1::uuid AND hour_bucket = $2`, "8", targetID, hour)
	assertText(`SELECT count(*)::text FROM platform.content_metrics_hourly WHERE content_type = 'work' AND content_id = $1::uuid`, "0", sourceID)
	assertText(`SELECT count(*)::text FROM platform.public_published_works WHERE work_id = $1::uuid`, "0", sourceID)
	assertText(`SELECT count(*)::text FROM platform.entity_merges WHERE entity_type = 'work' AND source_entity_id = $1::uuid AND target_entity_id = $2::uuid`, "1", sourceID, targetID)
	assertText(`SELECT count(*)::text FROM audit.audit_logs WHERE action = 'entity.merge' AND object_id = $1::uuid`, "1", sourceID)
	assertText(`SELECT count(*)::text FROM platform.outbox_events WHERE event_type = 'cache_purge' AND aggregate_id = $1::uuid`, "1", sourceID)
	assertText(`SELECT is_deleted::text FROM platform.favorites WHERE user_id = $1::uuid AND work_id = $2::uuid`, "false", actorID, targetID)

	_, err = store.MergeEntity(ctx, sourceID, operations.MergeEntityInput{
		EntityType: "work", TargetEntityID: targetID, Reason: "repeat merge",
	}, actorID, "merge-roundtrip-repeat", now.Add(time.Second))
	if !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("repeat merge error = %v, want conflict", err)
	}
}
