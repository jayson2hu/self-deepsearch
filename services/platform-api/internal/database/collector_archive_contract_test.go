package database

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/collectoringest"
	"self-deepsearch/services/platform-api/internal/httpapi"
)

// Optional cross-language, full archive HTTP -> restricted PostgreSQL test.
func TestPostgresCollectorArchiveHTTP(t *testing.T) {
	directory, endpoint := os.Getenv("COLLECTOR_TEST_BATCH_DIR"), os.Getenv("COLLECTOR_TEST_DATABASE_URL")
	if directory == "" || endpoint == "" {
		t.Skip("prepared archive and isolated database are required")
	}
	if !validIdentityContractURL(endpoint) {
		t.Fatal("requires isolated migration test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	paths, err := filepath.Glob(filepath.Join(directory, "batch-*.json"))
	if err != nil || len(paths) != 9 {
		t.Fatal("expected nine prepared batches")
	}
	token := strings.Repeat("archive-contract-only-", 3)
	source := "12345678-1234-1234-1234-123456789abc"
	handler := httpapi.New(httpapi.Options{Collector: store, CollectorCredentials: collectoringest.Credentials{SourceID: source, Token: token}, SecureCookies: true, Now: func() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) }})
	total := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var original collectoringest.Receipt
		for pass := 0; pass < 2; pass++ {
			r := httptest.NewRequest("POST", collectoringest.Path, bytes.NewReader(raw))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Content-SHA256", collectoringest.Hash(raw))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			want := 201
			if pass == 1 {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("%s pass %d: %d %s", filepath.Base(path), pass, w.Code, w.Body)
			}
			var receipt collectoringest.Receipt
			if err = json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			if pass == 0 {
				original = receipt
				total += receipt.InsertedCount
			} else if receipt != original {
				t.Fatal("receipt changed on replay")
			}
		}
	}
	if total != 4196 {
		t.Fatalf("inserted %d, want 4196", total)
	}
	var count int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM collector.normalized_records n JOIN collector.source_records s USING(source_record_id) WHERE s.source_id=$1::uuid AND s.payload->'metadata'->>'profile_url' LIKE 'https://jable.tv/%' AND n.candidate_status='pending' AND s.payload->>'adult_status'='unknown' AND s.payload->>'rights_status'='needs_review' AND s.payload->>'publication_status'='staging'`, source).Scan(&count); err != nil || count != 4196 {
		t.Fatal("pending state mismatch", count, err)
	}
	for _, query := range []string{`SELECT count(*) FROM platform.performers`, `SELECT count(*) FROM platform.publications`, `SELECT count(*) FROM collector.media_staging`} {
		if err = store.pool.QueryRow(ctx, query).Scan(&count); err != nil || count != 0 {
			t.Fatal("unexpected entity/publication/media writes", err)
		}
	}
	t.Log("4196 real archive records staged through authenticated HTTP; nine exact replays; no formal entities, publications or media")
}
