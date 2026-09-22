package database

import (
	"context"
	"errors"
	"net/url"
	"os"
	"self-deepsearch/services/platform-api/internal/catalog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVisitorMetricsPostgres(t *testing.T) {
	dsn := os.Getenv("VISITOR_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires disposable clone")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Path != "/self_deepsearch_stage_name_test_20260919" || u.RawQuery != "sslmode=disable" || u.User == nil || u.User.Username() != "platform_api_login" {
		t.Fatal("unsafe test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.pool.Exec(ctx, `DELETE FROM platform.visitor_metrics_daily`); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = store.pool.QueryRow(ctx, `SELECT work_id::text FROM platform.public_published_works LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	before, err := store.TrafficReport(ctx, "all", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	for _, hash := range []string{a, a, b, ""} {
		if err = store.RecordPageViewWithVisitor(ctx, "work", id, hash); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.RecordPageViewWithVisitor(ctx, "work", "ffffffff-ffff-4fff-8fff-ffffffffffff", a); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatal("unpublished counted", err)
	}
	if err = store.RecordPageViewWithVisitor(ctx, "work", id, "bad-hash"); err == nil {
		t.Fatal("invalid hash accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- store.RecordPageViewWithVisitor(ctx, "work", id, c) }()
	}
	wg.Wait()
	close(errs)
	for err = range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := store.TrafficReport(ctx, "all", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if after.PageViews-before.PageViews != 10 || after.UniqueVisitors == nil || *after.UniqueVisitors != 3 || after.ConsentedPageViews != 9 || after.UniqueVisitorToday != 3 {
		t.Fatalf("PV/UV mismatch: before=%+v after=%+v", before, after)
	}
	now := time.Now()
	day := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.DateOnly)
	_, err = store.pool.Exec(ctx, `INSERT INTO platform.visitor_metrics_daily VALUES($1::date-1,$2,2),($1::date-8,$3,1),($1::date-31,$4,1)`, day, a, strings.Repeat("d", 64), strings.Repeat("e", 64))
	if err != nil {
		t.Fatal(err)
	}
	seven, err := store.TrafficReport(ctx, "7d", now)
	if err != nil {
		t.Fatal(err)
	}
	all, err := store.TrafficReport(ctx, "all", now)
	if err != nil {
		t.Fatal(err)
	}
	if *seven.UniqueVisitors != 3 || *all.UniqueVisitors != 4 || all.ConsentedPageViews != 12 {
		t.Fatal("range deduplication or 30-day cap failed")
	}
	deleted, err := store.PurgeVisitorMetrics(ctx, now)
	if err != nil || deleted != 1 {
		t.Fatal("retention failed", err, deleted)
	}
	t.Log("10 detail PV / 3 distinct browsers / 9 consented PV; 6 concurrent updates counted exactly; invalid entity/hash leaves no partial PV; cross-day dedup and 30-day cap passed; 1 expired row purged")
}
