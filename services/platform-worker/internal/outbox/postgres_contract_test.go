package outbox

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func validOutboxContractURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "postgres" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" &&
		parsed.Path == "/self_deepsearch_worker_test" && parsed.User != nil &&
		(parsed.RawQuery == "" || parsed.RawQuery == "sslmode=disable") && !parsed.ForceQuery &&
		parsed.Fragment == "" && !strings.Contains(value, "#")
}

func sameOutboxContractDatabase(adminURL, runtimeURL string) bool {
	if !validOutboxContractURL(adminURL) || !validOutboxContractURL(runtimeURL) {
		return false
	}
	admin, _ := url.Parse(adminURL)
	runtime, _ := url.Parse(runtimeURL)
	return admin.Host == runtime.Host
}

func TestOutboxContractDatabaseGuard(t *testing.T) {
	valid := "postgres://test:password@127.0.0.1:5432/self_deepsearch_worker_test"
	if !validOutboxContractURL(valid) {
		t.Fatal("isolated loopback database rejected")
	}
	if !sameOutboxContractDatabase(valid, valid+"?sslmode=disable") || sameOutboxContractDatabase(valid, strings.Replace(valid, ":5432/", ":5433/", 1)) {
		t.Fatal("admin and runtime must address the same isolated database server")
	}
	for _, value := range []string{
		strings.Replace(valid, "127.0.0.1", "example.test", 1),
		strings.Replace(valid, "self_deepsearch_worker_test", "production", 1),
		valid + "?host=example.test", valid + "#", valid + "?", "host=127.0.0.1 dbname=self_deepsearch_worker_test",
	} {
		if validOutboxContractURL(value) {
			t.Fatal("unsafe database configuration accepted")
		}
	}
}

