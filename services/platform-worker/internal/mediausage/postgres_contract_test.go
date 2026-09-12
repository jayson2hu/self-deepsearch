package mediausage

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

func validUsageContractURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && u.Scheme == "postgres" && u.Hostname() == "127.0.0.1" && u.Port() != "" && u.User != nil && u.Path == "/self_deepsearch_worker_test" &&
		(u.RawQuery == "" || u.RawQuery == "sslmode=disable") && !u.ForceQuery && u.Fragment == "" && !strings.Contains(value, "#")
}

func TestUsageContractGuard(t *testing.T) {
	good := "postgres://worker:synthetic@127.0.0.1:5432/self_deepsearch_worker_test"
	if !validUsageContractURL(good) || !validUsageContractURL(good+"?sslmode=disable") {
		t.Fatal("safe contract URL rejected")
	}
	for _, bad := range []string{strings.Replace(good, "127.0.0.1", "remote.test", 1), strings.Replace(good, "worker_test", "production", 1), good + "?host=remote.test", good + "?", good + "#", "host=127.0.0.1 dbname=self_deepsearch_worker_test"} {
		if validUsageContractURL(bad) {
			t.Fatal("unsafe contract accepted")
		}
	}
}

// This test is deliberately skipped without explicit disposable-loopback
// configuration. Transaction substitutes cannot prove these SQL invariants.
func TestPostgresMediaUsageStateContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("MEDIA_USAGE_CONTRACT_DATABASE_URL"), os.Getenv("MEDIA_USAGE_CONTRACT_ADMIN_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated media usage PostgreSQL contract database is not configured")
	}
	a, _ := url.Parse(adminURL)
	r, _ := url.Parse(runtimeURL)
	if os.Getenv("CONFIRM_MEDIA_USAGE_CONTRACT") != "disposable-database" || !validUsageContractURL(runtimeURL) || !validUsageContractURL(adminURL) || a.Host != r.Host {
		t.Fatal("explicit same-server disposable loopback database required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(runtimeURL)
	if err != nil {
		t.Fatal("invalid runtime configuration")
	}
	config.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("cannot open runtime pool")
	}
	defer func() { cancel(); pool.Close() }()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open owner guard")
	}
	defer admin.Close()
	guard, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire guard")
	}
	defer guard.Release()
	var locked bool
	if err := guard.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('release-a-worker-outbox-contract'))`).Scan(&locked); err != nil || !locked {
		t.Fatal("worker contracts must run serially")
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = guard.Exec(cleanup, `SELECT pg_advisory_unlock(hashtext('release-a-worker-outbox-contract'))`)
	}()
	var version, existing int
	if err := admin.QueryRow(ctx, `SELECT schema_version,(SELECT count(*) FROM platform.users)+(SELECT count(*) FROM platform.works)+
 (SELECT count(*) FROM platform.media_assets)+(SELECT count(*) FROM platform.outbox_events)+
 (SELECT count(*) FROM platform.media_usage_state)+(SELECT count(*) FROM audit.media_usage_runs)
 FROM platform.system_metadata WHERE singleton`).Scan(&version, &existing); err != nil || version != 21 || existing != 0 {
		t.Fatal("schema 21 and empty disposable worker/usage inventory required")
	}
	var restricted bool
	if err := pool.QueryRow(ctx, `SELECT current_user='platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls AND pg_has_role(current_user,'platform_worker','member') FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("restricted worker login required")
	}
	var now time.Time
	if err := pool.QueryRow(ctx, `SELECT date_trunc('second',clock_timestamp())`).Scan(&now); err != nil {
		t.Fatal("cannot read test clock")
	}
	schedule := testSchedule()
	schedule.PeriodStart = now.Add(-time.Hour)
	schedule.PeriodEnd = now.Add(24 * time.Hour)
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = admin.Exec(cleanup, `DELETE FROM audit.media_usage_runs WHERE config_digest=$1`, schedule.Fingerprint())
		_, _ = admin.Exec(cleanup, `DELETE FROM platform.media_usage_state WHERE config_digest=$1`, schedule.Fingerprint())
	}()
	repo, err := NewPostgresRepository(pool, schedule)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("cannot acquire schedule guard")
	}
	defer blocker.Release()
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_lock(hashtext('self-deepsearch-media-usage'))`); err != nil {
		t.Fatal("cannot hold schedule lock")
	}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_, _ = blocker.Exec(cleanup, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-usage'))`)
	}()
	defer cancel()
	pids := make(chan int32, 2)
	begin := func(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
		tx, err := pool.BeginTx(ctx, opts)
		if err == nil {
			pids <- int32(tx.Conn().PgConn().PID())
		}
		return tx, err
	}
	concurrent := PostgresRepository{begin, schedule}
	type result struct {
		run Run
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { run, err := concurrent.Prepare(ctx, now); results <- result{run, err} }()
	}
	var waitPIDs []int32
	for len(waitPIDs) < 2 {
		select {
		case pid := <-pids:
			waitPIDs = append(waitPIDs, pid)
		case <-ctx.Done():
			t.Fatal("schedule contenders did not start")
		}
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND NOT granted AND pid=ANY($1::int[])`, waitPIDs).Scan(&waiting); err != nil {
			t.Fatal("cannot observe real lock waiters")
		}
		if waiting == 2 {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("both schedule waiters not observed")
		}
	}
	if _, err := blocker.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('self-deepsearch-media-usage'))`); err != nil {
		t.Fatal("cannot release schedule guard")
	}
	winners, notDue := 0, 0
	var first Run
	for range 2 {
		select {
		case got := <-results:
			if got.err == nil {
				winners++
				first = got.run
			} else if errors.Is(got.err, ErrNotDue) {
				notDue++
			} else {
				t.Fatal("schedule contender failed")
			}
		case <-ctx.Done():
			t.Fatal("schedule did not complete")
		}
	}
	if winners != 1 || notDue != 1 {
		t.Fatal("duplicate schedule admitted")
	}
	makeObservation := func(run Run, at time.Time, a, b uint64) *Observation {
		return &Observation{Window: run.Window, ReceivedAt: at, Segments: 1, BoundaryPolicy: "inclusive_shared_endpoints", Counts: Counts{a, b, 0}}
	}
	state, err := repo.Finish(ctx, first, makeObservation(first, now.Add(time.Second), 100, 200), nil, now.Add(time.Second))
	if err != nil || state.HighWater.ClassA != 100 {
		t.Fatalf("initial observation not persisted: %v", err)
	}
	if _, err := repo.Finish(ctx, first, nil, ErrUnavailable, now.Add(2*time.Second)); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("completed observation overwritten")
	}
	guardNow := now.Add(time.Second)
	taskGuard, err := NewTaskGuard(repo, schedule, func() time.Time { return guardNow })
	if err != nil || taskGuard.Allow(ctx) != nil {
		t.Fatal("restricted persisted low observation did not admit scan")
	}
	if _, err := pool.Exec(ctx, `UPDATE audit.media_usage_runs SET observation='{}' WHERE run_id=$1`, first.ID); err == nil {
		t.Fatal("completed audit record was mutable")
	}

	failedAt := now.Add(16 * time.Minute)
	failed, err := repo.Prepare(ctx, failedAt)
	if err != nil {
		t.Fatal("next schedule not due")
	}
	state, err = repo.Finish(ctx, failed, nil, ErrUnavailable, failedAt.Add(time.Second))
	if err != nil || !state.ReviewRequired || state.HighWater.ClassA != 100 {
		t.Fatal("failure lost last valid evidence")
	}
	guardNow = failedAt.Add(time.Second)
	if !errors.Is(taskGuard.Allow(ctx), ErrTaskAdmission) {
		t.Fatal("persisted failure/review admitted scan")
	}
	stopAt := now.Add(32 * time.Minute)
	stopRun, err := repo.Prepare(ctx, stopAt)
	if err != nil {
		t.Fatal("stop observation not due")
	}
	state, err = repo.Finish(ctx, stopRun, makeObservation(stopRun, stopAt.Add(time.Second), 950000, 200), nil, stopAt.Add(time.Second))
	if err != nil || !state.StopRecommended {
		t.Fatal("stop latch missing")
	}
	restarted, _ := NewPostgresRepository(pool, schedule)
	restored, err := restarted.ReadMetricsState(ctx)
	if err != nil || !restored.StopRecommended || !restored.ReviewRequired {
		t.Fatal("restart lost latches")
	}
	guardNow = stopAt.Add(time.Second)
	restartedGuard, err := NewTaskGuard(restarted, schedule, func() time.Time { return guardNow })
	if err != nil || !errors.Is(restartedGuard.Allow(ctx), ErrTaskAdmission) {
		t.Fatal("restarted guard lost persisted stop")
	}
	for _, sql := range []string{
		`UPDATE platform.media_usage_state SET high_class_a=0 WHERE singleton`,
		`UPDATE platform.media_usage_state SET review_required=false,review_since=NULL,review_reason=NULL WHERE singleton`,
		`UPDATE platform.media_usage_state SET stop_recommended=false WHERE singleton`,
		`UPDATE platform.media_usage_state SET config_digest=repeat('f',64) WHERE singleton`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Fatal("runtime SQL bypassed state latch")
		}
	}
	for _, item := range []struct{ role, sql string }{{"platform_api", `UPDATE platform.media_usage_state SET updated_at=now()`}, {"public_reader", `SELECT * FROM platform.media_usage_state`}, {"public_reader", `SELECT * FROM audit.media_usage_runs`}, {"platform_worker", `DELETE FROM audit.media_usage_runs`}} {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal("cannot start permission test")
		}
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE "+item.role); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("cannot select test role")
		}
		_, err = tx.Exec(ctx, item.sql)
		_ = tx.Rollback(ctx)
		if err == nil {
			t.Fatal("private state/audit permission escaped")
		}
	}
	lowerAt := now.Add(48 * time.Minute)
	lowerRun, err := repo.Prepare(ctx, lowerAt)
	if err != nil {
		t.Fatal("regression run not due")
	}
	state, err = repo.Finish(ctx, lowerRun, makeObservation(lowerRun, lowerAt.Add(time.Second), 1, 2), nil, lowerAt.Add(time.Second))
	if err != nil || state.HighWater.ClassA != 950000 || !state.StopRecommended || state.LastAssessment.Reason != "usage_counter_regressed" {
		t.Fatal("lower estimate restored quota")
	}
	var status, code string
	if err := pool.QueryRow(ctx, `SELECT run_status,error_code FROM audit.media_usage_runs WHERE run_id=$1`, lowerRun.ID).Scan(&status, &code); err != nil || status != "failed" || code != "usage_counter_regressed" {
		t.Fatal("regression was recorded as a complete estimate")
	}
	crashAt := now.Add(64 * time.Minute)
	crashRun, err := repo.Prepare(ctx, crashAt)
	if err != nil {
		t.Fatal("crash run not due")
	}
	if _, err := repo.Finish(ctx, crashRun, nil, ErrUnavailable, crashAt.Add(3*time.Minute)); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("expired lease accepted completion")
	}
	replacement, err := repo.Prepare(ctx, now.Add(80*time.Minute))
	if err != nil || replacement.ID == crashRun.ID {
		t.Fatal("interrupted run was not replaced")
	}
	if err := pool.QueryRow(ctx, `SELECT run_status,error_code FROM audit.media_usage_runs WHERE run_id=$1`, crashRun.ID).Scan(&status, &code); err != nil || status != "failed" || code != "usage_interrupted" {
		t.Fatal("interruption was not preserved")
	}
	changed := schedule
	changed.AccountID = strings.Repeat("f", 32)
	different, _ := NewPostgresRepository(pool, changed)
	if _, err := different.Prepare(ctx, now.Add(81*time.Minute)); !errors.Is(err, ErrScheduleChanged) {
		t.Fatal("different account reused persistent state")
	}
	differentGuard, err := NewTaskGuard(different, changed, func() time.Time { return now.Add(81 * time.Minute) })
	if err != nil || !errors.Is(differentGuard.Allow(ctx), ErrTaskAdmission) {
		t.Fatal("different configured account was admitted")
	}
}
