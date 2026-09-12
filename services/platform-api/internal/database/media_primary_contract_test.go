package database

import (
	"errors"
	"strings"
	"testing"
	"time"

	"self-deepsearch/services/platform-api/internal/operations"
)

func replacementContractManifest(t *testing.T, harness *invitationContractHarness, old operations.MediaManifestInput) operations.MediaManifestInput {
	t.Helper()
	input := old
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT gen_random_uuid()::text`).Scan(&input.AssetID); err != nil {
		t.Fatal(err)
	}
	input.Objects = append([]operations.MediaObjectInput(nil), old.Objects...)
	for i := range input.Objects {
		object := &input.Objects[i]
		object.StorageKey = strings.ReplaceAll(object.StorageKey, old.AssetID, input.AssetID)
		object.BackupPath = object.StorageKey
		if object.PublicURL != nil {
			url := strings.ReplaceAll(*object.PublicURL, old.AssetID, input.AssetID)
			object.PublicURL = &url
		}
	}
	return input
}

func TestPrimaryReplacementPostgresCASAndRetention(t *testing.T) {
	harness := newInvitationContractHarness(t)
	for _, entityType := range []string{"work", "performer"} {
		t.Run(entityType, func(t *testing.T) {
			old := contractMediaManifest(t, harness, entityType, "draft")
			if _, err := harness.store.RegisterMediaManifest(harness.ctx, old, harness.actorID, "primary-old", contractNow()); err != nil {
				t.Fatal(err)
			}
			newImage := replacementContractManifest(t, harness, old)
			now := contractNow().Add(time.Hour)
			result, err := harness.store.ReplacePrimaryMedia(harness.ctx, operations.ReplacePrimaryMediaInput{ExpectedAssetID: strings.ToUpper(old.AssetID), Manifest: newImage}, harness.actorID, "primary-switch", now)
			if err != nil || !result.OldAssetRetired || result.PrivateRetainedUntil == nil || !result.PrivateRetainedUntil.Equal(now.Add(30*24*time.Hour)) {
				t.Fatalf("replacement: %+v %v", result, err)
			}
			state, err := harness.store.GetPrimaryMedia(harness.ctx, entityType, old.EntityID)
			if err != nil || state.Primary == nil || state.Primary.AssetID != newImage.AssetID || state.EntityStatus != "draft" {
				t.Fatalf("current primary: %+v %v", state, err)
			}
			deletions := deletionSnapshots(t, harness, old.AssetID)
			if len(deletions) != 4 {
				t.Fatalf("missing object deletions: %d", len(deletions))
			}
			for _, event := range deletions {
				due := now
				if event.Payload.StorageScope == "private" {
					due = *result.PrivateRetainedUntil
				}
				if !event.AvailableAt.Equal(due) {
					t.Fatalf("wrong deadline: %+v", event)
				}
			}
			stale := replacementContractManifest(t, harness, old)
			_, err = harness.store.ReplacePrimaryMedia(harness.ctx, operations.ReplacePrimaryMediaInput{ExpectedAssetID: old.AssetID, Manifest: stale}, harness.actorID, "primary-stale", now)
			if !errors.Is(err, operations.ErrConflict) {
				t.Fatalf("stale replacement accepted: %v", err)
			}
			var leaked, audits, caches int
			err = harness.store.pool.QueryRow(harness.ctx, `SELECT
 (SELECT count(*) FROM platform.media_assets WHERE asset_id = $1::uuid),
 (SELECT count(*) FROM audit.audit_logs WHERE object_id = $2::uuid AND action = 'media.primary_replace'),
 (SELECT count(*) FROM platform.outbox_events WHERE dedupe_key = 'media-publish:' || $2::text)`, stale.AssetID, newImage.AssetID).Scan(&leaked, &audits, &caches)
			if err != nil || leaked != 0 || audits != 1 || caches != 1 {
				t.Fatalf("atomic effects: %d/%d/%d %v", leaked, audits, caches, err)
			}
			if err := mediaTakedownContract(t, harness, old.AssetID, now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			for _, event := range deletionSnapshots(t, harness, old.AssetID) {
				if event.Payload.StorageScope == "private" && !event.AvailableAt.Equal(now.Add(time.Hour)) {
					t.Fatal("rights takedown did not accelerate retained master")
				}
			}
		})
	}
}

func TestPrimaryReplacementPostgresSharedAndRollback(t *testing.T) {
	harness := newInvitationContractHarness(t)
	old := registeredMediaContract(t, harness)
	other := contractMediaManifest(t, harness, "work", "draft")
	_, err := harness.store.pool.Exec(harness.ctx, `INSERT INTO platform.entity_media (entity_type, entity_id, asset_id, purpose, position, is_primary, publication_status)
VALUES ('work', $1::uuid, $2::uuid, 'cover', 0, true, 'published')`, other.EntityID, old.AssetID)
	if err != nil {
		t.Fatal(err)
	}
	newImage := replacementContractManifest(t, harness, old)
	result, err := harness.store.ReplacePrimaryMedia(harness.ctx, operations.ReplacePrimaryMediaInput{ExpectedAssetID: old.AssetID, Manifest: newImage}, harness.actorID, "primary-shared", contractNow())
	if err != nil || result.OldAssetRetired || result.PrivateRetainedUntil != nil || len(deletionSnapshots(t, harness, old.AssetID)) != 0 {
		t.Fatalf("shared media retired: %+v %v", result, err)
	}
	var status string
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT asset_status FROM platform.media_assets WHERE asset_id = $1::uuid`, old.AssetID).Scan(&status); err != nil || status != "published" {
		t.Fatalf("shared status: %s %v", status, err)
	}
	// Duplicate immutable ID fails after old-link withdrawal. Rollback must
	// restore the exact new primary and leave retirement/audit unchanged.
	_, err = harness.store.ReplacePrimaryMedia(harness.ctx, operations.ReplacePrimaryMediaInput{ExpectedAssetID: newImage.AssetID, Manifest: old}, harness.actorID, "primary-rollback", contractNow())
	if !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("duplicate asset accepted: %v", err)
	}
	state, err := harness.store.GetPrimaryMedia(harness.ctx, old.EntityType, old.EntityID)
	if err != nil || state.Primary == nil || state.Primary.AssetID != newImage.AssetID || len(deletionSnapshots(t, harness, newImage.AssetID)) != 0 {
		t.Fatalf("old primary lost on rollback: %+v %v", state, err)
	}
}

func TestPrimaryReplacementPostgresConcurrentWriters(t *testing.T) {
	harness := newInvitationContractHarness(t)
	old := registeredMediaContract(t, harness)
	images := []operations.MediaManifestInput{replacementContractManifest(t, harness, old), replacementContractManifest(t, harness, old)}
	results := make(chan error, 2)
	start := make(chan struct{})
	for _, item := range images {
		go func() {
			<-start
			_, err := harness.store.ReplacePrimaryMedia(harness.ctx, operations.ReplacePrimaryMediaInput{ExpectedAssetID: old.AssetID, Manifest: item}, harness.actorID, "primary-race", contractNow())
			results <- err
		}()
	}
	close(start)
	winners, conflicts := 0, 0
	for range images {
		err := <-results
		if err == nil {
			winners++
		} else if errors.Is(err, operations.ErrConflict) {
			conflicts++
		} else {
			t.Fatalf("race error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("race: winners=%d conflicts=%d", winners, conflicts)
	}
}
