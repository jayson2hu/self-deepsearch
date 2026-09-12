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

// Opt-in disposable PostgreSQL only. This exercises the actual API repository;
// unit transaction fakes cannot prove grants, constraints or lock semantics.
func TestPostgresMediaUploadAPIRepositoryContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("PLATFORM_API_TEST_DATABASE_URL"), os.Getenv("CONTRACT_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated API upload-control PostgreSQL contract is not configured")
	}
	a, err := url.Parse(adminURL)
	r, _ := url.Parse(runtimeURL)
	if !validIdentityContractURL(runtimeURL) || err != nil || a.Scheme != "postgres" || a.Host != r.Host || a.Path != r.Path || a.User == nil || a.User.Username() != "platform" ||
		(a.RawQuery != "" && a.RawQuery != "sslmode=disable") || a.ForceQuery || strings.Contains(adminURL, "#") || os.Getenv("CONFIRM_MEDIA_UPLOAD_API_CONTRACT") != "disposable-database" {
		t.Fatal("explicit same-loopback disposable API and owner configuration required")
	}
	h := newInvitationContractHarness(t)
	ctx, store := h.ctx, h.store
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot configure owner pool")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire guard")
	}
	defer guard.Release()
	var locked bool
	if err = guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-media-upload-api-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("upload contracts must be serial")
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = guard.Exec(c, `SELECT pg_advisory_unlock(hashtext('release-a-media-upload-api-contract'))`)
	}()
	var existing int
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform.media_upload_control_state)+(SELECT count(*) FROM audit.media_upload_commands)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatal("empty disposable upload inventory required; existing rows are not changed")
	}
	var now time.Time
	if err = store.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal("clock unavailable")
	}
	in := uploadInput(now)
	in.TargetRef = h.newHash()
	in.IdempotencyKey = h.actorID
	overview, err := store.GetMediaUploadControl(ctx, now)
	if err != nil || overview.State != nil || len(overview.Commands) != 0 {
		t.Fatal("absent state fabricated", err)
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		// Preserve the synthetic actor and its append-only request audit.
		if _, e := admin.Exec(c, `DELETE FROM audit.media_upload_commands WHERE actor_id=$1::uuid`, h.actorID); e != nil {
			t.Error("scoped command cleanup failed")
		}
		if _, e := admin.Exec(c, `DELETE FROM platform.media_upload_control_state WHERE target_ref=$1`, in.TargetRef); e != nil {
			t.Error("scoped state cleanup failed")
		}
	}()
	_, err = admin.Exec(ctx, `INSERT INTO platform.media_upload_control_state(target_ref,last_receipt,last_success_at,updated_at,next_poll_at)
 VALUES($1,'{"status":"observed","epoch":"","generation":0,"mode":"paused","command_id":"","applied_at":""}'::jsonb,$2,$2,$2)`, in.TargetRef, now)
	if err != nil {
		t.Fatal("synthetic state failed", err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal("lock probe failed")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var target string
	if err = tx.QueryRow(ctx, `SELECT target_ref FROM platform.lock_media_upload_control_state()`).Scan(&target); err != nil || target != in.TargetRef {
		t.Fatal("restricted state lock failed", err)
	}
	_, err = admin.Exec(ctx, `SELECT singleton FROM platform.media_upload_control_state WHERE singleton FOR UPDATE NOWAIT`)
	var sqlError *pgconn.PgError
	if !errors.As(err, &sqlError) || sqlError.Code != "55P03" {
		t.Fatal("helper failed to lock row")
	}
	_ = tx.Rollback(ctx)
	var stateUpdate, commandUpdate, publicExec, workerExec bool
	err = store.pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'platform.media_upload_control_state','UPDATE'),
 has_any_column_privilege(current_user,'audit.media_upload_commands','UPDATE'),
 has_function_privilege('public_reader','platform.lock_media_upload_control_state()','EXECUTE'),
 has_function_privilege('platform_worker','platform.lock_media_upload_control_state()','EXECUTE')`).Scan(&stateUpdate, &commandUpdate, &publicExec, &workerExec)
	if err != nil || stateUpdate || commandUpdate || publicExec || workerExec {
		t.Fatal("API/helper privileges broadened", err)
	}
	stale := in
	stale.ExpectedObservedAt = now.Add(-time.Microsecond)
	if _, err = store.SubmitMediaUploadCommand(ctx, stale, h.actorID, "upload-stale", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("stale CAS accepted", err)
	}
	item, err := store.SubmitMediaUploadCommand(ctx, in, h.actorID, "upload-submit", now)
	if err != nil || item.Status != "pending" || item.IdempotentReplay {
		t.Fatal("restricted repository submit failed", err)
	}
	replay, err := store.SubmitMediaUploadCommand(ctx, in, h.actorID, "upload-retry", now.Add(time.Minute))
	if err != nil || replay.ID != item.ID || !replay.IdempotentReplay {
		t.Fatal("idempotency failed", err)
	}
	changed := in
	changed.Reason = "different request"
	if _, err = store.SubmitMediaUploadCommand(ctx, changed, h.actorID, "upload-changed", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
	changed = in
	changed.IdempotencyKey = "90000000-0000-4000-8000-000000000002"
	if _, err = store.SubmitMediaUploadCommand(ctx, changed, h.actorID, "upload-second", now); !errors.Is(err, operations.ErrConflict) {
		t.Fatal("second open request accepted", err)
	}
	var audits int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_logs WHERE actor_id=$1::uuid AND action='media_upload.command_requested'`, h.actorID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("audit missing or duplicated")
	}
	overview, err = store.GetMediaUploadControl(ctx, now)
	if err != nil || overview.State == nil || !overview.State.Fresh || len(overview.Commands) != 1 || overview.Commands[0].ID != item.ID || overview.Commands[0].FirstDispatchedAt != nil {
		t.Fatal("queue incorrectly changed executor state", err)
	}
}
