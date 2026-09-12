package database

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

type mediaDeletionSnapshot struct {
	ID          string
	Payload     retiredMediaObject
	Status      string
	AvailableAt time.Time
	LeaseOwner  *string
	Attempts    int
}

func registeredMediaContract(t *testing.T, harness *invitationContractHarness) operations.MediaManifestInput {
	t.Helper()
	input := contractMediaManifest(t, harness, "work", "published")
	if _, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, "media-retirement-registration", contractNow()); err != nil {
		t.Fatal(err)
	}
	return input
}

func mediaTakedownContract(t *testing.T, harness *invitationContractHarness, assetID string, now time.Time) error {
	t.Helper()
	requestID := "media-takedown-" + harness.newHash()[:24]
	request, err := harness.store.CreateTakedownRequest(harness.ctx, operations.TakedownInput{
		RequesterReference: "synthetic rights contract", EntityType: "media", EntityID: assetID,
	}, harness.actorID, requestID, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = harness.store.CompleteTakedownRequest(harness.ctx, request.ID, harness.actorID, "synthetic rights confirmed", requestID, now)
	return err
}

func deletionSnapshots(t *testing.T, harness *invitationContractHarness, assetID string) map[string]mediaDeletionSnapshot {
	t.Helper()
	rows, err := harness.store.pool.Query(harness.ctx, `
SELECT event_id::text, payload, status, available_at, lease_owner, attempt_count
FROM platform.outbox_events
WHERE aggregate_id = $1::uuid AND event_type = 'media_delete' AND dedupe_key LIKE 'media-delete:%'`, assetID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := make(map[string]mediaDeletionSnapshot)
	for rows.Next() {
		var snapshot mediaDeletionSnapshot
		var payload []byte
		if err := rows.Scan(&snapshot.ID, &payload, &snapshot.Status, &snapshot.AvailableAt, &snapshot.LeaseOwner, &snapshot.Attempts); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(payload, &snapshot.Payload); err != nil {
			t.Fatal(err)
		}
		if err := validateRetiredMediaObject(snapshot.Payload); err != nil {
			t.Fatalf("persisted invalid deletion payload: %v", err)
		}
		result[snapshot.Payload.StorageKey] = snapshot
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestMediaRetirementPostgresRepeatedTakedownPreservesIdentityAndLease(t *testing.T) {
	harness := newInvitationContractHarness(t)
	input := registeredMediaContract(t, harness)
	now := contractNow()
	if err := mediaTakedownContract(t, harness, input.AssetID, now); err != nil {
		t.Fatal(err)
	}
	before := deletionSnapshots(t, harness, input.AssetID)
	if len(before) != 4 {
		t.Fatalf("expected one canonical deletion per object, got %d", len(before))
	}
	for _, object := range input.Objects {
		item, ok := before[object.StorageKey]
		if !ok || !reflect.DeepEqual(item.Payload.PublicURL, object.PublicURL) {
			t.Fatal("original URL was not saved exactly")
		}
	}
	// Preserve an in-flight lease and an exhausted task as well as pending
	// tasks; a new rights case must not reset retry limits or fabricate success.
	if _, err := harness.store.pool.Exec(harness.ctx, `
UPDATE platform.outbox_events SET status = 'running', lease_owner = 'media-contract',
 lease_expires_at = $2, attempt_count = 1 WHERE event_id = $1::uuid`,
		before[input.Objects[0].StorageKey].ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.pool.Exec(harness.ctx, `
UPDATE platform.outbox_events SET status = 'dead', attempt_count = 10
WHERE event_id = $1::uuid`, before[input.Objects[1].StorageKey].ID); err != nil {
		t.Fatal(err)
	}
	before = deletionSnapshots(t, harness, input.AssetID)
	if err := mediaTakedownContract(t, harness, input.AssetID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	after := deletionSnapshots(t, harness, input.AssetID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("repeat takedown changed payload, timing, attempts or lease")
	}
	var hidden, urls int
	var countedBytes int64
	if err := harness.store.pool.QueryRow(harness.ctx, `
SELECT count(*) FILTER (WHERE object_status = 'hidden'), count(public_url),
       coalesce(sum(byte_size) FILTER (WHERE object_status <> 'deleted'), 0)
FROM platform.media_objects WHERE asset_id = $1::uuid`, input.AssetID).Scan(&hidden, &urls, &countedBytes); err != nil {
		t.Fatal(err)
	}
	if hidden != 4 || urls != 0 || countedBytes != 4800 {
		t.Fatal("takedown failed to hide URLs or prematurely released capacity")
	}
}

func TestMediaRetirementPostgresRightsAcceleratesPrivateRetention(t *testing.T) {
	harness := newInvitationContractHarness(t)
	input := registeredMediaContract(t, harness)
	now := contractNow()
	deadline := now.Add(30 * 24 * time.Hour)
	tx, err := harness.store.pool.BeginTx(harness.ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(harness.ctx) }()
	// Model only the retirement half of a future primary replacement. This
	// does not claim that the primary-switch API or UI has been implemented.
	if _, err := tx.Exec(harness.ctx, `UPDATE platform.media_assets SET asset_status = 'hidden' WHERE asset_id = $1::uuid`, input.AssetID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(harness.ctx, `UPDATE platform.entity_media SET publication_status = 'hidden' WHERE asset_id = $1::uuid`, input.AssetID); err != nil {
		t.Fatal(err)
	}
	if err := retireMediaObjects(harness.ctx, tx, []string{input.AssetID}, deadline, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(harness.ctx); err != nil {
		t.Fatal(err)
	}
	before := deletionSnapshots(t, harness, input.AssetID)
	if len(before) != 4 {
		t.Fatal("retirement did not queue every object")
	}
	for _, item := range before {
		want := now
		if item.Payload.StorageScope == "private" {
			want = deadline
		}
		if !item.AvailableAt.Equal(want) {
			t.Fatal("incorrect retention deadline")
		}
	}
	rightsTime := now.Add(time.Second)
	if err := mediaTakedownContract(t, harness, input.AssetID, rightsTime); err != nil {
		t.Fatal(err)
	}
	after := deletionSnapshots(t, harness, input.AssetID)
	if len(after) != 4 {
		t.Fatal("rights escalation created duplicate object tasks")
	}
	for key, item := range after {
		want := now
		if item.Payload.StorageScope == "private" {
			want = rightsTime
		}
		if item.ID != before[key].ID || !reflect.DeepEqual(item.Payload, before[key].Payload) || !item.AvailableAt.Equal(want) {
			t.Fatal("rights escalation delayed work or lost original identity")
		}
	}
}

func TestMediaRetirementPostgresRecoversLegacyRequestScopedURL(t *testing.T) {
	harness := newInvitationContractHarness(t)
	input := registeredMediaContract(t, harness)
	now := contractNow()
	if err := mediaTakedownContract(t, harness, input.AssetID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.store.pool.Exec(harness.ctx, `
UPDATE platform.outbox_events SET dedupe_key = 'legacy-takedown:' || event_id::text
WHERE aggregate_id = $1::uuid AND event_type = 'media_delete'`, input.AssetID); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := mediaTakedownContract(t, harness, input.AssetID, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	canonical := deletionSnapshots(t, harness, input.AssetID)
	if len(canonical) != 4 {
		t.Fatal("legacy URL recovery did not create stable per-object tasks")
	}
	for _, object := range input.Objects {
		if !reflect.DeepEqual(canonical[object.StorageKey].Payload.PublicURL, object.PublicURL) {
			t.Fatal("legacy task URL lost")
		}
	}
	var total int
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.outbox_events WHERE aggregate_id = $1::uuid AND event_type = 'media_delete'`, input.AssetID).Scan(&total); err != nil || total != 8 {
		t.Fatalf("legacy evidence was removed or repeated new cases created more tasks: %d, %v", total, err)
	}
}

func TestMediaRetirementPostgresMissingIdentityRollsBackTakedown(t *testing.T) {
	harness := newInvitationContractHarness(t)
	input := registeredMediaContract(t, harness)
	// Simulate corrupt historical metadata with neither a current URL nor any
	// preserved deletion payload. Do not guess a host or use a source page URL.
	if _, err := harness.store.pool.Exec(harness.ctx, `UPDATE platform.media_objects SET public_url = NULL WHERE asset_id = $1::uuid AND storage_scope = 'public'`, input.AssetID); err != nil {
		t.Fatal(err)
	}
	if err := mediaTakedownContract(t, harness, input.AssetID, contractNow()); err == nil {
		t.Fatal("incomplete deletion identity was accepted")
	}
	if len(deletionSnapshots(t, harness, input.AssetID)) != 0 {
		t.Fatal("failed takedown left partial canonical tasks")
	}
	var status string
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT asset_status FROM platform.media_assets WHERE asset_id = $1::uuid`, input.AssetID).Scan(&status); err != nil || status != "published" {
		t.Fatalf("failed takedown did not roll back asset state: %s, %v", status, err)
	}
}

func TestMediaRetirementPostgresWithdrawsPreparedDraftMedia(t *testing.T) {
	harness := newInvitationContractHarness(t)
	for _, entityType := range []string{"work", "performer"} {
		input := contractMediaManifest(t, harness, entityType, "draft")
		now := contractNow()
		if _, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, "draft-media-contract", now); err != nil {
			t.Fatal(err)
		}
		request, err := harness.store.CreateTakedownRequest(harness.ctx, operations.TakedownInput{
			RequesterReference: "synthetic draft rights contract", EntityType: entityType, EntityID: input.EntityID,
		}, harness.actorID, "draft-rights-contract", now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := harness.store.CompleteTakedownRequest(harness.ctx, request.ID, harness.actorID, "withdraw prepared draft", "draft-rights-contract", now); err != nil {
			t.Fatal(err)
		}
		if len(deletionSnapshots(t, harness, input.AssetID)) != 4 {
			t.Fatal("draft media was not queued for deletion")
		}
		var count int
		if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.publications WHERE entity_type = $1 AND entity_id = $2::uuid`, entityType, input.EntityID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("draft takedown fabricated a publication record: count=%d, %v", count, err)
		}
	}
}
