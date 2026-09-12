package mediainspect

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

func validInspectionContractURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "postgres" && parsed.Hostname() == "127.0.0.1" && parsed.Port() != "" && parsed.User != nil &&
		parsed.Path == "/self_deepsearch_worker_test" && (parsed.RawQuery == "" || parsed.RawQuery == "sslmode=disable") &&
		!parsed.ForceQuery && parsed.Fragment == "" && !strings.Contains(value, "#")
}

func sameInspectionContractDatabase(adminURL, runtimeURL string) bool {
	if !validInspectionContractURL(adminURL) || !validInspectionContractURL(runtimeURL) {
		return false
	}
	admin, _ := url.Parse(adminURL)
	runtime, _ := url.Parse(runtimeURL)
	return admin.Host == runtime.Host
}

func TestInspectionContractGuard(t *testing.T) {
	valid := "postgres://test:password@127.0.0.1:5432/self_deepsearch_worker_test"
	if !validInspectionContractURL(valid) || !validInspectionContractURL(valid+"?sslmode=disable") {
		t.Fatal("safe disposable URL rejected")
	}
	if !sameInspectionContractDatabase(valid, valid+"?sslmode=disable") || sameInspectionContractDatabase(valid, strings.Replace(valid, ":5432/", ":5433/", 1)) {
		t.Fatal("guard and runtime must use the same disposable server")
	}
	for _, value := range []string{strings.Replace(valid, "127.0.0.1", "example.test", 1), strings.Replace(valid, "worker_test", "production", 1), valid + "?host=example.test", valid + "#", valid + "?", "host=127.0.0.1 dbname=self_deepsearch_worker_test"} {
		if validInspectionContractURL(value) {
			t.Fatal("unsafe contract URL accepted")
		}
	}
}

// Run serially after the outbox contracts in the dedicated worker database.
// Both transactions must be observed waiting on the real advisory lock before
// release. A stale REPEATABLE READ snapshot yields two runs; READ COMMITTED
// must yield one run and ErrNotDue, including on an empty inventory.
func TestPostgresInspectionScheduleContract(t *testing.T) {
	databaseURL := os.Getenv("MEDIA_INSPECT_CONTRACT_DATABASE_URL")
	adminURL := os.Getenv("MEDIA_INSPECT_CONTRACT_ADMIN_DATABASE_URL")
	if databaseURL == "" && adminURL == "" {
		t.Skip("dedicated inspection PostgreSQL contract database is not configured")
	}
	if os.Getenv("CONFIRM_MEDIA_INSPECT_CONTRACT") != "disposable-database" || !sameInspectionContractDatabase(adminURL, databaseURL) {
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
	if err := pool.QueryRow(ctx, `SELECT coalesce(max(started_at),now()) + interval '25 hours' FROM audit.media_inspection_runs`).Scan(&now); err != nil {
		t.Fatal("cannot select synthetic schedule time")
	}
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire schedule lock")
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock(hashtext('self-deepsearch-media-inspection'))`); err != nil {
		t.Fatal("cannot hold schedule lock")
	}
	defer func() {
		unlock, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = blocker.Exec(unlock, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-inspection'))`)
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
		go func() { _, err := prepareInspection(ctx, begin, now, "https://display.example.test"); results <- err }()
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
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-inspection'))`); err != nil {
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
				t.Fatal("scheduler returned an unexpected database error")
			}
		case <-ctx.Done():
			t.Fatal("schedulers did not finish before deadline")
		}
	}
	if winners != 1 || notDue != 1 {
		t.Fatalf("duplicate inspection scheduling: winners=%d notDue=%d", winners, notDue)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.media_inspection_runs WHERE started_at=$1`, now).Scan(&count); err != nil || count != 1 {
		t.Fatal("expected exactly one durable inspection run")
	}
	var runID string
	if err := pool.QueryRow(ctx, `SELECT run_id::text FROM audit.media_inspection_runs WHERE started_at=$1`, now).Scan(&runID); err != nil {
		t.Fatal("cannot read scheduled run")
	}
	repository, _ := NewPostgresRepository(pool)
	report := Inspect(Snapshot{})
	report.RunID, report.Defaults = runID, healthyDefaults()
	if err := repository.Complete(ctx, report, now.Add(time.Second)); err != nil {
		t.Fatal("cannot persist bounded successful inspection")
	}
	if err := repository.Complete(ctx, report, now.Add(2*time.Second)); err == nil {
		t.Fatal("completed run overwritten")
	}
	if err := repository.Fail(ctx, runID, "snapshot_unavailable", now.Add(2*time.Second)); err == nil {
		t.Fatal("completed run turned into failure")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.media_inspection_runs WHERE run_id=$1::uuid AND run_status='completed' AND jsonb_array_length(default_results)=2`, runID).Scan(&count); err != nil || count != 1 {
		t.Fatal("durable report incomplete")
	}
}
