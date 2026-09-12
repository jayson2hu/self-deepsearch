package mediareconcile

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func validReconcileContractURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "postgres" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" && parsed.User != nil &&
		parsed.Path == "/self_deepsearch_worker_test" && (parsed.RawQuery == "" || parsed.RawQuery == "sslmode=disable") &&
		!parsed.ForceQuery && parsed.Fragment == "" && !strings.Contains(value, "#")
}

func sameReconcileContractDatabase(adminURL, runtimeURL string) bool {
	if !validReconcileContractURL(adminURL) || !validReconcileContractURL(runtimeURL) {
		return false
	}
	admin, _ := url.Parse(adminURL)
	runtime, _ := url.Parse(runtimeURL)
	return admin.Host == runtime.Host
}

func TestReconcileContractGuard(t *testing.T) {
	valid := "postgres://test:password@127.0.0.1:5432/self_deepsearch_worker_test"
	if !validReconcileContractURL(valid) || !validReconcileContractURL(valid+"?sslmode=disable") {
		t.Fatal("safe disposable URL rejected")
	}
	if !sameReconcileContractDatabase(valid, valid+"?sslmode=disable") || sameReconcileContractDatabase(valid, strings.Replace(valid, ":5432/", ":5433/", 1)) {
		t.Fatal("guard and runtime must use the same disposable server")
	}
	for _, value := range []string{strings.Replace(valid, "127.0.0.1", "example.test", 1), strings.Replace(valid, "worker_test", "production", 1), valid + "?host=example.test", valid + "#", valid + "?", "host=127.0.0.1 dbname=self_deepsearch_worker_test"} {
		if validReconcileContractURL(value) {
			t.Fatal("unsafe contract URL accepted")
		}
	}
}

// Run serially after the outbox contracts in the dedicated worker database.
// Both transactions must be observed waiting on the real advisory lock before
// release. A stale REPEATABLE READ snapshot yields two runs; READ COMMITTED
// must yield one run and ErrNotDue, including on an empty inventory.
func TestPostgresReconciliationScheduleContract(t *testing.T) {
	databaseURL := os.Getenv("MEDIA_RECONCILE_CONTRACT_DATABASE_URL")
	adminURL := os.Getenv("MEDIA_RECONCILE_CONTRACT_ADMIN_DATABASE_URL")
	if databaseURL == "" && adminURL == "" {
		t.Skip("dedicated reconciliation PostgreSQL contract database is not configured")
	}
	if os.Getenv("CONFIRM_MEDIA_RECONCILE_CONTRACT") != "disposable-database" || !sameReconcileContractDatabase(adminURL, databaseURL) {
		t.Fatal("explicit disposable loopback contract configuration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("cannot configure restricted test database")
	}
	config.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot open restricted test database")
	}
	defer func() { cancel(); pool.Close() }()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open disposable database guard")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire test guard")
	}
	defer guard.Release()
	var locked bool
	if err := guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("worker contracts must run serially in the isolated database")
	}
	defer func() {
		unlock, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = guard.Exec(unlock, `SELECT pg_advisory_unlock(hashtext('release-a-worker-outbox-contract'))`)
	}()
	var restricted bool
	if err := pool.QueryRow(ctx, `SELECT current_user = 'platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls AND pg_has_role(current_user,'platform_worker','member') FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("restricted worker login required")
	}
	var version, userData int
	if err := admin.QueryRow(ctx, `SELECT schema_version,
 (SELECT count(*) FROM platform.users) + (SELECT count(*) FROM platform.works) + (SELECT count(*) FROM platform.media_assets) + (SELECT count(*) FROM platform.outbox_events)
FROM platform.system_metadata WHERE singleton`).Scan(&version, &userData); err != nil || version != 21 || userData != 0 {
		t.Fatal("schema 21 and empty dedicated worker database required")
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT coalesce(max(started_at),now()) + interval '25 hours' FROM audit.media_reconciliation_runs`).Scan(&now); err != nil {
		t.Fatal("cannot select synthetic schedule time")
	}
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire schedule lock")
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock(hashtext('self-deepsearch-media-reconciliation'))`); err != nil {
		t.Fatal("cannot hold schedule lock")
	}
	defer func() {
		unlock, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = blocker.Exec(unlock, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-reconciliation'))`)
	}()
	pids := make(chan uint32, 2)
	results := make(chan error, 2)
	begin := func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		tx, err := pool.BeginTx(ctx, options)
		if err == nil {
			pids <- tx.Conn().PgConn().PID()
		}
		return tx, err
	}
	for range 2 {
		go func() { _, err := prepareReconciliation(ctx, begin, now, 24*time.Hour); results <- err }()
	}
	var waitPIDs []int32
	for len(waitPIDs) < 2 {
		select {
		case pid := <-pids:
			waitPIDs = append(waitPIDs, int32(pid))
		case <-ctx.Done():
			t.Fatal("scheduler connections did not start before deadline")
		}
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND pid=ANY($1::int[])`, waitPIDs).Scan(&waiting); err != nil {
			t.Fatal("cannot observe scheduler lock waiters")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("both schedulers did not wait on the same real lock")
		}
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-reconciliation'))`); err != nil {
		t.Fatal("cannot release schedule lock")
	}
	winners, notDue := 0, 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				winners++
			} else if errors.Is(err, ErrNotDue) {
				notDue++
			} else {
				t.Fatalf("scheduler returned an unexpected database error: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("schedulers did not finish before deadline")
		}
	}
	if winners != 1 || notDue != 1 {
		t.Fatalf("duplicate inventory scheduling: winners=%d notDue=%d", winners, notDue)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.media_reconciliation_runs WHERE started_at=$1`, now).Scan(&count); err != nil || count != 1 {
		t.Fatal("expected exactly one durable reconciliation run")
	}
	var original string
	if err := pool.QueryRow(ctx, `SELECT run_id::text FROM audit.media_reconciliation_runs WHERE started_at=$1`, now).Scan(&original); err != nil {
		t.Fatal("cannot read admitted run")
	}
	repo, err := NewPostgresRepository(pool)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.Fail(ctx, original, "usage_guard_denied", now.Add(time.Second)); err != nil {
		t.Fatal("cannot record pre-dispatch denial")
	}
	retry, err := repo.Prepare(ctx, now.Add(2*time.Second), 24*time.Hour)
	if err != nil || retry.ID == original {
		t.Fatal("usage denial consumed the scan cadence or reused failed evidence")
	}
	if err = repo.Fail(ctx, retry.ID, "reconcile_unavailable", now.Add(3*time.Second)); err != nil {
		t.Fatal("cannot record ordinary scan failure")
	}
	if _, err = repo.Prepare(ctx, now.Add(4*time.Second), 24*time.Hour); !errors.Is(err, ErrNotDue) {
		t.Fatal("ordinary failure bypassed retry interval")
	}
	var status, code string
	if err = pool.QueryRow(ctx, `SELECT run_status,error_code FROM audit.media_reconciliation_runs WHERE run_id=$1::uuid`, original).Scan(&status, &code); err != nil || status != "failed" || code != "usage_guard_denied" {
		t.Fatal("deferred evidence was overwritten")
	}
}
