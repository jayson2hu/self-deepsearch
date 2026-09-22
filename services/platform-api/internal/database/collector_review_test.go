package database

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/collectoringest"
	"self-deepsearch/services/platform-api/internal/collectorreview"
)

func TestPostgresCollectorReview(t *testing.T) {
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
	prefix := "read-" + time.Now().Format("150405.000000000")
	b := collectoringest.Batch{SchemaVersion: "collector.performers.v1", SourceID: "12345678-1234-1234-1234-123456789abc", Region: "japan", ConnectorVersion: "fixture@v1", IdempotencyKey: prefix}
	for _, name := range []string{"人物甲", "人物乙", "<script>literal</script>"} {
		b.Records = append(b.Records, collectoringest.Record{ExternalID: prefix + name, CheckedAt: "2026-09-19T00:00:00Z", Payload: collectoringest.Payload{Name: name, Aliases: []string{"Alias_100%" + name}, ProfileURL: "https://example.test/person", SourceContentHash: "sha256:" + strings.Repeat("a", 64)}})
	}
	raw, _ := json.Marshal(b)
	receipt, _, err := store.IngestPerformers(ctx, b, collectoringest.Hash(raw))
	if err != nil {
		t.Fatal(err)
	}
	q := collectorreview.Query{BatchID: receipt.BatchID, Limit: 2}
	first, err := store.ListCollectorCandidates(ctx, q)
	if err != nil || first.Total != 3 || len(first.Items) != 2 || first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	q.Offset = 2
	second, err := store.ListCollectorCandidates(ctx, q)
	if err != nil || second.Total != 3 || len(second.Items) != 1 || second.NextOffset != nil {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if first.Items[0].CandidateID == second.Items[0].CandidateID || first.Items[1].CandidateID == second.Items[0].CandidateID {
		t.Fatal("pagination repeated candidate")
	}
	for _, item := range append(first.Items, second.Items...) {
		if item.CandidateStatus != "pending" || item.AdultStatus != "unknown" || item.RightsStatus != "needs_review" || item.PublicationStatus != "staging" {
			t.Fatalf("unsafe state: %+v", item)
		}
		detail, err := store.CollectorCandidate(ctx, item.CandidateID)
		if err != nil || detail.Name != item.Name || detail.ExternalID != item.ExternalID || detail.BatchID != receipt.BatchID || len(detail.Aliases) != 1 {
			t.Fatalf("detail=%+v err=%v", detail, err)
		}
	}
	q.Offset = 0
	q.Search = "Alias_100%人物甲"
	matched, err := store.ListCollectorCandidates(ctx, q)
	if err != nil || matched.Total != 1 || matched.Items[0].Name != "人物甲" {
		t.Fatalf("alias search=%+v err=%v", matched, err)
	}
	q.Search = "_100X" // Percent and underscore are literal source text, not SQL wildcards.
	matched, err = store.ListCollectorCandidates(ctx, q)
	if err != nil || matched.Total != 0 || len(matched.Items) != 0 {
		t.Fatalf("literal search=%+v err=%v", matched, err)
	}
	q.Search = ""
	q.Status = "accepted"
	matched, err = store.ListCollectorCandidates(ctx, q)
	if err != nil || matched.Total != 0 {
		t.Fatalf("status filter=%+v err=%v", matched, err)
	}
	if _, err = store.CollectorCandidate(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, collectorreview.ErrNotFound) {
		t.Fatal("missing candidate", err)
	}
	b.IdempotencyKey += "-duplicate"
	raw, _ = json.Marshal(b)
	duplicate, _, err := store.IngestPerformers(ctx, b, collectoringest.Hash(raw))
	if err != nil {
		t.Fatal(err)
	}
	batches, err := store.ListCollectorBatches(ctx, collectorreview.Query{SourceID: b.SourceID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, batch := range batches.Items {
		if batch.BatchID == receipt.BatchID {
			found++
			if batch.InputCount != 3 || batch.InsertedCount != 3 || batch.DuplicateCount != 0 || batch.CandidateCount != 3 || batch.PendingCount != 3 {
				t.Fatalf("first batch counts: %+v", batch)
			}
		}
		if batch.BatchID == duplicate.BatchID {
			found++
			if batch.InputCount != 3 || batch.InsertedCount != 0 || batch.DuplicateCount != 3 || batch.CandidateCount != 0 || batch.PendingCount != 0 {
				t.Fatalf("duplicate batch counts: %+v", batch)
			}
		}
	}
	if found != 2 {
		t.Fatal("batch visibility incomplete")
	}
}
