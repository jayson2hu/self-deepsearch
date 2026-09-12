package database

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/operations"
)

// Opt-in SQL contracts use the same restricted, disposable loopback database
// as the identity contracts. All media here are synthetic metadata: no object
// store requests or fixture downloads are made.
func contractMediaManifest(t *testing.T, harness *invitationContractHarness, entityType, status string) operations.MediaManifestInput {
	t.Helper()
	var entityID, assetID string
	label := "media-" + harness.newHash()[:24]
	var err error
	if entityType == "performer" {
		err = harness.store.pool.QueryRow(harness.ctx, `
INSERT INTO platform.performers (display_name, canonical_slug, adult_status, publication_status)
VALUES ($1, $1, 'verified', $2) RETURNING performer_id::text`, label, status).Scan(&entityID)
	} else {
		err = harness.store.pool.QueryRow(harness.ctx, `
INSERT INTO platform.works (canonical_code, compact_code, title, publication_status)
VALUES ($1, $2, 'Synthetic media contract', $3) RETURNING work_id::text`,
			label, strings.ToUpper(strings.ReplaceAll(label, "-", "")), status).Scan(&entityID)
	}
	if err != nil {
		t.Fatalf("insert synthetic media parent: %v", err)
	}
	if err = harness.store.pool.QueryRow(harness.ctx, `SELECT gen_random_uuid()::text`).Scan(&assetID); err != nil {
		t.Fatal(err)
	}
	input := operations.MediaManifestInput{
		AssetID: assetID, EntityType: entityType, EntityID: entityID, AssetType: "work_image",
		SourceType: "manual", CheckedAt: contractNow(), Confidence: 1,
		Purpose: "cover", IsPrimary: true, Reason: "Synthetic registration contract", ToolVersion: "media-python/1",
	}
	if entityType == "performer" {
		input.AssetType, input.Purpose = "performer_avatar", "avatar"
	}
	for _, width := range []int{0, 320, 640, 960} {
		object := operations.MediaObjectInput{
			Rendition: "master", StorageScope: "private", SHA256: harness.newHash(),
			MimeType: "image/webp", Width: 960, Height: 600, ByteSize: 1200,
			StorageKey: fmt.Sprintf("media-master/%s/v1/master.webp", assetID),
		}
		if width != 0 {
			object.Rendition, object.StorageScope = fmt.Sprintf("w%d", width), "public"
			object.Width, object.Height = width, width*5/8
			object.StorageKey = fmt.Sprintf("media-public/default/%ss/%s/%s/v1/%s-w%d.webp", entityType, entityID, assetID, input.Purpose, width)
			publicURL := "https://media.example.test/" + object.StorageKey
			object.PublicURL = &publicURL
		}
		object.BackupPath = object.StorageKey
		input.Objects = append(input.Objects, object)
	}
	return input
}

func TestMediaRegistrationPostgresDurableRefresh(t *testing.T) {
	harness := newInvitationContractHarness(t)
	for _, entityType := range []string{"work", "performer"} {
		for _, status := range []string{"draft", "reviewing", "approved", "published", "hidden"} {
			t.Run(entityType+"/"+status, func(t *testing.T) {
				input := contractMediaManifest(t, harness, entityType, status)
				requestID := "media-contract-" + harness.newHash()[:24]
				result, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, requestID, contractNow())
				if err != nil || result.AssetID != input.AssetID || result.ObjectCount != 4 {
					t.Fatalf("media registration failed: %v", err)
				}
				// Retrying an immutable manifest is a conflict, not another asset,
				// audit entry or independently scheduled cache refresh.
				if _, err = harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, requestID, contractNow()); !errors.Is(err, operations.ErrConflict) {
					t.Fatalf("duplicate manifest should conflict: %v", err)
				}
				var objects, links, events, audits int
				err = harness.store.pool.QueryRow(harness.ctx, `
SELECT
 (SELECT count(*) FROM platform.media_objects WHERE asset_id = $1::uuid),
 (SELECT count(*) FROM platform.entity_media WHERE asset_id = $1::uuid AND entity_id = $2::uuid),
 (SELECT count(*) FROM platform.outbox_events
  WHERE event_type = 'cache_purge' AND dedupe_key = 'media-publish:' || $1::text
    AND aggregate_type = $3 AND aggregate_id = $2::uuid
    AND payload->>'entity_type' = $3 AND payload->>'entity_id' = $2::text AND payload->>'asset_id' = $1::text),
 (SELECT count(*) FROM audit.audit_logs WHERE object_id = $1::uuid AND action = 'media.manifest_publish')`,
					input.AssetID, input.EntityID, input.EntityType).Scan(&objects, &links, &events, &audits)
				if err != nil || objects != 4 || links != 1 || events != 1 || audits != 1 {
					t.Fatalf("unexpected persisted media/event/audit: %d/%d/%d/%d, %v", objects, links, events, audits, err)
				}
			})
		}
	}
}

