package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"self-deepsearch/services/platform-api/internal/collectoringest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPostgresCollectorIngest(t *testing.T) {
	endpoint := os.Getenv("COLLECTOR_TEST_DATABASE_URL")
	if endpoint == "" {
		t.Skip("COLLECTOR_TEST_DATABASE_URL is not set")
	}
	if !validIdentityContractURL(endpoint) {
		t.Fatal("requires isolated migration test database with restricted role")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prefix := time.Now().Format("150405.000000000")
	b := collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abc", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "contract-" + prefix, Records: []collectoringest.Record{{ExternalID: "synthetic-" + prefix, CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: "Synthetic", Aliases: []string{}, ProfileURL: "https://example.test/person", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}}
	var baseline int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM platform.performers`).Scan(&baseline); err != nil {
		t.Fatal(err)
	}
	ingest := func(batch collectoringest.Batch) (collectoringest.Receipt, bool, error) {
		raw, err := json.Marshal(batch)
		if err != nil {
			return collectoringest.Receipt{}, false, err
		}
		return store.IngestPerformers(ctx, batch, collectoringest.Hash(raw))
	}
	first, replay, err := ingest(b)
	if err != nil || replay || first.InsertedCount != 1 {
		t.Fatalf("first: %+v %v", first, err)
	}
	// Preserve manually changed candidate state through same and different batch keys.
	_, err = store.pool.Exec(ctx, `UPDATE collector.normalized_records SET candidate_status='reviewing' WHERE source_record_id IN (SELECT source_record_id FROM collector.source_records WHERE batch_id=$1::uuid)`, first.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	again, replay, err := ingest(b)
	if err != nil || !replay || again != first {
		t.Fatalf("replay: %+v %v", again, err)
	}
	if _, _, err = store.IngestPerformers(ctx, b, collectoringest.Hash([]byte("changed"))); !errors.Is(err, collectoringest.ErrConflict) {
		t.Fatal("same key different content accepted", err)
	}
	b.IdempotencyKey += "-repeat"
	b.Records[0].CheckedAt = "2026-09-20T00:00:00Z"
	duplicate, _, err := ingest(b)
	if err != nil || duplicate.DuplicateCount != 1 || duplicate.InsertedCount != 0 {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	var status string
	if err = store.pool.QueryRow(ctx, `SELECT n.candidate_status FROM collector.normalized_records n JOIN collector.source_records s USING(source_record_id) WHERE s.batch_id=$1::uuid`, first.BatchID).Scan(&status); err != nil || status != "reviewing" {
		t.Fatal("review state overwritten", err)
	}
	b.IdempotencyKey += "-changed"
	b.Records[0].Payload.Name = "Changed synthetic"
	changed, _, err := ingest(b)
	if err != nil || changed.InsertedCount != 1 {
		t.Fatal("new observation missing", err)
	}
	// Concurrent same-key requests must have exactly one first response.
	b.IdempotencyKey += "-concurrent"
	b.Records[0].ExternalID += "-concurrent"
	var wg sync.WaitGroup
	var mu sync.Mutex
	created := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, replay, err := ingest(b)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				t.Error(err)
			} else if !replay {
				created++
			}
		}()
	}
	wg.Wait()
	if created != 1 {
		t.Fatalf("concurrent first responses: %d", created)
	}
	// Distinct batch keys racing on one observation must still insert it only once.
	b.IdempotencyKey += "-overlap"
	b.Records[0].ExternalID += "-overlap"
	inserted, duplicates := 0, 0
	for i := 0; i < 8; i++ {
		batch := b
		batch.IdempotencyKey += fmt.Sprintf("-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, replay, err := ingest(batch)
			mu.Lock()
			defer mu.Unlock()
			if err != nil || replay {
				t.Errorf("overlap: replay %v, error %v", replay, err)
				return
			}
			inserted += receipt.InsertedCount
			duplicates += receipt.DuplicateCount
		}()
	}
	wg.Wait()
	if inserted != 1 || duplicates != 7 {
		t.Fatalf("overlapping batches: inserted %d, duplicates %d", inserted, duplicates)
	}
	// Fail on the second row inside SQL; the first row and receiving batch must roll back.
	b.IdempotencyKey += "-rollback"
	b.Records[0].ExternalID += "-rollback"
	invalid := b.Records[0]
	invalid.ExternalID = ""
	b.Records = append(b.Records, invalid)
	if _, _, err = ingest(b); err == nil {
		t.Fatal("constraint failure expected")
	}
	var count int
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM collector.source_records WHERE external_id=$1`, b.Records[0].ExternalID).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial rows remained", err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM collector.publication_batches WHERE idempotency_key=$1`, "collector.performers.v1:"+b.SourceID+":"+b.IdempotencyKey).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed batch remained", err)
	}
	if err = store.pool.QueryRow(ctx, `SELECT count(*) FROM platform.performers`).Scan(&count); err != nil || count != baseline {
		t.Fatal("formal entities changed", err)
	}
	b.Records = b.Records[:1]
	b.Records[0].Payload.ProfileURL = "https://other.test/person"
	if _, _, err = ingest(b); !errors.Is(err, collectoringest.ErrSource) {
		t.Fatal("foreign profile host accepted", err)
	}
}

