package database

import (
	"errors"
	"strings"
	"testing"

	"self-deepsearch/services/platform-api/internal/catalog"
	"self-deepsearch/services/platform-api/internal/operations"
)

// These fixtures use the existing restricted, disposable PostgreSQL harness.
// They publish synthetic metadata only and never access the image URLs.
func publishedMediaReadContract(t *testing.T, harness *invitationContractHarness, entityType string) (operations.MediaManifestInput, string) {
	t.Helper()
	input := contractMediaManifest(t, harness, entityType, "published")
	if _, err := harness.store.RegisterMediaManifest(harness.ctx, input, harness.actorID, "media-read-register", contractNow()); err != nil {
		t.Fatal(err)
	}
	var slug string
	query := `SELECT lower(canonical_code) FROM platform.works WHERE work_id = $1::uuid`
	if entityType == "performer" {
		query = `SELECT canonical_slug FROM platform.performers WHERE performer_id = $1::uuid`
	}
	if err := harness.store.pool.QueryRow(harness.ctx, query, input.EntityID).Scan(&slug); err != nil {
		t.Fatal(err)
	}
	reviewerID := harness.insertUser(t, harness.newEmail("media-reviewer"), "admin", "active")
	_, err := harness.store.pool.Exec(harness.ctx, `
WITH revision AS (
 INSERT INTO platform.content_revisions (
  entity_type, entity_id, version, payload, revision_status, author_id,
  reviewer_id, reason, submitted_at, reviewed_at
 ) VALUES ($1, $2::uuid, 1, '{}'::jsonb, 'approved', $3::uuid,
           $4::uuid, 'Synthetic media read contract', now(), now())
 RETURNING revision_id
)
INSERT INTO platform.publications (
 entity_type, entity_id, current_revision_id, publication_status, canonical_slug, published_at
)
SELECT $1, $2::uuid, revision_id, 'published', $5, now() FROM revision`,
		entityType, input.EntityID, harness.actorID, reviewerID, slug)
	if err != nil {
		t.Fatalf("publish synthetic media parent: %v", err)
	}
	return input, slug
}

type mediaReadSlot struct {
	name     string
	purpose  string
	position int
	primary  bool
}

func invalidPrimaryReadSlots(entityType, purpose string) []mediaReadSlot {
	items := []mediaReadSlot{
		{name: "nonzero-position", purpose: purpose, position: 1, primary: true},
		{name: "not-primary", purpose: purpose, position: 0, primary: false},
	}
	// The schema forbids cross-entity purposes, but a work gallery can have
	// the wrong primary flag and still satisfy the historical DB constraints.
	if entityType == "work" {
		items = append(items,
			mediaReadSlot{name: "gallery-as-primary", purpose: "gallery", position: 0, primary: true},
			mediaReadSlot{name: "gallery-without-cover", purpose: "gallery", position: 1, primary: false},
		)
	}
	return items
}

func setMediaReadSlot(t *testing.T, harness *invitationContractHarness, input operations.MediaManifestInput, slot mediaReadSlot) {
	t.Helper()
	command, err := harness.store.pool.Exec(harness.ctx, `
UPDATE platform.entity_media SET purpose = $3, position = $4, is_primary = $5
WHERE entity_id = $1::uuid AND asset_id = $2::uuid AND publication_status = 'published'`,
		input.EntityID, input.AssetID, slot.purpose, slot.position, slot.primary)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("change exactly one synthetic media slot: rows=%d err=%v", command.RowsAffected(), err)
	}
}

func assertMediaReadURL(t *testing.T, label string, actual *string, manifest operations.MediaManifestInput, rendition string, visible bool) {
	t.Helper()
	if !visible {
		if actual != nil {
			t.Fatalf("%s exposed an invalid primary URL: %s", label, *actual)
		}
		return
	}
	for _, object := range manifest.Objects {
		if object.Rendition == rendition && object.PublicURL != nil {
			if actual == nil || *actual != *object.PublicURL {
				t.Fatalf("%s did not select the expected %s rendition: %v", label, rendition, actual)
			}
			return
		}
	}
	t.Fatalf("fixture has no %s rendition", rendition)
}

