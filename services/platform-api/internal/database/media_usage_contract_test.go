package database

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"self-deepsearch/services/platform-api/internal/operations"
)

// Exercise the real repository, not a hand-written API-role INSERT. In
// particular SELECT FOR UPDATE must work without giving the API state UPDATE.
func TestPostgresMediaUsageAPIRepositoryContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("PLATFORM_API_TEST_DATABASE_URL"), os.Getenv("CONTRACT_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated API media usage PostgreSQL contract is not configured")
	}
	a, err := url.Parse(adminURL)
	r, _ := url.Parse(runtimeURL)
	if !validIdentityContractURL(runtimeURL) || err != nil || a.Scheme != "postgres" || a.Host != r.Host ||
		a.Path != r.Path || a.User == nil || a.User.Username() != "platform" ||
		(a.RawQuery != "" && a.RawQuery != "sslmode=disable") || a.ForceQuery || strings.Contains(adminURL, "#") ||
		os.Getenv("CONFIRM_MEDIA_USAGE_API_CONTRACT") != "disposable-database" {
		t.Fatal("explicit same-loopback disposable API and owner configuration required")
	}
	h := newInvitationContractHarness(t)
	ctx, store := h.ctx, h.store
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot configure disposable owner pool")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire serial guard")
	}
	defer guard.Release()
	var locked bool
	if err = guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-media-usage-api-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("API media usage contracts must be serial")
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = guard.Exec(c, `SELECT pg_advisory_unlock(hashtext('release-a-media-usage-api-contract'))`)
	}()
	var existing int
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform.media_usage_state)+(SELECT count(*) FROM audit.media_usage_runs)+(SELECT count(*) FROM audit.media_usage_reviews)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("empty disposable usage inventory required; existing rows will not be changed")
	}
	var now time.Time
	if err = store.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal("clock unavailable")
	}
	input := usageInput(now)
	input.ExpectedDigest = h.newHash()
	input.IdempotencyKey = h.actorID
	overview, err := store.GetMediaUsage(ctx, now)
	if err != nil || overview.State != nil || len(overview.Reviews) != 0 || overview.Enforcement != "not_connected" {
		t.Fatal("uninitialized overview is not explicit")
	}
	if _, err = store.SubmitMediaUsageReview(ctx, input, h.actorID, "usage-api-absent", now); !errors.Is(err, operations.ErrNotFound) {
		t.Fatal("missing state did not fail closed", err)
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		// Only the scoped disposable usage fixtures are removed. Keep the user
		// and append-only request audit, as in the other API contracts.
		if _, e := admin.Exec(c, `DELETE FROM platform.media_usage_state WHERE config_digest=$1`, input.ExpectedDigest); e != nil {
			t.Error("scoped state cleanup failed")
		}
		if _, e := admin.Exec(c, `DELETE FROM audit.media_usage_reviews WHERE actor_id=$1::uuid`, h.actorID); e != nil {
			t.Error("scoped review cleanup failed")
		}
	}()
	_, err = admin.Exec(ctx, `INSERT INTO platform.media_usage_state(config_digest,account_ref,policy_snapshot,period_start,period_end,
 high_class_a,high_class_b,review_required,review_since,review_reason,updated_at,next_poll_at)
 VALUES($1,$1,'{"class_a_limit":1000000,"class_b_limit":10000000}'::jsonb,$2,$3,123,456,true,$4,'usage_no_data',$4,$4)`,
		input.ExpectedDigest, now.Add(-time.Hour), now.Add(time.Hour), now)
	if err != nil {
		t.Fatal("synthetic usage state failed", err)
	}
	// Verify the helper really holds a row lock, and is not an unlocked SELECT.
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal("lock probe begin failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var digest string
	if err = tx.QueryRow(ctx, `SELECT config_digest FROM platform.lock_media_usage_review_state()`).Scan(&digest); err != nil || digest != input.ExpectedDigest {
		t.Fatal("restricted API cannot acquire state lock", err)
	}
	_, err = admin.Exec(ctx, `SELECT singleton FROM platform.media_usage_state WHERE singleton FOR UPDATE NOWAIT`)
	var sqlError *pgconn.PgError
	if !errors.As(err, &sqlError) || sqlError.Code != "55P03" {
		t.Fatal("helper did not lock the state row")
	}
	_ = tx.Rollback(ctx)
	var canUpdate, publicExec, workerExec bool
	err = store.pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'platform.media_usage_state','UPDATE'),
 has_function_privilege('public_reader','platform.lock_media_usage_review_state()','EXECUTE'),
 has_function_privilege('platform_worker','platform.lock_media_usage_review_state()','EXECUTE')`).Scan(&canUpdate, &publicExec, &workerExec)
	if err != nil || canUpdate || publicExec || workerExec {
		t.Fatal("state helper broadened privileges")
	}
	stale := input
	stale.ExpectedUpdatedAt = now.Add(-time.Second)
	if _, err = store.SubmitMediaUsageReview(ctx, stale, h.actorID, "usage-api-stale", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("stale repository CAS accepted", err)
	}
	item, err := store.SubmitMediaUsageReview(ctx, input, h.actorID, "usage-api-submit", now)
	if err != nil || item.Status != "pending" || item.IdempotentReplay {
		t.Fatal("real restricted repository submission failed", err)
	}
	replay, err := store.SubmitMediaUsageReview(ctx, input, h.actorID, "usage-api-retry", now)
	if err != nil || replay.ID != item.ID || !replay.IdempotentReplay {
		t.Fatal("real repository idempotency failed", err)
	}
	changed := input
	changed.Reason = "different reason"
	if _, err = store.SubmitMediaUsageReview(ctx, changed, h.actorID, "usage-api-changed", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("changed replay accepted", err)
	}
	changed = input
	changed.IdempotencyKey = "90000000-0000-4000-8000-000000000002"
	if _, err = store.SubmitMediaUsageReview(ctx, changed, h.actorID, "usage-api-pending", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("second pending request accepted", err)
	}
	var audits int
	err = store.pool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_logs WHERE actor_id=$1::uuid AND action='media_usage.review_requested'`, h.actorID).Scan(&audits)
	if err != nil || audits != 1 {
		t.Fatal("request audit is missing or duplicated")
	}
	overview, err = store.GetMediaUsage(ctx, now)
	if err != nil || overview.State == nil || overview.State.ClassAHighwater != "123" || !overview.State.ReviewRequired ||
		len(overview.Reviews) != 1 || overview.Reviews[0].ID != item.ID {
		t.Fatal("submission changed observation state or lost receipt", err)
	}
}