func TestPostgresCollectorSourcePolicy(t *testing.T) {
	endpoint := os.Getenv("COLLECTOR_TEST_DATABASE_URL")
	if endpoint == "" {
		t.Skip("COLLECTOR_TEST_DATABASE_URL is not set")
	}
	if !validIdentityContractURL(endpoint) {
		t.Fatal("requires isolated migration test database with restricted role")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := Open(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	prefix := time.Now().Format("150405.000000000")
	b := collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abc", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: "policy-" + prefix, Records: []collectoringest.Record{{ExternalID: "policy-" + prefix, CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: "Synthetic", Aliases: []string{}, ProfileURL: "https://example.test/person", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}}}}
	var batchesBefore, recordsBefore int
	if err := store.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM collector.publication_batches),(SELECT count(*) FROM collector.source_records)`).Scan(&batchesBefore, &recordsBefore); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		name, sourceSuffix, profileURL string
	}{
		{"paused source", "abd", "https://example.test/person"},
		{"blocked source", "abe", "https://example.test/person"},
		{"disabled source", "abf", "https://example.test/person"},
		{"restricted rights", "ac0", "https://example.test/person"},
		{"takedown rights", "ac1", "https://example.test/person"},
		{"fixture source", "ac2", "https://example.test/person"},
		{"manual source", "ac3", "https://example.test/person"},
		{"missing base URL", "ac4", "https://example.test/person"},
		{"unregistered source", "acc", "https://example.test/person"},
		{"foreign host", "abc", "https://other.test/person"},
		{"unregistered subdomain", "abc", "https://sub.example.test/person"},
		{"unregistered port", "abc", "https://example.test:8443/person"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := b
			batch.SourceID = "12345678-1234-1234-1234-123456789" + tc.sourceSuffix
			batch.IdempotencyKey += fmt.Sprintf("-%d", i)
			batch.Records = append([]collectoringest.Record(nil), b.Records...)
			batch.Records[0].Payload.ProfileURL = tc.profileURL
			raw, err := json.Marshal(batch)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = store.IngestPerformers(ctx, batch, collectoringest.Hash(raw)); !errors.Is(err, collectoringest.ErrSource) {
				t.Fatalf("source policy should deny with ErrSource: %v", err)
			}
		})
	}
	var batchesAfter, recordsAfter int
	if err := store.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM collector.publication_batches),(SELECT count(*) FROM collector.source_records)`).Scan(&batchesAfter, &recordsAfter); err != nil || batchesAfter != batchesBefore || recordsAfter != recordsBefore {
		t.Fatalf("source denials wrote data: batches %d -> %d, records %d -> %d, error %v", batchesBefore, batchesAfter, recordsBefore, recordsAfter, err)
	}
	// A source-level permission still cannot approve an individual observation.
	b.SourceID = "12345678-1234-1234-1234-123456789ac5"
	raw, _ := json.Marshal(b)
	receipt, replay, err := store.IngestPerformers(ctx, b, collectoringest.Hash(raw))
	if err != nil || replay || receipt.InsertedCount != 1 {
		t.Fatalf("allowed source: receipt %+v, replay %v, error %v", receipt, replay, err)
	}
	var candidate, rights, publication, adult string
	if err := store.pool.QueryRow(ctx, `SELECT n.candidate_status,s.payload->>'rights_status',s.payload->>'publication_status',s.payload->>'adult_status' FROM collector.source_records s JOIN collector.normalized_records n USING(source_record_id) WHERE s.batch_id=$1::uuid`, receipt.BatchID).Scan(&candidate, &rights, &publication, &adult); err != nil || candidate != "pending" || rights != "needs_review" || publication != "staging" || adult != "unknown" {
		t.Fatalf("source policy bypassed record review: %q %q %q %q, error %v", candidate, rights, publication, adult, err)
	}
}
