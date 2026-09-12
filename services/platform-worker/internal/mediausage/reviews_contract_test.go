package mediausage

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explicit disposable SQL contract: no production connection or real Analytics.
// It exercises API-role inserts and real Worker review execution, not HTTP auth.
func TestPostgresMediaUsageReviewContract(t *testing.T) {
	runtimeURL, adminURL := os.Getenv("MEDIA_USAGE_CONTRACT_DATABASE_URL"), os.Getenv("MEDIA_USAGE_CONTRACT_ADMIN_DATABASE_URL")
	if runtimeURL == "" && adminURL == "" {
		t.Skip("dedicated media usage review PostgreSQL contract database is not configured")
	}
	if !validUsageContractURL(runtimeURL) || !validUsageContractURL(adminURL) || os.Getenv("CONFIRM_MEDIA_USAGE_CONTRACT") != "disposable-database" {
		t.Fatal("explicit disposable loopback configuration required")
	}
	a, _ := url.Parse(adminURL)
	r, _ := url.Parse(runtimeURL)
	if a.Host != r.Host {
		t.Fatal("same loopback server required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("cannot open restricted pool")
	}
	defer pool.Close()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("cannot open guard")
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
 (SELECT count(*) FROM platform.media_assets)+(SELECT count(*) FROM platform.outbox_events)+(SELECT count(*) FROM platform.media_usage_state)+
 (SELECT count(*) FROM audit.media_usage_runs)+(SELECT count(*) FROM audit.media_usage_reviews) FROM platform.system_metadata WHERE singleton`).Scan(&version, &existing); err != nil || version != 21 || existing != 0 {
		t.Fatal("schema 21 and empty disposable inventory required")
	}
	var restricted bool
	if err = pool.QueryRow(ctx, `SELECT current_user='platform_worker_login' AND NOT rolsuper AND NOT rolbypassrls AND pg_has_role(current_user,'platform_worker','member') FROM pg_roles WHERE rolname=current_user`).Scan(&restricted); err != nil || !restricted {
		t.Fatal("restricted Worker login required")
	}
	clock := func() time.Time {
		var now time.Time
		if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal("clock unavailable")
		}
		return now
	}
	now := clock()
	old := testSchedule()
	old.PeriodStart = now.Truncate(time.Second).Add(-48 * time.Hour)
	old.PeriodEnd = now.Truncate(time.Second).Add(-time.Hour)
	next := old
	next.PeriodStart = old.PeriodEnd
	next.PeriodEnd = next.PeriodStart.Add(24 * time.Hour)
	var actor string
	if err = admin.QueryRow(ctx, `INSERT INTO platform.users(normalized_email,password_hash,role,account_status,email_verified_at)
 VALUES ('usage-review-'||gen_random_uuid()::text||'@example.test','synthetic-contract-not-a-login-hash','owner','active',clock_timestamp()) RETURNING user_id::text`).Scan(&actor); err != nil {
		t.Fatal("synthetic actor failed")
	}
	defer func() {
		c, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM audit.media_usage_runs WHERE config_digest IN ($1,$2)`, []any{old.Fingerprint(), next.Fingerprint()}},
			{`DELETE FROM platform.media_usage_state WHERE account_ref=$1`, []any{old.AccountRef()}},
			{`DELETE FROM audit.media_usage_reviews WHERE actor_id=$1::uuid`, []any{actor}},
			{`DELETE FROM platform.users WHERE user_id=$1::uuid`, []any{actor}},
		} {
			if _, err := admin.Exec(c, q.sql, q.args...); err != nil {
				t.Error("scoped disposable cleanup failed")
			}
		}
	}()
	_, err = admin.Exec(ctx, `INSERT INTO platform.media_usage_state(config_digest,account_ref,policy_snapshot,period_start,period_end,
 high_class_a,high_class_b,last_success_at,last_until,review_required,review_since,review_reason,stop_recommended,last_status,last_reason,last_recommendation,updated_at,next_poll_at)
 VALUES($1,$2,$3::jsonb,$4,$5,950000,200,$6,$6,true,$6,'operations_reserve_reached',true,'stop_recommended','operations_reserve_reached','stop_media_review_required',$6,$6)`,
		old.Fingerprint(), old.AccountRef(), old.PolicyDocument(), old.PeriodStart, old.PeriodEnd, old.PeriodEnd.Add(-time.Minute))
	if err != nil {
		t.Fatal("synthetic prior period failed")
	}
	repo, _ := NewPostgresRepository(pool, next)
	queue := func(action string, expired, stale bool) string {
		tx, e := admin.Begin(ctx)
		if e != nil {
			t.Fatal("queue begin failed")
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, e = tx.Exec(ctx, `SET LOCAL ROLE platform_api`); e != nil {
			t.Fatal("cannot select API role")
		}
		var start, end any
		if action == "rotate_period" {
			start, end = next.PeriodStart, next.PeriodEnd
		}
		at := clock()
		if expired {
			at = at.Add(-10 * time.Minute)
		}
		delta := time.Duration(0)
		if stale {
			delta = time.Second
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO audit.media_usage_reviews(actor_id,idempotency_key,action,expected_digest,expected_updated_at,next_period_start,next_period_end,reason,request_id,created_at,expires_at)
 SELECT $1::uuid,gen_random_uuid(),$2,config_digest,updated_at+$7::double precision*interval '1 second',$3,$4,'synthetic operator review','usage-review-contract',$5,$6
 FROM platform.media_usage_state WHERE singleton RETURNING review_id::text`, actor, action, start, end, at, at.Add(5*time.Minute), delta.Seconds()).Scan(&id)
		if e != nil {
			t.Fatal("API role queue failed")
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal("queue commit failed")
		}
		return id
	}
	assertStatus := func(id, want string) {
		var status string
		if e := pool.QueryRow(ctx, `SELECT status FROM audit.media_usage_reviews WHERE review_id=$1::uuid`, id).Scan(&status); e != nil || status != want {
			t.Fatalf("review outcome: got %s want %s", status, want)
		}
	}
	rotate := queue("rotate_period", false, false)
	if err = repo.ProcessReviews(ctx, clock()); err != nil {
		t.Fatal("rotation failed", err)
	}
	assertStatus(rotate, "applied")
	s, err := repo.ReadMetricsState(ctx)
	if err != nil || s.HighWater.ClassA != 0 || !s.ReviewRequired || s.StopRecommended || s.LastAssessment.Recommendation != "hold_for_review" {
		t.Fatal("rotation automatically resumed or lost new-period state")
	}
	var before string
	if err = pool.QueryRow(ctx, `SELECT before_state->>'high_class_a' FROM audit.media_usage_reviews WHERE review_id=$1::uuid`, rotate).Scan(&before); err != nil || before != "950000" {
		t.Fatal("old period evidence lost")
	}
	claim, err := repo.Prepare(ctx, clock())
	if err != nil {
		t.Fatal("new-period observation not due")
	}
	received := clock()
	obs := &Observation{Window: claim.Window, ReceivedAt: received, Segments: 1, BoundaryPolicy: "inclusive_shared_endpoints", Counts: Counts{ClassA: 10, ClassB: 20}}
	if _, err = repo.Finish(ctx, claim, obs, nil, received); err != nil {
		t.Fatal("new observation failed", err)
	}
	ack := queue("acknowledge", false, false)
	if err = repo.ProcessReviews(ctx, clock()); err != nil {
		t.Fatal("ack failed", err)
	}
	assertStatus(ack, "applied")
	s, err = repo.ReadMetricsState(ctx)
	if err != nil || s.ReviewRequired || s.HighWater.ClassA != 10 {
		t.Fatal("ack did not preserve counters")
	}
	for _, tc := range []struct {
		name                     string
		expired, stale, inactive bool
		code                     string
	}{{"expired", true, false, false, "usage_review_expired"}, {"stale", false, true, false, "usage_review_stale"}, {"revoked", false, false, true, "usage_reviewer_inactive"}} {
		id := queue("acknowledge", tc.expired, tc.stale)
		if tc.inactive {
			if _, err = admin.Exec(ctx, `UPDATE platform.users SET role='editor' WHERE user_id=$1::uuid`, actor); err != nil {
				t.Fatal("role change failed")
			}
		}
		if err = repo.ProcessReviews(ctx, clock()); err != nil {
			t.Fatal("rejection failed", err)
		}
		assertStatus(id, "rejected")
		var code string
		if err = pool.QueryRow(ctx, `SELECT error_code FROM audit.media_usage_reviews WHERE review_id=$1::uuid`, id).Scan(&code); err != nil || code != tc.code {
			t.Fatal("wrong rejection code", tc.name)
		}
	}
	for _, tc := range []struct{ role, sql string }{
		{"platform_api", `UPDATE audit.media_usage_reviews SET status='pending'`},
		{"platform_worker", `DELETE FROM audit.media_usage_reviews`},
		{"platform_worker", `SELECT password_hash FROM platform.users`},
		{"public_reader", `SELECT * FROM audit.media_usage_reviews`},
		{"public_reader", `SELECT platform.lock_media_usage_reviewer('` + actor + `')`},
		{"platform_worker", `UPDATE platform.media_usage_state SET high_class_a=0`},
		{"platform_worker", `UPDATE audit.media_usage_reviews SET reason='changed'`},
	} {
		tx, e := admin.Begin(ctx)
		if e != nil {
			t.Fatal("permission begin failed")
		}
		if _, e = tx.Exec(ctx, "SET LOCAL ROLE "+tc.role); e != nil {
			t.Fatal("permission role failed")
		}
		_, e = tx.Exec(ctx, tc.sql)
		_ = tx.Rollback(ctx)
		if e == nil {
			t.Fatal("permission/evidence guard escaped")
		}
	}
	// A Worker cannot claim application by writing only a review receipt.
	if _, err = admin.Exec(ctx, `UPDATE platform.users SET role='owner' WHERE user_id=$1::uuid`, actor); err != nil {
		t.Fatal("restore synthetic owner failed")
	}
	id := queue("acknowledge", false, false)
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal("receipt test begin failed")
	}
	_, err = tx.Exec(ctx, `UPDATE audit.media_usage_reviews SET status='applied',completed_at=clock_timestamp(),before_state=to_jsonb(state),after_state=to_jsonb(state) FROM platform.media_usage_state state WHERE state.singleton AND review_id=$1::uuid`, id)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal("receipt test setup failed", err)
	}
	if err = tx.Commit(ctx); err == nil {
		t.Fatal("receipt committed without matching state transition")
	}
	assertStatus(id, "pending")
	if _, err = pool.Exec(ctx, `UPDATE audit.media_usage_reviews SET status='rejected',completed_at=clock_timestamp(),error_code='usage_review_not_recoverable' WHERE review_id=$1::uuid`, id); err != nil {
		t.Fatal("pending test cleanup failed")
	}
	if !strings.Contains(next.PolicyDocument(), "max_age_seconds") {
		t.Fatal("policy fixture incomplete")
	}
}
