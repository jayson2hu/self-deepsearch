package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type retiredMediaObject struct {
	ID           string  `json:"media_object_id"`
	AssetID      string  `json:"asset_id"`
	StorageScope string  `json:"storage_scope"`
	StorageKey   string  `json:"storage_key"`
	BackupPath   string  `json:"backup_path"`
	PublicURL    *string `json:"public_url"`
}

type mediaDeletionLocation struct {
	AssetID, Scope, Key, BackupPath string
}

func deletionLocation(object retiredMediaObject) mediaDeletionLocation {
	return mediaDeletionLocation{object.AssetID, object.StorageScope, object.StorageKey, object.BackupPath}
}

type preservedMediaURL struct {
	URL       string
	CreatedAt time.Time
	EventID   string
}

// The caller must hold the target asset row locks and remove its public links
// before retiring objects. Private objects may be retained until the supplied
// deadline; public derivatives are always due immediately. Bytes remain
// counted until the worker confirms remote deletion.
func retireMediaObjects(ctx context.Context, tx pgx.Tx, assetIDs []string, privateDeleteAfter, now time.Time) error {
	if len(assetIDs) == 0 {
		return nil
	}
	// Worker completion locks outbox before media_objects. Take existing event
	// locks first as well, including legacy request-scoped events. Do not invert
	// this order when a takedown accelerates a future private-retention task.
	rows, err := tx.Query(ctx, `
SELECT event_id::text, aggregate_id::text, payload, created_at FROM platform.outbox_events
WHERE event_type = 'media_delete' AND aggregate_id = ANY($1::uuid[])
ORDER BY event_id FOR UPDATE`, assetIDs)
	if err != nil {
		return fmt.Errorf("lock media deletion events: %w", err)
	}
	originalURLs := make(map[mediaDeletionLocation]preservedMediaURL)
	for rows.Next() {
		var eventID, assetID string
		var payload []byte
		var createdAt time.Time
		if err := rows.Scan(&eventID, &assetID, &payload, &createdAt); err != nil {
			rows.Close()
			return fmt.Errorf("scan media deletion event: %w", err)
		}
		// Parse each retained event once rather than running a correlated
		// outbox search for every object. Malformed/unrelated legacy payloads
		// are not evidence of a deletion location and must never be reused.
		var previous retiredMediaObject
		if json.Unmarshal(payload, &previous) != nil || previous.AssetID != assetID || previous.StorageScope != "public" {
			continue
		}
		if previous.ID == "" {
			previous.ID = "legacy"
		}
		if validateRetiredMediaObject(previous) != nil {
			continue
		}
		key := deletionLocation(previous)
		old, exists := originalURLs[key]
		if !exists || createdAt.Before(old.CreatedAt) || (createdAt.Equal(old.CreatedAt) && eventID < old.EventID) {
			originalURLs[key] = preservedMediaURL{URL: *previous.PublicURL, CreatedAt: createdAt, EventID: eventID}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read media deletion events: %w", err)
	}
	rows, err = tx.Query(ctx, `
WITH targets AS MATERIALIZED (
  SELECT object.media_object_id, object.asset_id, object.storage_scope, object.storage_key,
         object.backup_path, object.public_url
  FROM platform.media_objects object
  WHERE object.asset_id = ANY($1::uuid[]) AND object.storage_provider = 's3'
    AND object.object_status <> 'deleted'
  ORDER BY object.media_object_id FOR UPDATE OF object
), withdrawn AS (
  UPDATE platform.media_objects object
  SET object_status = 'hidden', public_url = NULL, deleted_at = NULL
  FROM targets WHERE object.media_object_id = targets.media_object_id
  RETURNING object.media_object_id
)
SELECT targets.media_object_id::text, targets.asset_id::text, targets.storage_scope,
       targets.storage_key, targets.backup_path, targets.public_url
FROM targets JOIN withdrawn USING (media_object_id)`, assetIDs)
	if err != nil {
		return fmt.Errorf("withdraw media objects: %w", err)
	}
	objects := make([]retiredMediaObject, 0)
	for rows.Next() {
		var object retiredMediaObject
		if err := rows.Scan(&object.ID, &object.AssetID, &object.StorageScope, &object.StorageKey, &object.BackupPath, &object.PublicURL); err != nil {
			rows.Close()
			return fmt.Errorf("scan retired media object: %w", err)
		}
		if object.StorageScope == "public" && object.PublicURL == nil {
			if preserved, ok := originalURLs[deletionLocation(object)]; ok {
				value := preserved.URL
				object.PublicURL = &value
			}
		}
		objects = append(objects, object)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read retired media objects: %w", err)
	}
	for _, object := range objects {
		deleteAfter := now
		if object.StorageScope == "private" && privateDeleteAfter.After(now) {
			deleteAfter = privateDeleteAfter
		}
		if err := queueMediaDeletion(ctx, tx, object, deleteAfter); err != nil {
			return err
		}
	}
	return nil
}

func queueMediaDeletion(ctx context.Context, tx pgx.Tx, object retiredMediaObject, deleteAfter time.Time) error {
	if err := validateRetiredMediaObject(object); err != nil {
		return err
	}
	payload, err := json.Marshal(object)
	if err != nil {
		return fmt.Errorf("encode media deletion identity: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO platform.outbox_events AS existing
    (aggregate_type, aggregate_id, event_type, payload, dedupe_key, available_at)
VALUES ('media', $1::uuid, 'media_delete', $2::jsonb, 'media-delete:' || $3::text, $4)
ON CONFLICT (event_type, dedupe_key) DO UPDATE
SET available_at = least(existing.available_at, EXCLUDED.available_at)
WHERE existing.status IN ('pending', 'retry')
  AND EXCLUDED.available_at < existing.available_at`, object.AssetID, payload, object.ID, deleteAfter)
	if err != nil {
		return fmt.Errorf("enqueue media delete: %w", err)
	}
	return nil
}

func validateRetiredMediaObject(object retiredMediaObject) error {
	invalid := errors.New("media deletion identity is incomplete")
	if object.ID == "" || object.AssetID == "" || object.StorageKey == "" || object.BackupPath != object.StorageKey ||
		strings.ContainsAny(object.StorageKey, "\\\r\n\x00") {
		return invalid
	}
	for _, segment := range strings.Split(object.StorageKey, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return invalid
		}
	}
	if object.StorageScope == "private" {
		if object.PublicURL != nil || !strings.HasPrefix(object.StorageKey, "media-master/") {
			return invalid
		}
		return nil
	}
	if object.StorageScope != "public" || object.PublicURL == nil || !strings.HasPrefix(object.StorageKey, "media-public/") {
		return invalid
	}
	parsed, err := url.Parse(*object.PublicURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || !strings.HasSuffix(parsed.Path, "/"+object.StorageKey) {
		return invalid
	}
	return nil
}
