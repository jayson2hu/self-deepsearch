package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func TestSEOFullCyclePostgres(t *testing.T) {
	dsn := os.Getenv("SEO_CYCLE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires exact disposable clone")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/self_deepsearch_stage_name_test_20260919" || u.RawQuery != "sslmode=disable" || u.User == nil || u.User.Username() != "platform_api_login" {
		t.Fatal("unsafe test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h := &invitationContractHarness{store: store, ctx: ctx, prefix: fmt.Sprintf("seo-cycle-%d", time.Now().UnixNano())}
	actor := h.insertUser(t, h.newEmail("owner"), "owner", "active")
	reviewer := h.insertUser(t, h.newEmail("reviewer"), "admin", "active")
	_, err = store.pool.Exec(ctx, `WITH entities AS (
 INSERT INTO platform.studios(name,normalized_name,canonical_slug,publication_status)
 SELECT $1::text||g,$1::text||g,$1::text||g,'published' FROM generate_series(1,2050)g RETURNING studio_id,canonical_slug
 ), revisions AS (
 INSERT INTO platform.content_revisions(entity_type,entity_id,version,payload,revision_status,author_id,reviewer_id,reason,submitted_at,reviewed_at)
 SELECT 'studio',studio_id,1,'{}','approved',$2::uuid,$3::uuid,'isolated batch scan test',now(),now() FROM entities RETURNING revision_id,entity_id
 ) INSERT INTO platform.publications(entity_type,entity_id,current_revision_id,publication_status,canonical_slug,published_at)
 SELECT 'studio',e.studio_id,r.revision_id,'published',e.canonical_slug,now() FROM entities e JOIN revisions r ON r.entity_id=e.studio_id`, h.prefix, actor, reviewer)
	if err != nil {
		t.Fatal(err)
	}
	var targets int
	if err = store.pool.QueryRow(ctx, seoInventoryCTE+` SELECT count(*) FROM classified WHERE publication_status='published'`).Scan(&targets); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2055, 9, 19, 10, 0, 0, 0, time.UTC)
	started := time.Now()
	scan, err := store.RunSEOScan(ctx, actor, "cycle-contract", now)
	if err != nil {
		t.Fatal(err)
	}
	if scan.TotalScannedCount != targets || scan.BatchCount != (targets+999)/1000 || scan.ScannedCount > 1000 || scan.Truncated {
		t.Fatalf("incomplete scan: %+v targets=%d", scan, targets)
	}
	var audited int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_logs WHERE after_version->>'cycle_id'=$1`, scan.CycleID).Scan(&audited); err != nil || audited != scan.BatchCount {
		t.Fatal("missing batch audit", err, audited)
	}
	t.Logf("targets=%d batches=%d scanned=%d audit_batches=%d elapsed=%s", targets, scan.BatchCount, scan.TotalScannedCount, audited, time.Since(started))
	daily, err := store.RunScheduledSEOScan(ctx, actor, now)
	if err != nil || daily.ID == "" {
		t.Fatal("daily run failed", err)
	}
	repeated, err := store.RunScheduledSEOScan(ctx, actor, now.Add(time.Hour))
	if err != nil || repeated.ID != "" {
		t.Fatal("same day duplicated", err)
	}
	next, err := store.RunScheduledSEOScan(ctx, actor, now.AddDate(0, 0, 1))
	if err != nil || next.ID == "" {
		t.Fatal("next day skipped", err)
	}
	var systemAudit int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM audit.audit_logs WHERE after_version->>'cycle_id'=$1 AND actor_type='system'`, daily.CycleID).Scan(&systemAudit); err != nil || systemAudit != daily.BatchCount {
		t.Fatal("scheduled job impersonated a human", err)
	}
	lock, err := store.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(ctx, `SELECT pg_advisory_xact_lock(20260919,607)`); err != nil {
		t.Fatal(err)
	}
	_, err = store.RunSEOScan(ctx, actor, "concurrent", now)
	_ = lock.Rollback(ctx)
	if !errors.Is(err, operations.ErrConflict) {
		t.Fatal("overlap not rejected", err)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err = store.RunSEOScan(canceled, actor, "canceled", now); err == nil {
		t.Fatal("cancellation ignored")
	}
	if _, err = store.RunScheduledSEOScan(ctx, "ffffffff-ffff-4fff-8fff-ffffffffffff", now); !errors.Is(err, operations.ErrForbidden) {
		t.Fatal("unconfigured actor allowed", err)
	}
	t.Log("same-day idempotence, next-day run, system audit, overlap rejection, cancellation and actor authorization passed")

	// Inject a failure only on the second batch of this isolated clone. Ensure
	// the first batch's rows/issues/audit are rolled back, then retry successfully.
	adminURL := os.Getenv("SEO_CYCLE_ADMIN_DATABASE_URL")
	adminParsed, err := url.Parse(adminURL)
	if err != nil || adminParsed.Host != u.Host || adminParsed.Path != u.Path || adminParsed.RawQuery != "sslmode=disable" || adminParsed.User == nil || adminParsed.User.Username() != "platform" {
		t.Fatal("requires matching isolated admin URL for fault injection")
	}
	admin, err := Open(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	failureTime := now.AddDate(0, 0, 2)
	_, err = admin.pool.Exec(ctx, fmt.Sprintf(`CREATE OR REPLACE FUNCTION platform.test_fail_second_seo_batch() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.started_at='%s'::timestamptz AND EXISTS(SELECT 1 FROM platform.seo_scan_runs WHERE started_at=NEW.started_at) THEN RAISE EXCEPTION 'isolated second batch failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER test_second_seo_failure BEFORE INSERT ON platform.seo_scan_runs FOR EACH ROW EXECUTE FUNCTION platform.test_fail_second_seo_batch();`, failureTime.Format(time.RFC3339)))
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_, _ = admin.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_second_seo_failure ON platform.seo_scan_runs; DROP FUNCTION IF EXISTS platform.test_fail_second_seo_batch();`)
	}
	defer cleanup()
	var before int
	_ = store.pool.QueryRow(ctx, `SELECT count(*) FROM platform.seo_scan_runs`).Scan(&before)
	if _, err = store.RunScheduledSEOScan(ctx, actor, failureTime); err == nil {
		t.Fatal("injected failure was ignored")
	}
	var after int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM platform.seo_scan_runs`).Scan(&after); err != nil || after != before {
		t.Fatal("partial cycle survived rollback", err)
	}
	cleanup()
	retry, err := store.RunScheduledSEOScan(ctx, actor, failureTime)
	if err != nil || retry.TotalScannedCount != targets {
		t.Fatal("failed cycle cannot retry", err)
	}
	t.Log("second-batch failure rolls back complete cycle; same-day retry scans all targets")
}