func TestMediaRegistrationPostgresRejectsWithdrawnParents(t *testing.T) {
	harness := newInvitationContractHarness(t)
	for _, entityType := range []string{"work", "performer"} {
		for _, status := range []string{"takedown", "merged"} {
			t.Run(entityType+"/"+status, func(t *testing.T) {
				input := contractMediaManifest(t, harness, entityType, status)
				_, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, "media-rejected-contract", contractNow())
				if !errors.Is(err, operations.ErrConflict) {
					t.Fatalf("withdrawn parent accepted media: %v", err)
				}
				var count int
				err = harness.store.pool.QueryRow(harness.ctx, `
SELECT (SELECT count(*) FROM platform.media_assets WHERE asset_id = $1::uuid)
     + (SELECT count(*) FROM platform.entity_media WHERE asset_id = $1::uuid)
     + (SELECT count(*) FROM platform.outbox_events WHERE payload->>'asset_id' = $1::text)
     + (SELECT count(*) FROM audit.audit_logs WHERE object_id = $1::uuid)`, input.AssetID).Scan(&count)
				if err != nil || count != 0 {
					t.Fatalf("rejected media left durable effects: count=%d, %v", count, err)
				}
			})
		}
	}
}

func TestMediaRegistrationPostgresGalleryRequiresPrimaryAndStopsAtThree(t *testing.T) {
	harness := newInvitationContractHarness(t)
	primary := contractMediaManifest(t, harness, "work", "draft")
	if _, err := harness.store.RegisterMediaManifest(harness.ctx, primary, harness.actorID, "gallery-primary", contractNow()); err != nil {
		t.Fatal(err)
	}
	gallery := func(position int) operations.MediaManifestInput {
		input := replacementContractManifest(t, harness, primary)
		input.Purpose, input.Position, input.IsPrimary = "gallery", position, false
		for index := range input.Objects {
			object := &input.Objects[index]
			object.StorageKey = strings.Replace(object.StorageKey, "/cover-", "/gallery-", 1)
			object.BackupPath = object.StorageKey
			if object.PublicURL != nil {
				url := strings.Replace(*object.PublicURL, "/cover-", "/gallery-", 1)
				object.PublicURL = &url
			}
		}
		return input
	}
	for position := 1; position <= 3; position++ {
		input := gallery(position)
		if _, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, fmt.Sprintf("gallery-%d", position), contractNow()); err != nil {
			t.Fatalf("register gallery %d: %v", position, err)
		}
		if position == 1 {
			duplicate := gallery(position)
			if _, err := harness.store.RegisterMediaManifest(harness.ctx, duplicate, harness.actorID, "gallery-duplicate-position", contractNow()); !errors.Is(err, operations.ErrConflict) {
				t.Fatalf("duplicate gallery position should conflict: %v", err)
			}
			var leaked int
			if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.media_assets WHERE asset_id = $1::uuid`, duplicate.AssetID).Scan(&leaked); err != nil || leaked != 0 {
				t.Fatalf("rejected duplicate gallery position leaked an asset: count=%d err=%v", leaked, err)
			}
		}
	}
	fourth := gallery(3)
	if _, err := harness.store.RegisterMediaManifest(harness.ctx, fourth, harness.actorID, "gallery-fourth", contractNow()); !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("fourth gallery should conflict: %v", err)
	}
	var leaked int
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.media_assets WHERE asset_id = $1::uuid`, fourth.AssetID).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("rejected fourth gallery leaked an asset: count=%d err=%v", leaked, err)
	}

	withoutPrimary := contractMediaManifest(t, harness, "work", "draft")
	withoutPrimary.Purpose, withoutPrimary.Position, withoutPrimary.IsPrimary = "gallery", 1, false
	for index := range withoutPrimary.Objects {
		object := &withoutPrimary.Objects[index]
		object.StorageKey = strings.Replace(object.StorageKey, "/cover-", "/gallery-", 1)
		object.BackupPath = object.StorageKey
		if object.PublicURL != nil {
			url := strings.Replace(*object.PublicURL, "/cover-", "/gallery-", 1)
			object.PublicURL = &url
		}
	}
	if _, err := harness.store.RegisterMediaManifest(harness.ctx, withoutPrimary, harness.actorID, "gallery-no-primary", contractNow()); !errors.Is(err, operations.ErrConflict) {
		t.Fatalf("gallery without a primary should conflict: %v", err)
	}
}