func assertMediaReadSurfaces(t *testing.T, harness *invitationContractHarness, input operations.MediaManifestInput, slug string, visible bool) {
	t.Helper()
	// A negative case is meaningful only if the parent and all three public
	// derivatives are still visible through the view before slot filtering.
	var publicRows int
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.public_entity_media WHERE entity_id = $1::uuid AND asset_id = $2::uuid`, input.EntityID, input.AssetID).Scan(&publicRows); err != nil || publicRows != 3 {
		t.Fatalf("fixture lost public media before read guards: rows=%d err=%v", publicRows, err)
	}
	var detailImage *catalog.Image
	var detailImages []catalog.Image
	preferred := "w640"
	if input.EntityType == "work" {
		items, err := harness.store.ListFavorites(harness.ctx, harness.actorID, 10)
		if err != nil || len(items) != 1 || items[0].ID != input.EntityID {
			t.Fatalf("work summary disappeared or duplicated: %+v %v", items, err)
		}
		assertMediaReadURL(t, "work summary", items[0].ImageURL, input, preferred, visible)
		if (items[0].Image != nil) != visible {
			t.Fatal("work summary structured image disagrees with primary visibility")
		}
		detail, err := harness.store.WorkBySlug(harness.ctx, slug)
		if err != nil || detail.ID != input.EntityID {
			t.Fatalf("work detail disappeared: %+v %v", detail, err)
		}
		assertMediaReadURL(t, "work detail", detail.ImageURL, input, preferred, visible)
		detailImage, detailImages = detail.Image, detail.Images
	} else {
		preferred = "w320"
		items, err := harness.store.ListFollows(harness.ctx, harness.actorID, 10)
		if err != nil || len(items) != 1 || items[0].ID != input.EntityID {
			t.Fatalf("followed performer disappeared or duplicated: %+v %v", items, err)
		}
		assertMediaReadURL(t, "followed performer", items[0].ImageURL, input, preferred, visible)
		detail, err := harness.store.PerformerBySlug(harness.ctx, slug)
		if err != nil || detail.ID != input.EntityID {
			t.Fatalf("performer detail disappeared: %+v %v", detail, err)
		}
		assertMediaReadURL(t, "performer summary/detail", detail.ImageURL, input, preferred, visible)
		detailImage, detailImages = detail.Image, detail.Images
	}
	expectedImages := 0
	if visible {
		expectedImages = 1
	}
	if (detailImage != nil) != visible || len(detailImages) != expectedImages {
		t.Fatalf("detail primary/gallery visibility mismatch: image=%v gallery=%d", detailImage != nil, len(detailImages))
	}
	history, err := harness.store.ListHistory(harness.ctx, harness.actorID, 10)
	if err != nil || len(history) != 1 || history[0].ID != input.EntityID || history[0].ContentType != input.EntityType {
		t.Fatalf("history row disappeared or duplicated: %+v %v", history, err)
	}
	assertMediaReadURL(t, "history", history[0].ImageURL, input, preferred, visible)
	state, err := harness.store.GetPrimaryMedia(harness.ctx, input.EntityType, input.EntityID)
	if err != nil || state.EntityID != input.EntityID || state.EntityStatus != "published" || (state.Primary != nil) != visible {
		t.Fatalf("admin primary visibility mismatch: %+v %v", state, err)
	}
	if state.Primary != nil && state.Primary.AssetID != input.AssetID {
		t.Fatal("admin returned a different primary asset")
	}
}

func TestMediaReadsPostgresPrimarySlots(t *testing.T) {
	for _, entityType := range []string{"work", "performer"} {
		t.Run(entityType, func(t *testing.T) {
			harness := newInvitationContractHarness(t)
			input, slug := publishedMediaReadContract(t, harness, entityType)
			now := contractNow()
			var err error
			if entityType == "work" {
				err = harness.store.SetFavorite(harness.ctx, harness.actorID, input.EntityID, true, now)
			} else {
				err = harness.store.SetFollow(harness.ctx, harness.actorID, input.EntityID, true, now)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := harness.store.RecordHistory(harness.ctx, harness.actorID, entityType, input.EntityID, now); err != nil {
				t.Fatal(err)
			}
			assertMediaReadSurfaces(t, harness, input, slug, true)
			for _, slot := range invalidPrimaryReadSlots(entityType, input.Purpose) {
				t.Run(slot.name, func(t *testing.T) {
					setMediaReadSlot(t, harness, input, slot)
					assertMediaReadSurfaces(t, harness, input, slug, false)
					setMediaReadSlot(t, harness, input, mediaReadSlot{purpose: input.Purpose, primary: true})
					assertMediaReadSurfaces(t, harness, input, slug, true)
				})
			}
		})
	}
}

func TestPrimaryReplacementPostgresRejectsInvalidCurrentSlot(t *testing.T) {
	for _, entityType := range []string{"work", "performer"} {
		purpose := "cover"
		if entityType == "performer" {
			purpose = "avatar"
		}
		for _, slot := range invalidPrimaryReadSlots(entityType, purpose) {
			t.Run(entityType+"/"+slot.name, func(t *testing.T) {
				harness := newInvitationContractHarness(t)
				old := contractMediaManifest(t, harness, entityType, "draft")
				if _, err := harness.store.RegisterMediaManifest(harness.ctx, old, harness.actorID, "invalid-primary-old", contractNow()); err != nil {
					t.Fatal(err)
				}
				setMediaReadSlot(t, harness, old, slot)
				oldState := func() string {
					t.Helper()
					var snapshot string
					err := harness.store.pool.QueryRow(harness.ctx, `SELECT jsonb_build_object(
 'asset', (SELECT to_jsonb(asset) FROM platform.media_assets asset WHERE asset_id = $1::uuid),
 'objects', (SELECT jsonb_agg(to_jsonb(object) ORDER BY rendition) FROM platform.media_objects object WHERE asset_id = $1::uuid),
 'links', (SELECT jsonb_agg(to_jsonb(link) ORDER BY entity_media_id) FROM platform.entity_media link WHERE asset_id = $1::uuid),
 'deletions', (SELECT count(*) FROM platform.outbox_events WHERE aggregate_id = $1::uuid AND event_type = 'media_delete')
)::text`, old.AssetID).Scan(&snapshot)
					if err != nil {
						t.Fatalf("snapshot old asset state: %v", err)
					}
					return snapshot
				}
				before := oldState()
				next := replacementContractManifest(t, harness, old)
				request := operations.ReplacePrimaryMediaInput{ExpectedAssetID: old.AssetID, Manifest: next}
				if _, err := harness.store.ReplacePrimaryMedia(harness.ctx, request, harness.actorID, "invalid-primary-rejected", contractNow()); !errors.Is(err, operations.ErrConflict) {
					t.Fatalf("invalid current slot replacement should conflict: %v", err)
				}
				if oldState() != before {
					t.Fatal("rejected replacement changed the old asset, objects, links or deletion queue")
				}
				var leaked, oldLinks int
				err := harness.store.pool.QueryRow(harness.ctx, `SELECT
 (SELECT count(*) FROM platform.media_assets WHERE asset_id = $1::uuid)
 + (SELECT count(*) FROM platform.media_objects WHERE asset_id = $1::uuid)
 + (SELECT count(*) FROM platform.entity_media WHERE asset_id = $1::uuid)
 + (SELECT count(*) FROM audit.audit_logs WHERE object_id = $1::uuid)
 + (SELECT count(*) FROM platform.outbox_events WHERE payload->>'asset_id' = $1::text),
 (SELECT count(*) FROM platform.entity_media WHERE asset_id = $2::uuid AND publication_status = 'published')`, next.AssetID, old.AssetID).Scan(&leaked, &oldLinks)
				if err != nil || leaked != 0 || oldLinks != 1 {
					t.Fatalf("rejected replacement changed durable state: leaked=%d oldLinks=%d err=%v", leaked, oldLinks, err)
				}
				// Restoring the fixture proves the rejection was the current-slot
				// guard, rather than an unusable manifest, parent or account.
				setMediaReadSlot(t, harness, old, mediaReadSlot{purpose: old.Purpose, primary: true})
				if _, err := harness.store.ReplacePrimaryMedia(harness.ctx, request, harness.actorID, "invalid-primary-recovered", contractNow()); err != nil {
					t.Fatalf("same manifest failed after restoring a valid slot: %v", err)
				}
				state, err := harness.store.GetPrimaryMedia(harness.ctx, entityType, old.EntityID)
				if err != nil || state.Primary == nil || state.Primary.AssetID != next.AssetID {
					t.Fatalf("replacement did not publish the new primary: %+v %v", state, err)
				}
			})
		}
	}
}

func TestMediaReadsPostgresGalleryOrderAndMissingCover(t *testing.T) {
	harness := newInvitationContractHarness(t)
	cover, slug := publishedMediaReadContract(t, harness, "work")
	expected := make([]operations.MediaManifestInput, 4)
	expected[0] = cover
	for _, position := range []int{3, 1, 2} {
		gallery := replacementContractManifest(t, harness, cover)
		gallery.Purpose, gallery.Position, gallery.IsPrimary = "gallery", position, false
		for index := range gallery.Objects {
			object := &gallery.Objects[index]
			object.StorageKey = strings.Replace(object.StorageKey, "/cover-", "/gallery-", 1)
			object.BackupPath = object.StorageKey
			if object.PublicURL != nil {
				url := strings.Replace(*object.PublicURL, "/cover-", "/gallery-", 1)
				object.PublicURL = &url
			}
		}
		if _, err := harness.store.RegisterMediaManifest(harness.ctx, gallery, harness.actorID, "gallery-order", contractNow()); err != nil {
			t.Fatal(err)
		}
		expected[position] = gallery
	}
	assertGallery := func() {
		t.Helper()
		detail, err := harness.store.WorkBySlug(harness.ctx, slug)
		if err != nil || len(detail.Images) != 4 {
			t.Fatalf("expected one cover and three logical gallery assets: images=%d err=%v", len(detail.Images), err)
		}
		assertMediaReadURL(t, "cover", detail.ImageURL, cover, "w640", true)
		for position, image := range detail.Images {
			assertMediaReadURL(t, "ordered gallery", &image.URL, expected[position], "w640", true)
			if len(image.Renditions) != 3 {
				t.Fatalf("position %d lost or duplicated renditions: %+v", position, image.Renditions)
			}
			for index, name := range []string{"w320", "w640", "w960"} {
				rendition := image.Renditions[index]
				if rendition.Rendition != name {
					t.Fatalf("position %d has wrong rendition order", position)
				}
				assertMediaReadURL(t, "gallery rendition", &rendition.URL, expected[position], name, true)
			}
		}
	}
	assertGallery()
	command, err := harness.store.pool.Exec(harness.ctx, `UPDATE platform.entity_media SET publication_status = 'hidden' WHERE asset_id = $1::uuid`, cover.AssetID)
	if err != nil || command.RowsAffected() != 1 {
		t.Fatalf("hide the synthetic cover link: %v", err)
	}
	var galleryRows int
	if err := harness.store.pool.QueryRow(harness.ctx, `SELECT count(*) FROM platform.public_entity_media WHERE entity_id = $1::uuid`, cover.EntityID).Scan(&galleryRows); err != nil || galleryRows != 9 {
		t.Fatalf("gallery derivatives must remain public before filtering: rows=%d err=%v", galleryRows, err)
	}
	detail, err := harness.store.WorkBySlug(harness.ctx, slug)
	if err != nil || detail.ID != cover.EntityID || detail.Image != nil || detail.ImageURL != nil || len(detail.Images) != 0 {
		t.Fatalf("gallery-only work promoted an image or disappeared: %+v %v", detail, err)
	}
	if _, err := harness.store.pool.Exec(harness.ctx, `UPDATE platform.entity_media SET publication_status = 'published' WHERE asset_id = $1::uuid`, cover.AssetID); err != nil {
		t.Fatal(err)
	}
	assertGallery()
}
