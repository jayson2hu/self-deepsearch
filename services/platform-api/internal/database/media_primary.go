package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

// A single snapshot includes draft/hidden parent state for operators. No
// storage keys or private master URLs are part of this read contract.
func (store *Store) GetPrimaryMedia(ctx context.Context, entityType, entityID string) (operations.PrimaryMediaState, error) {
	if entityType != "work" && entityType != "performer" {
		return operations.PrimaryMediaState{}, operations.ErrInvalidInput
	}
	return scanPrimaryMediaState(store.pool.QueryRow(ctx, `
WITH parent AS (
 SELECT work_id AS entity_id, publication_status FROM platform.works WHERE $1 = 'work' AND work_id = $2::uuid
 UNION ALL
 SELECT performer_id, publication_status FROM platform.performers WHERE $1 = 'performer' AND performer_id = $2::uuid
)
SELECT parent.entity_id::text, parent.publication_status, link.asset_id::text,
       link.entity_media_id::text, link.purpose
FROM parent LEFT JOIN platform.entity_media link
 ON link.entity_type = $1 AND link.entity_id = parent.entity_id
 AND link.is_primary AND link.publication_status = 'published'
 AND (($1 = 'work' AND link.purpose = 'cover' AND link.position = 0)
   OR ($1 = 'performer' AND link.purpose = 'avatar' AND link.position = 0))`, entityType, entityID), entityType)
}

func scanPrimaryMediaState(row pgx.Row, entityType string) (operations.PrimaryMediaState, error) {
	state := operations.PrimaryMediaState{EntityType: entityType}
	var assetID, linkID, purpose *string
	err := row.Scan(&state.EntityID, &state.EntityStatus, &assetID, &linkID, &purpose)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.PrimaryMediaState{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.PrimaryMediaState{}, fmt.Errorf("read primary media: %w", err)
	}
	if assetID != nil && linkID != nil && purpose != nil {
		state.Primary = &operations.PrimaryMedia{AssetID: *assetID, EntityMediaID: *linkID, Purpose: *purpose}
	}
	return state, nil
}

func (store *Store) ReplacePrimaryMedia(ctx context.Context, input operations.ReplacePrimaryMediaInput, actorID, requestID string, now time.Time) (operations.PrimaryMediaReplacement, error) {
	return replacePrimaryMedia(ctx, store.pool.BeginTx, input, actorID, requestID, now)
}

func replacePrimaryMedia(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), input operations.ReplacePrimaryMediaInput, actorID, requestID string, now time.Time) (operations.PrimaryMediaReplacement, error) {
	empty := operations.PrimaryMediaReplacement{}
	manifest := input.Manifest
	if !validMediaAssociation(manifest) || !manifest.IsPrimary ||
		input.ExpectedAssetID == "" || strings.EqualFold(input.ExpectedAssetID, manifest.AssetID) {
		return empty, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{})
	if err != nil {
		return empty, fmt.Errorf("begin primary replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockMediaParent(ctx, tx, manifest); err != nil {
		return empty, err
	}
	readPrimary := func() (operations.PrimaryMedia, error) {
		var item operations.PrimaryMedia
		err := tx.QueryRow(ctx, `
SELECT asset_id::text, entity_media_id::text, purpose FROM platform.entity_media
WHERE entity_type = $1 AND entity_id = $2::uuid AND is_primary AND publication_status = 'published'
  AND (($1 = 'work' AND purpose = 'cover' AND position = 0)
    OR ($1 = 'performer' AND purpose = 'avatar' AND position = 0))`,
			manifest.EntityType, manifest.EntityID).Scan(&item.AssetID, &item.EntityMediaID, &item.Purpose)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !strings.EqualFold(item.AssetID, input.ExpectedAssetID)) {
			return item, operations.ErrConflict
		}
		return item, err
	}
	previous, err := readPrimary()
	if err != nil {
		return empty, err
	}
	// Match asset-rights takedown's asset -> link lock order. Re-read after
	// waiting: a concurrent takedown may have withdrawn the link meanwhile.
	var assetStatus, rightsStatus string
	err = tx.QueryRow(ctx, `SELECT asset_status, rights_status FROM platform.media_assets WHERE asset_id = $1::uuid FOR UPDATE`,
		previous.AssetID).Scan(&assetStatus, &rightsStatus)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (assetStatus != "published" || rightsStatus != "allowed")) {
		return empty, operations.ErrConflict
	}
	if err != nil {
		return empty, fmt.Errorf("lock previous media asset: %w", err)
	}
	current, err := readPrimary()
	if err != nil {
		return empty, err
	}
	if current.EntityMediaID != previous.EntityMediaID {
		return empty, operations.ErrConflict
	}
	command, err := tx.Exec(ctx, `UPDATE platform.entity_media SET publication_status = 'hidden'
WHERE entity_media_id = $1::uuid AND asset_id = $2::uuid AND is_primary AND publication_status = 'published'`,
		previous.EntityMediaID, previous.AssetID)
	if err != nil {
		return empty, fmt.Errorf("withdraw previous primary link: %w", err)
	}
	if command.RowsAffected() != 1 {
		return empty, operations.ErrConflict
	}
	media, err := insertMediaManifest(ctx, tx, manifest, actorID, requestID, now)
	if err != nil {
		return empty, err
	}
	result := operations.PrimaryMediaReplacement{Media: media, PreviousAssetID: previous.AssetID}
	var shared bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.entity_media
WHERE asset_id = $1::uuid AND publication_status = 'published')`, previous.AssetID).Scan(&shared); err != nil {
		return empty, fmt.Errorf("check previous media references: %w", err)
	}
	if !shared {
		if _, err := tx.Exec(ctx, `UPDATE platform.media_assets SET asset_status = 'hidden' WHERE asset_id = $1::uuid`, previous.AssetID); err != nil {
			return empty, fmt.Errorf("retire previous media asset: %w", err)
		}
		deadline := now.UTC().Add(30 * 24 * time.Hour)
		if err := retireMediaObjects(ctx, tx, []string{previous.AssetID}, deadline, now); err != nil {
			return empty, err
		}
		result.OldAssetRetired, result.PrivateRetainedUntil = true, &deadline
	}
	if err := insertAudit(ctx, tx, actorID, "media.primary_replace", "media_asset", media.AssetID,
		map[string]any{"asset_id": previous.AssetID, "entity_media_id": previous.EntityMediaID},
		map[string]any{"asset_id": media.AssetID, "entity_type": manifest.EntityType, "entity_id": manifest.EntityID,
			"old_asset_retired": result.OldAssetRetired, "private_retained_until": result.PrivateRetainedUntil}, manifest.Reason, requestID, now); err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, fmt.Errorf("commit primary replacement: %w", err)
	}
	return result, nil
}
