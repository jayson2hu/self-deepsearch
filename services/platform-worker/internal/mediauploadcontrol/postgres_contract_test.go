package mediauploadcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func validUploadContractURL(raw, role string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "postgres" && u.User != nil && u.User.Username() == role &&
		(u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1") && u.Path == "/self_deepsearch_worker_test" &&
		(u.RawQuery == "" || u.RawQuery == "sslmode=disable") && !u.ForceQuery && !strings.Contains(raw, "#")
}
func TestPostgresMediaUploadQueueContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("MEDIA_UPLOAD_CONTRACT_DATABASE_URL"), os.Getenv("MEDIA_UPLOAD_CONTRACT_ADMIN_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated upload queue PostgreSQL contract is not configured")
	}
	if !validUploadContractURL(runtimeURL, "platform_worker_login") || !validUploadContractURL(adminURL, "platform") || os.Getenv("CONFIRM_MEDIA_UPLOAD_CONTRACT") != "disposable-database" {
		t.Fatal("explicit disposable restricted loopback configuration required")
	}
	a, _ := url.Parse(adminURL)
	r, _ := url.Parse(runtimeURL)
	if a.Host != r.Host {
		t.Fatal("same disposable server required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("cannot open worker pool")
	}
	defer pool.Close()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open owner pool")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire guard")
	}
	defer guard.Release()
	var locked bool
	if err = guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("worker contracts must be serial")
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_, _ = guard.Exec(c, `SELECT pg_advisory_unlock(hashtext('release-a-worker-outbox-contract'))`)
	}()
	var version, existing int
	if err = admin.QueryRow(ctx, `SELECT schema_version,(SELECT count(*) FROM platform.users)+(SELECT count(*) FROM platform.works)+
 (SELECT count(*) FROM platform.media_assets)+(SELECT count(*) FROM platform.outbox_events)+(SELECT count(*) FROM platform.media_upload_control_state)+
 (SELECT count(*) FROM audit.media_upload_commands) FROM platform.system_metadata WHERE singleton`).Scan(&version, &existing); err != nil || version != 21 || existing != 0 {
		t.Fatal("schema 21 and empty disposable inventory required")
	}
	var restricted bool
	if err = pool.QueryRow(ctx, `SELECT current_user='platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls AND pg_has_role(current_user,'platform_worker','member') FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("restricted Worker required")
	}
	clock := func() time.Time {
		var now time.Time
		if e := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal("clock unavailable")
		}
		return now
	}
	var actor string
	if err = admin.QueryRow(ctx, `INSERT INTO platform.users(normalized_email,password_hash,role,account_status,email_verified_at)
 VALUES ('upload-'||gen_random_uuid()::text||'@example.test','synthetic-contract-not-a-login-hash','owner','active',clock_timestamp()) RETURNING user_id::text`).Scan(&actor); err != nil {
		t.Fatal("actor failed", err)
	}
	target := strings.Repeat("a", 64)
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, s := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM audit.media_upload_commands WHERE actor_id=$1::uuid`, actor},
			{`DELETE FROM platform.media_upload_control_state WHERE target_ref=$1`, target},
			{`DELETE FROM platform.users WHERE user_id=$1::uuid`, actor},
		} {
			if _, e := admin.Exec(c, s.sql, s.arg); e != nil {
				t.Error("scoped cleanup failed")
			}
		}
	}()
	repo, _ := NewPostgresQueue(pool, target)
	initial := Receipt{Status: "observed", Mode: "paused"}
	lease, err := repo.Claim(ctx, clock())
	if err != nil || lease.Command != nil {
		t.Fatal("initial claim failed", err)
	}
	if _, e := repo.Claim(ctx, clock()); !errors.Is(e, ErrNoWork) {
		t.Fatal("live lease stolen", e)
	}
	if err = repo.Observe(ctx, lease, initial, clock()); err != nil {
		t.Fatal("initial observation failed", err)
	}
	if err = repo.Finish(ctx, lease, Outcome{}, clock()); err != nil {
		t.Fatal("initial release failed", err)
	}
	// Test-only owner advances the due time; no sleeping or lease timeout hacks.
	due := func() {
		if _, e := admin.Exec(ctx, `UPDATE platform.media_upload_control_state SET next_poll_at=clock_timestamp() WHERE target_ref=$1`, target); e != nil {
			t.Fatal("synthetic poll failed", e)
		}
	}
	queue := func() string {
		tx, e := admin.Begin(ctx)
		if e != nil {
			t.Fatal("queue begin failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, e = tx.Exec(ctx, `SET LOCAL ROLE platform_api`); e != nil {
			t.Fatal("API role selection failed")
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO audit.media_upload_commands(actor_id,idempotency_key,target_ref,expected_epoch,expected_generation,expected_observed_at,mode,reason,request_id,created_at,expires_at)
 SELECT $1::uuid,gen_random_uuid(),target_ref,last_receipt->>'epoch',(last_receipt->>'generation')::bigint,last_success_at,'paused','synthetic operator pause','upload-contract',clock_timestamp(),date_trunc('second',clock_timestamp())+interval '5 minutes'
 FROM platform.media_upload_control_state WHERE singleton RETURNING command_id::text`, actor).Scan(&id)
		if e != nil {
			t.Fatal("API role queue failed", e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal("queue commit failed", e)
		}
		return id
	}
	id := queue()
	due()
	lease, err = repo.Claim(ctx, clock())
	if err != nil || lease.Command == nil || lease.Command.ID != id {
		t.Fatal("queued claim failed", err)
	}
	if err = repo.Observe(ctx, lease, initial, clock()); err != nil {
		t.Fatal("pre-dispatch observation failed", err)
	}
	if ok, e := repo.AuthorizeDispatch(ctx, lease, clock()); e != nil || !ok {
		t.Fatal("restricted dispatch failed", e)
	}
	if err = repo.Finish(ctx, lease, Outcome{Status: "uncertain", Code: "control_apply_unconfirmed", Receipt: &initial}, clock()); err != nil {
		t.Fatal("uncertain evidence failed", err)
	}
	// Stale owner cannot claim completion, even with a syntactically valid ack.
	ack := ackFor(lease.Command.Command)
	ack.AppliedAt = clock().UTC().Format("2006-01-02T15:04:05Z")
	if err = repo.Finish(ctx, lease, Outcome{Status: "applied", Receipt: &ack}, clock()); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("released lease completed request", err)
	}
	due()
	replacement, e := repo.Claim(ctx, clock())
	if e != nil || !replacement.Command.Dispatched {
		t.Fatal("attempt was not preserved", e)
	}
	ack.Status = "observed"
	if err = repo.Observe(ctx, replacement, ack, clock()); err != nil {
		t.Fatal("recovered status failed", err)
	}
	if err = repo.Finish(ctx, replacement, Outcome{Status: "applied", Receipt: &ack}, clock()); err != nil {
		t.Fatal("recovered outcome failed", err)
	}
	var status string
	var count int
	var receipt []byte
	if err = pool.QueryRow(ctx, `SELECT status,dispatch_count,receipt FROM audit.media_upload_commands WHERE command_id=$1::uuid`, id).Scan(&status, &count, &receipt); err != nil || status != "applied" || count != 1 {
		t.Fatal("lost reply caused redispatch or incorrect outcome", err)
	}
	var stored Receipt
	if json.Unmarshal(receipt, &stored) != nil || !replacement.Command.Matches(stored) {
		t.Fatal("final receipt mismatch")
	}
	reject := func(sql string, args ...any) {
		if _, e := admin.Exec(ctx, sql, args...); e == nil {
			t.Fatal("database accepted forbidden transition")
		}
	}
	reject(`UPDATE audit.media_upload_commands SET reason='tampered' WHERE command_id=$1::uuid`, id)
	reject(`UPDATE audit.media_upload_commands SET error_code='tampered' WHERE command_id=$1::uuid`, id)
	reject(`UPDATE platform.media_upload_control_state SET target_ref=$1`, strings.Repeat("b", 64))
	reject(`UPDATE platform.media_upload_control_state SET last_receipt='{}'::jsonb`)
	b, _ := json.Marshal(initial)
	reject(`UPDATE platform.media_upload_control_state SET last_receipt=$1::jsonb`, string(b))
	// Recheck actual role after submission, not just HTTP-time authorization.
	second := queue()
	due()
	lease, err = repo.Claim(ctx, clock())
	if err != nil || lease.Command.ID != second {
		t.Fatal("second claim failed", err)
	}
	if _, err = admin.Exec(ctx, `UPDATE platform.users SET role='user' WHERE user_id=$1::uuid`, actor); err != nil {
		t.Fatal("synthetic revocation failed", err)
	}
	if ok, e := repo.AuthorizeDispatch(ctx, lease, clock()); e != nil || ok {
		t.Fatal("revoked actor dispatched", e)
	}
	if err = repo.Finish(ctx, lease, Outcome{Status: "rejected", Code: "control_actor_or_window_invalid", Receipt: &ack}, clock()); err != nil {
		t.Fatal("revocation outcome failed", err)
	}
	var canInsert, canRewrite, canDelete bool
	if err = pool.QueryRow(ctx, `SELECT has_table_privilege(current_user,'audit.media_upload_commands','INSERT'),has_column_privilege(current_user,'audit.media_upload_commands','reason','UPDATE'),has_table_privilege(current_user,'audit.media_upload_commands','DELETE')`).Scan(&canInsert, &canRewrite, &canDelete); err != nil || canInsert || canRewrite || canDelete {
		t.Fatal("Worker command privileges broadened", err)
	}
}

func TestUploadContractURLGuard(t *testing.T) {
	if !validUploadContractURL("postgres://platform_worker_login:synthetic@127.0.0.1:5432/self_deepsearch_worker_test", "platform_worker_login") {
		t.Fatal("valid disposable URL refused")
	}
	for _, raw := range []string{"postgres://platform_worker_login@public.test/self_deepsearch_worker_test", "postgres://platform@127.0.0.1/self_deepsearch_worker_test", "postgres://platform_worker_login@127.0.0.1/production", "postgres://platform_worker_login@127.0.0.1/self_deepsearch_worker_test?host=public.test"} {
		if validUploadContractURL(raw, "platform_worker_login") {
			t.Fatal("unsafe contract URL accepted")
		}
	}
}
