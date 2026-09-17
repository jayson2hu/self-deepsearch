package database

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func TestPostgresSEOReportImportIsIdempotentAndSeparatesDatasetKind(t *testing.T) {
	databaseURL := os.Getenv("SEO_WORKFLOW_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SEO_WORKFLOW_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var actorID string
	if err := store.pool.QueryRow(ctx, `SELECT user_id::text FROM platform.users WHERE account_status='active' AND role IN ('owner','admin') ORDER BY user_id LIMIT 1`).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 17, 13, 0, 0, 0, time.UTC)
	input := operations.SEOReportImportInput{ReportType: "performance", DatasetKind: "demo", PropertyURI: "sc-domain:example.test",
		PeriodStart: now.AddDate(0, 0, -7), PeriodEnd: now, ExportedAt: now, FileSHA256: strings.Repeat("a", 64),
		Rows: []operations.SEOReportRow{{Dimensions: map[string]string{"query": "幕鉴", "page": "https://example.test/works/a"},
			Metrics: map[string]float64{"clicks": 1, "impressions": 10, "ctr": 0.1, "position": 2.5}}}}
	first, err := store.ImportSEOReport(ctx, input, actorID, "seo-report-contract-first", now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM platform.seo_report_rows WHERE import_id=$1::uuid`, first.ID)
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM platform.seo_report_imports WHERE import_id=$1::uuid`, first.ID)
	}()
	second, err := store.ImportSEOReport(ctx, input, actorID, "seo-report-contract-repeat", now.Add(time.Minute))
	if err != nil || second.ID != first.ID {
		t.Fatalf("repeat import was not idempotent: first=%s second=%s err=%v", first.ID, second.ID, err)
	}
	var imports, rows int
	if err := store.pool.QueryRow(ctx, `SELECT count(*),(SELECT count(*) FROM platform.seo_report_rows WHERE import_id=$1::uuid)
FROM platform.seo_report_imports WHERE import_id=$1::uuid`, first.ID).Scan(&imports, &rows); err != nil || imports != 1 || rows != 1 {
		t.Fatalf("unexpected report persistence: imports=%d rows=%d err=%v", imports, rows, err)
	}
	input.DatasetKind, input.FileSHA256 = "real", strings.Repeat("b", 64)
	real, err := store.ImportSEOReport(ctx, input, actorID, "seo-report-contract-real", now.Add(2*time.Minute))
	if err != nil || real.ID == first.ID {
		t.Fatalf("real/demo datasets were not separated: demo=%s real=%s err=%v", first.ID, real.ID, err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM platform.seo_report_rows WHERE import_id=$1::uuid`, real.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `DELETE FROM platform.seo_report_imports WHERE import_id=$1::uuid`, real.ID); err != nil {
		t.Fatal(err)
	}
}