// This is deliberately a dedicated disposable database, not the API fixture DB:
// Claim selects eligible work globally and must never consume a user's queue.
func TestPostgresOutboxLeaseContract(t *testing.T) {
	adminURL := os.Getenv("WORKER_CONTRACT_ADMIN_DATABASE_URL")
	runtimeURL := os.Getenv("WORKER_CONTRACT_DATABASE_URL")
	if adminURL == "" && runtimeURL == "" {
		t.Skip("dedicated worker PostgreSQL contract database is not configured")
	}
	if os.Getenv("CONFIRM_WORKER_OUTBOX_CONTRACT") != "disposable-database" || !sameOutboxContractDatabase(adminURL, runtimeURL) {
		t.Fatal("worker contract requires two loopback disposable database URLs and explicit confirmation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot configure admin database connection")
	}
	defer admin.Close()
	repository, err := OpenPostgres(ctx, runtimeURL)
	if err != nil {
		t.Fatal("cannot configure restricted worker connection")
	}
	defer repository.Close()
	lockConnection, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire contract lock connection")
	}
	defer lockConnection.Release()
	var locked bool
	if err := lockConnection.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("another worker contract is using this database")
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		_, _ = lockConnection.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtext('release-a-worker-outbox-contract'))`)
	}()
	var version, initialEvents int
	if err := admin.QueryRow(ctx, `SELECT schema_version, (SELECT count(*) FROM platform.outbox_events) + (SELECT count(*) FROM platform.users) + (SELECT count(*) FROM platform.works) + (SELECT count(*) FROM platform.media_assets) FROM platform.system_metadata WHERE singleton`).Scan(&version, &initialEvents); err != nil || version != 21 || initialEvents != 0 {
		t.Fatal("contract requires migrated schema 21 and an empty dedicated database")
	}
	var restricted bool
	if err := repository.pool.QueryRow(ctx, `SELECT current_user = 'platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls AND pg_has_role(current_user, 'platform_worker', 'member') FROM pg_roles WHERE rolname = current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("contract must run repository calls using the restricted worker login")
	}
	insert := func(t *testing.T, kind string) string {
		t.Helper()
		var id string
		err := admin.QueryRow(ctx, `INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, dedupe_key) VALUES ('work', gen_random_uuid(), $1, gen_random_uuid()::text) RETURNING event_id::text`, kind).Scan(&id)
		if err != nil {
			t.Fatal("cannot insert synthetic outbox event")
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			if _, err := admin.Exec(cleanupCtx, `DELETE FROM platform.outbox_events WHERE event_id = $1::uuid`, id); err != nil {
				t.Error("cannot clean up synthetic outbox event")
			}
		})
		return id
	}
	claim := func(t *testing.T, worker string) Event {
		t.Helper()
		events, err := repository.Claim(ctx, worker, time.Now().UTC(), time.Minute, 1)
		if err != nil || len(events) != 1 || events[0].LeaseExpiresAt.IsZero() {
			t.Fatalf("expected one claimed event with a database deadline, count=%d error=%v", len(events), err)
		}
		return events[0]
	}
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := admin.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture statement failed: %v", err)
		}
	}
	state := func(t *testing.T, id string) (string, int) {
		t.Helper()
		var status string
		var attempt int
		if err := admin.QueryRow(ctx, `SELECT status, attempt_count FROM platform.outbox_events WHERE event_id = $1::uuid`, id).Scan(&status, &attempt); err != nil {
			t.Fatal("cannot read fixture outcome")
		}
		return status, attempt
	}

	t.Run("same_worker_reclaim_fences_old_success_and_failure", func(t *testing.T) {
		id := insert(t, "publication_changed")
		first := claim(t, "same-worker")
		exec(t, `UPDATE platform.outbox_events SET lease_expires_at = now() - interval '1 second' WHERE event_id = $1::uuid`, id)
		if !errors.Is(repository.Complete(ctx, id, "same-worker", first.AttemptCount, time.Now()), ErrLeaseLost) ||
			!errors.Is(repository.Fail(ctx, id, "same-worker", first.AttemptCount, "old_error", time.Now()), ErrLeaseLost) {
			t.Fatal("expired lease was allowed to settle before reclaim")
		}
		second := claim(t, "same-worker")
		if second.AttemptCount != first.AttemptCount+1 {
			t.Fatal("reclaim did not advance the fencing value")
		}
		if !errors.Is(repository.Complete(ctx, id, "same-worker", first.AttemptCount, time.Now()), ErrLeaseLost) ||
			!errors.Is(repository.Fail(ctx, id, "same-worker", first.AttemptCount, "stale_error", time.Now()), ErrLeaseLost) {
			t.Fatal("old attempt overwrote the new lease")
		}
		if status, attempt := state(t, id); status != "running" || attempt != second.AttemptCount {
			t.Fatal("stale settlement changed the current attempt")
		}
		if err := repository.Complete(ctx, id, "same-worker", second.AttemptCount, time.Now()); err != nil {
			t.Fatal(err)
		}
		if status, _ := state(t, id); status != "completed" {
			t.Fatal("current lease did not complete")
		}
		if !errors.Is(repository.Complete(ctx, id, "same-worker", second.AttemptCount, time.Now()), ErrLeaseLost) {
			t.Fatal("completed lease was settled twice")
		}
	})

	t.Run("backoff_and_terminal_failure", func(t *testing.T) {
		id := insert(t, "publication_changed")
		first := claim(t, "worker")
		if err := repository.Fail(ctx, id, "worker", first.AttemptCount, "revalidate_unavailable", time.Now()); err != nil {
			t.Fatal(err)
		}
		if status, _ := state(t, id); status != "retry" {
			t.Fatal("retry was not persisted")
		}
		if events, err := repository.Claim(ctx, "worker", time.Now(), time.Minute, 1); err != nil || len(events) != 0 {
			t.Fatal("backoff did not defer another claim")
		}
		exec(t, `UPDATE platform.outbox_events SET available_at = now() - interval '1 second', attempt_count = 9 WHERE event_id = $1::uuid`, id)
		last := claim(t, "worker")
		if last.AttemptCount != 10 || repository.Fail(ctx, id, "worker", last.AttemptCount, "unavailable", time.Now()) != nil {
			t.Fatal("tenth failure could not be recorded")
		}
		if status, _ := state(t, id); status != "dead" {
			t.Fatal("tenth failure did not become dead")
		}
	})

	t.Run("crash_exhaustion_cannot_poison_fresh_claims", func(t *testing.T) {
		exhausted := insert(t, "cache_purge")
		fresh := insert(t, "cache_purge")
		exec(t, `UPDATE platform.outbox_events SET status = 'running', lease_owner = 'crashed', lease_expires_at = now() - interval '1 second', attempt_count = 20 WHERE event_id = $1::uuid`, exhausted)
		events, err := repository.Claim(ctx, "worker", time.Now(), time.Minute, 20)
		if err != nil || len(events) != 1 || events[0].ID != fresh {
			t.Fatalf("exhausted event blocked healthy work: count=%d error=%v", len(events), err)
		}
		if status, attempt := state(t, exhausted); status != "dead" || attempt != 20 {
			t.Fatal("exhausted crash must stop without incrementing beyond the database constraint")
		}
	})

	t.Run("concurrent_claims_do_not_share_an_event", func(t *testing.T) {
		insert(t, "publication_changed")
		type result struct {
			events []Event
			err    error
		}
		results := make(chan result, 2)
		start := make(chan struct{})
		for _, worker := range []string{"worker-a", "worker-b"} {
			go func(worker string) {
				<-start
				events, err := repository.Claim(ctx, worker, time.Now(), time.Minute, 1)
				results <- result{events, err}
			}(worker)
		}
		close(start)
		count := 0
		for range 2 {
			result := <-results
			if result.err != nil {
				t.Fatal(result.err)
			}
			count += len(result.events)
		}
		if count != 1 {
			t.Fatalf("concurrent claims returned %d copies of one event", count)
		}
	})

	t.Run("media_finalization_locks_and_rechecks_the_lease", func(t *testing.T) {
		var assetID, objectID string
		if err := admin.QueryRow(ctx, `INSERT INTO platform.media_assets (asset_type, source_type, checked_at, confidence, rights_status) VALUES ('work_image', 'fixture', now(), 1, 'allowed') RETURNING asset_id::text`).Scan(&assetID); err != nil {
			t.Fatal("cannot insert synthetic media asset")
		}
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, err := admin.Exec(cleanupCtx, `DELETE FROM platform.media_assets WHERE asset_id = $1::uuid`, assetID); err != nil {
				t.Error("cannot clean up synthetic asset")
			}
		})
		key := "media-master/" + assetID + "/contract.webp"
		if err := admin.QueryRow(ctx, `INSERT INTO platform.media_objects (asset_id, version, rendition, storage_provider, storage_scope, storage_key, sha256, mime_type, width, height, byte_size, object_status) VALUES ($1::uuid, 1, 'master', 's3', 'private', $2, repeat('a',64), 'image/webp', 1, 1, 1, 'hidden') RETURNING media_object_id::text`, assetID, key).Scan(&objectID); err != nil {
			t.Fatalf("cannot insert synthetic media object: %v", err)
		}
		id := insert(t, "media_delete")
		exec(t, `UPDATE platform.outbox_events SET payload = jsonb_build_object('storage_scope', 'private', 'storage_key', $2::text) WHERE event_id = $1::uuid`, id, key)
		first := claim(t, "same-worker")
		transaction, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer transaction.Rollback(context.Background())
		if _, err := transaction.Exec(ctx, `UPDATE platform.outbox_events SET attempt_count = attempt_count + 1 WHERE event_id = $1::uuid`, id); err != nil {
			t.Fatal(err)
		}
		result := make(chan error, 1)
		go func() { result <- repository.Complete(ctx, id, "same-worker", first.AttemptCount, time.Now()) }()
		deadline := time.Now().Add(5 * time.Second)
		blocked := false
		for time.Now().Before(deadline) {
			if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND usename = 'platform_worker_login' AND wait_event_type = 'Lock' AND query LIKE '%UPDATE platform.outbox_events event%')`).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("completion did not reach the concurrent row lock")
		}
		if err := transaction.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, ErrLeaseLost) {
			t.Fatalf("stale concurrent completion result: %v", err)
		}
		var status string
		if err := admin.QueryRow(ctx, `SELECT object_status FROM platform.media_objects WHERE media_object_id = $1::uuid`, objectID).Scan(&status); err != nil || status != "hidden" {
			t.Fatal("stale completion modified the media object before validating its lease")
		}
		if err := repository.Complete(ctx, id, "same-worker", first.AttemptCount+1, time.Now()); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRow(ctx, `SELECT object_status FROM platform.media_objects WHERE media_object_id = $1::uuid`, objectID).Scan(&status); err != nil || status != "deleted" {
			t.Fatal("valid completion did not finalize media")
		}
		if status, _ := state(t, id); status != "completed" {
			t.Fatal("media and outbox did not complete together")
		}
	})
}
