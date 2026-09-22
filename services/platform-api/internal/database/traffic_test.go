package database

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/catalog"
)

// This test intentionally writes only to an explicitly named disposable clone.
// Restore a review backup into self_deepsearch_traffic_test_20260919 first.
func TestTrafficPostgresContract(t *testing.T) {
	dsn := os.Getenv("TRAFFIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL clone")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/self_deepsearch_traffic_test_20260919" {
		t.Fatal("refusing writes outside exact disposable local test database")
	}
	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := store.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`DELETE FROM platform.content_metrics_hourly`)
	exec(`DELETE FROM platform.view_history`)
	now := time.Date(2026, 9, 19, 4, 0, 0, 0, time.UTC)
	var work, performer, studio string
	for _, entry := range []struct {
		sql string
		id  *string
	}{
		{`SELECT work_id::text FROM platform.public_published_works ORDER BY work_id LIMIT 1`, &work},
		{`SELECT performer_id::text FROM platform.public_published_performers ORDER BY performer_id LIMIT 1`, &performer},
		{`SELECT studio_id::text FROM platform.public_published_studios ORDER BY studio_id LIMIT 1`, &studio},
	} {
		if err := store.pool.QueryRow(ctx, entry.sql).Scan(entry.id); err != nil {
			t.Fatal(err)
		}
	}
	insertPV := func(kind, id string, at time.Time, count int) {
		exec(`INSERT INTO platform.content_metrics_hourly(content_type,content_id,hour_bucket,page_views) VALUES($1,$2,$3,$4)`, kind, id, at, count)
	}
	insertPV("work", work, now, 5)
	insertPV("performer", performer, now, 5)
	insertPV("studio", studio, now, 2)
	insertPV("work", work, now.AddDate(0, 0, -8), 7)
	insertPV("work", work, now.AddDate(0, 0, -31), 11)
	insertPV("work", work, time.Date(2026, 9, 12, 16, 0, 0, 0, time.UTC), 3)  // Shanghai start of 7-day window.
	insertPV("work", work, time.Date(2026, 9, 12, 15, 0, 0, 0, time.UTC), 13) // One hour outside.
	insertPV("work", "ffffffff-ffff-4fff-8fff-ffffffffffff", now, 17)         // No longer public: total retained, ranking excluded.
	insertPV("work", work, now.AddDate(0, 0, 1), 99)                          // Future excluded.
	for i, account := range []struct {
		role, status string
		test         bool
	}{{"user", "active", false}, {"user", "active", true}, {"admin", "active", false}, {"user", "closed", false}} {
		var id string
		err := store.pool.QueryRow(ctx, `INSERT INTO platform.users(normalized_email,password_hash,role,account_status,email_verified_at,closed_at,is_test) VALUES($1,repeat('x',60),$2,$3,$4,CASE WHEN $3='closed' THEN $4::timestamptz ELSE NULL END,$5) RETURNING user_id::text`, fmt.Sprintf("traffic-%d-%d@example.invalid", time.Now().UnixNano(), i), account.role, account.status, now, account.test).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO platform.view_history(user_id,content_type,content_id,viewed_at,is_deleted,deleted_at) VALUES($1,'work',$2,$3,true,$3)`, id, work, now)
		if i == 0 {
			exec(`INSERT INTO platform.view_history(user_id,content_type,content_id,viewed_at) VALUES($1,'work',$2,$3),($1,'work',$2,$4),($1,'work',$2,$5)`, id, work, now, now.AddDate(0, 0, -8), now.AddDate(0, 0, 1))
		}
	}
	for _, test := range []struct {
		period    string
		pv, users int64
		days      int
	}{{"7d", 32, 2, 7}, {"30d", 52, 3, 30}, {"all", 63, 3, 30}} {
		report, err := store.TrafficReport(ctx, test.period, now)
		if err != nil {
			t.Fatal(err)
		}
		if report.PageViews != test.pv || report.TodayPageViews != 29 || report.LoggedInUserViews != test.users || report.ActiveRegisteredUsers != 1 || report.UniqueVisitors == nil || *report.UniqueVisitors != 0 || len(report.Users) != 1 || len(report.Daily) != test.days {
			t.Fatalf("%s unexpected report: %+v", test.period, report)
		}
		if len(report.HotContent) != 3 || report.HotContent[0].ID != work || report.HotContent[0].Rank != 1 {
			t.Fatalf("invalid published ranking: %+v", report.HotContent)
		}
		if report.Daily[len(report.Daily)-1].PageViews != 29 || report.Daily[1].PageViews != 0 {
			t.Fatal("daily totals/zero fill mismatch")
		}
		t.Logf("range=%s PV=%d today=%d registeredViews=%d activeRegistered=%d dailyRows=%d hotRows=%d UV=0", test.period, report.PageViews, report.TodayPageViews, report.LoggedInUserViews, report.ActiveRegisteredUsers, len(report.Daily), len(report.HotContent))
	}
	exec(`DELETE FROM platform.content_metrics_hourly`)
	exec(`DELETE FROM platform.view_history`)
	report, err := store.TrafficReport(ctx, "7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if report.PageViews != 0 || len(report.HotContent) != 0 || len(report.Users) != 0 || len(report.Daily) != 7 {
		t.Fatal("empty data contract failed")
	}
	if _, err := store.TrafficReport(ctx, "invalid", now); err == nil {
		t.Fatal("invalid range accepted")
	}
	// List reads never count. Actual detail reporting counts once per accepted
	// event, for all three public types; unknown/unpublished entities cannot count.
	if _, err := store.ListPerformers(ctx, "", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListPerformers(ctx, performer, 2); err != nil {
		t.Fatal(err)
	}
	report, err = store.TrafficReport(ctx, "all", time.Now())
	if err != nil || report.PageViews != 0 {
		t.Fatal("list pagination changed PV", err)
	}
	for kind, id := range map[string]string{"work": work, "performer": performer, "studio": studio} {
		if err := store.RecordPageView(ctx, kind, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.RecordPageView(ctx, "work", "ffffffff-ffff-4fff-8fff-ffffffffffff"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("unknown entity counted", err)
	}
	report, err = store.TrafficReport(ctx, "all", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if report.PageViews != 3 || len(report.HotContent) != 3 || report.HotContent[0].EntityType != "performer" || report.HotContent[1].EntityType != "studio" || report.HotContent[2].EntityType != "work" {
		t.Fatalf("reporting/tie sorting failed: %+v", report)
	}
	t.Log("list pagination leaves PV unchanged; 3 detail types each add 1; unpublished report rejected; tied ranking stable")
}
