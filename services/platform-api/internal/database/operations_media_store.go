package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) RegisterMediaManifest(ctx context.Context, input operations.MediaManifestInput, actorID, requestID string, now time.Time) (operations.MediaManifest, error) {
	return registerMediaManifest(ctx, store.pool.BeginTx, input, actorID, requestID, now)
}

func registerMediaManifest(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), input operations.MediaManifestInput, actorID, requestID string, now time.Time) (operations.MediaManifest, error) {
	normalizeMediaPreviewPolicy(&input)
	if !validMediaAssociation(input) {
		return operations.MediaManifest{}, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.MediaManifest{}, fmt.Errorf("begin media manifest: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMediaParent(ctx, tx, input); err != nil {
		return operations.MediaManifest{}, err
	}
	result, err := insertMediaManifest(ctx, tx, input, actorID, requestID, now)
	if err != nil {
		return operations.MediaManifest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.MediaManifest{}, fmt.Errorf("commit media manifest: %w", err)
	}
	return result, nil
}

func lockMediaParent(ctx context.Context, tx pgx.Tx, input operations.MediaManifestInput) error {
	if err := lockEntity(ctx, tx, input.EntityType, input.EntityID); err != nil {
		return err
	}
	status, err := readEntityStatus(ctx, tx, input.EntityType, input.EntityID)
	if err != nil {
		return err
	}
	switch status {
	case "draft", "reviewing", "approved", "published", "hidden":
		// Preparation before publication remains supported. A withdrawn or
		// merged identity must never acquire new public media associations.
	default:
		return operations.ErrConflict
	}
	return nil
}

// Caller owns the transaction and parent lock. Adding a manifest never hides
// an existing primary; replacement must explicitly do that before this insert.

func insertMediaManifest(ctx context.Context, tx pgx.Tx, input operations.MediaManifestInput, actorID, requestID string, now time.Time) (operations.MediaManifest, error) {
	normalizeMediaPreviewPolicy(&input)
	if !validMediaAssociation(input) {
		return operations.MediaManifest{}, operations.ErrInvalidInput
	}
	if input.Purpose == "gallery" {
		var primaryExists bool
		var galleryCount int
		var positionOccupied bool
		if err := tx.QueryRow(ctx, `
SELECT
  EXISTS (
    SELECT 1 FROM platform.entity_media
    WHERE entity_type = $1 AND entity_id = $2::uuid
      AND purpose = 'cover' AND is_primary AND position = 0
      AND publication_status = 'published'
  ),
  count(*) FILTER (
    WHERE purpose = 'gallery' AND NOT is_primary AND position BETWEEN 1 AND 3
  ),
  coalesce(bool_or(
    purpose = 'gallery' AND NOT is_primary AND position = $3
  ), false)
FROM platform.entity_media
WHERE entity_type = $1 AND entity_id = $2::uuid AND publication_status = 'published'`,
			input.EntityType, input.EntityID, input.Position).Scan(&primaryExists, &galleryCount, &positionOccupied); err != nil {
			return operations.MediaManifest{}, fmt.Errorf("read media display slots: %w", err)
		}
		if !primaryExists || galleryCount >= 3 || positionOccupied {
			return operations.MediaManifest{}, operations.ErrConflict
		}
	}
	manifestJSON, err := json.Marshal(input)
	if err != nil {
		return operations.MediaManifest{}, operations.ErrInvalidInput
	}
	result := operations.MediaManifest{Version: 1, ObjectCount: len(input.Objects), Status: "published", CreatedAt: now}
	err = tx.QueryRow(ctx, `
INSERT INTO platform.media_assets (
    asset_id, asset_type, current_version, source_type, source_url, checked_at, operator_id,
    confidence, rights_status, asset_status, content_classification, external_preview_permission,
    preview_reviewed_at, preview_reviewed_by
) VALUES ($1::uuid, $2, 1, $3, $4, $5, $6::uuid, $7, 'allowed', 'published', $8, $9,
          CASE WHEN $8::text = 'unknown' THEN NULL::timestamptz ELSE $10::timestamptz END,
          CASE WHEN $8 = 'unknown' THEN NULL ELSE $6::uuid END)
RETURNING asset_id::text, created_at`, input.AssetID, input.AssetType, input.SourceType, input.SourceURL,
		input.CheckedAt, actorID, input.Confidence, input.ContentClassification, input.ExternalPreviewPermission, now).Scan(&result.AssetID, &result.CreatedAt)
	if err != nil {
		return operations.MediaManifest{}, mapConstraintError(err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.media_revisions (asset_id, version, editor_id, tool_version, reason, manifest)
VALUES ($1::uuid, 1, $2::uuid, $3, $4, $5::jsonb)`, result.AssetID, actorID, input.ToolVersion, input.Reason, manifestJSON); err != nil {
		return operations.MediaManifest{}, fmt.Errorf("create media revision: %w", err)
	}
	objectIDs := make(map[string]string, len(input.Objects))
	for _, object := range input.Objects {
		objectStatus := "published"
		if object.Rendition == "master" {
			objectStatus = "ready"
		}
		var objectID string
		if err := tx.QueryRow(ctx, `
INSERT INTO platform.media_objects (
    asset_id, version, rendition, storage_provider, storage_scope, storage_key, backup_path, public_url,
    sha256, mime_type, width, height, byte_size, object_status
) VALUES ($1::uuid, 1, $2, 's3', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING media_object_id::text`,
			result.AssetID, object.Rendition, object.StorageScope, object.StorageKey, object.BackupPath, object.PublicURL,
			object.SHA256, object.MimeType, object.Width, object.Height, object.ByteSize, objectStatus).Scan(&objectID); err != nil {
			return operations.MediaManifest{}, mapConstraintError(err)
		}
		objectIDs[object.Rendition] = objectID
	}
	for _, rendition := range []string{"w320", "w640", "w960"} {
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.media_derivations (
    parent_object_id, child_object_id, format, parameters, tool_version
) VALUES ($1::uuid, $2::uuid, 'webp', jsonb_build_object('rendition', $3::text), $4)`,
			objectIDs["master"], objectIDs[rendition], rendition, input.ToolVersion); err != nil {
			return operations.MediaManifest{}, fmt.Errorf("create media derivation: %w", err)
		}
	}
	err = tx.QueryRow(ctx, `
INSERT INTO platform.entity_media (
    entity_type, entity_id, asset_id, purpose, position, is_primary, publication_status
) VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6, 'published')
RETURNING entity_media_id::text`, input.EntityType, input.EntityID, result.AssetID,
		input.Purpose, input.Position, input.IsPrimary).Scan(&result.EntityMediaID)
	if err != nil {
		return operations.MediaManifest{}, mapConstraintError(err)
	}
	// Images are part of cached catalog payloads even when no text revision
	// changed. Commit their durable refresh event with the media association.
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1, $2::uuid, 'cache_purge',
        jsonb_build_object('entity_type', $1::text, 'entity_id', $2::uuid, 'asset_id', $3::text), 'media-publish:' || $3::text)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, input.EntityType, input.EntityID, result.AssetID); err != nil {
		return operations.MediaManifest{}, fmt.Errorf("enqueue media publication refresh: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "media.manifest_publish", "media_asset", result.AssetID, nil,
		map[string]any{"entity_type": input.EntityType, "entity_id": input.EntityID, "object_count": len(input.Objects),
			"content_classification": input.ContentClassification, "external_preview_permission": input.ExternalPreviewPermission}, input.Reason, requestID, now); err != nil {
		return operations.MediaManifest{}, err
	}
	return result, nil
}

func validMediaAssociation(input operations.MediaManifestInput) bool {
	policyValid := (input.ContentClassification == "unknown" && input.ExternalPreviewPermission == "unknown") ||
		((input.ContentClassification == "non_explicit" || input.ContentClassification == "explicit") &&
			(input.ExternalPreviewPermission == "deny" || (input.ExternalPreviewPermission == "allow" && input.ContentClassification == "non_explicit")))
	return policyValid && ((input.EntityType == "work" && input.AssetType == "work_image" && input.Purpose == "cover" && input.IsPrimary && input.Position == 0) ||
		(input.EntityType == "work" && input.AssetType == "work_image" && input.Purpose == "gallery" && !input.IsPrimary && input.Position >= 1 && input.Position <= 3) ||
		(input.EntityType == "performer" && input.AssetType == "performer_avatar" && input.Purpose == "avatar" && input.IsPrimary && input.Position == 0))
}

func normalizeMediaPreviewPolicy(input *operations.MediaManifestInput) {
	if input.ContentClassification == "" && input.ExternalPreviewPermission == "" {
		input.ContentClassification = "unknown"
		input.ExternalPreviewPermission = "unknown"
	}
}
