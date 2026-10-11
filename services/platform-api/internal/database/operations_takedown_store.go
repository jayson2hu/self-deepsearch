package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ListTakedownRequests(ctx context.Context, status string, limit int) ([]operations.TakedownRequest, error) {
	where := ""
	args := []any{limit}
	if status != "all" {
		where = "WHERE request_status = $2"
		args = append(args, status)
	}
	rows, err := store.pool.Query(ctx, `
SELECT takedown_request_id::text, requester_reference, entity_type, entity_id::text,
       evidence_reference, request_status, assigned_to::text, received_at, decided_at,
       completed_at, result_summary, updated_at
FROM audit.takedown_requests `+where+`
ORDER BY received_at DESC, takedown_request_id DESC LIMIT $1`, args...)
	if err != nil {
		return nil, fmt.Errorf("list takedown requests: %w", err)
	}
	defer rows.Close()
	items := make([]operations.TakedownRequest, 0)
	for rows.Next() {
		var item operations.TakedownRequest
		if err := rows.Scan(&item.ID, &item.RequesterReference, &item.EntityType, &item.EntityID,
			&item.EvidenceReference, &item.Status, &item.AssignedTo, &item.ReceivedAt, &item.DecidedAt,
			&item.CompletedAt, &item.ResultSummary, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan takedown request: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) CreateTakedownRequest(ctx context.Context, input operations.TakedownInput, actorID, requestID string, now time.Time) (operations.TakedownRequest, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("begin takedown request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.EntityType == "media" {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.media_assets WHERE asset_id = $1::uuid)`, input.EntityID).Scan(&exists); err != nil {
			return operations.TakedownRequest{}, fmt.Errorf("check media asset: %w", err)
		}
		if !exists {
			return operations.TakedownRequest{}, operations.ErrNotFound
		}
	} else if err := lockEntity(ctx, tx, input.EntityType, input.EntityID); err != nil {
		return operations.TakedownRequest{}, err
	}
	var item operations.TakedownRequest
	err = tx.QueryRow(ctx, `
INSERT INTO audit.takedown_requests (
    requester_reference, entity_type, entity_id, evidence_reference, request_status, received_at
) VALUES ($1, $2, $3::uuid, $4, 'received', $5)
RETURNING takedown_request_id::text, requester_reference, entity_type, entity_id::text,
          evidence_reference, request_status, assigned_to::text, received_at, decided_at,
          completed_at, result_summary, updated_at`, input.RequesterReference, input.EntityType,
		input.EntityID, input.EvidenceReference, now).Scan(&item.ID, &item.RequesterReference,
		&item.EntityType, &item.EntityID, &item.EvidenceReference, &item.Status, &item.AssignedTo,
		&item.ReceivedAt, &item.DecidedAt, &item.CompletedAt, &item.ResultSummary, &item.UpdatedAt)
	if err != nil {
		return operations.TakedownRequest{}, mapConstraintError(err)
	}
	if err := insertAudit(ctx, tx, actorID, "takedown.receive", "takedown_request", item.ID, nil,
		map[string]any{"entity_type": item.EntityType, "entity_id": item.EntityID}, "rights request registered", requestID, now); err != nil {
		return operations.TakedownRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("commit takedown request: %w", err)
	}
	return item, nil
}

func (store *Store) CompleteTakedownRequest(ctx context.Context, takedownID, actorID, resultSummary, requestID string, now time.Time) (operations.TakedownRequest, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("begin complete takedown: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item operations.TakedownRequest
	err = tx.QueryRow(ctx, `
SELECT takedown_request_id::text, requester_reference, entity_type, entity_id::text,
       evidence_reference, request_status, assigned_to::text, received_at, decided_at,
       completed_at, result_summary, updated_at
FROM audit.takedown_requests WHERE takedown_request_id = $1::uuid FOR UPDATE`, takedownID).Scan(
		&item.ID, &item.RequesterReference, &item.EntityType, &item.EntityID, &item.EvidenceReference,
		&item.Status, &item.AssignedTo, &item.ReceivedAt, &item.DecidedAt, &item.CompletedAt,
		&item.ResultSummary, &item.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.TakedownRequest{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("lock takedown request: %w", err)
	}
	if item.Status != "received" && item.Status != "validating" && item.Status != "approved" {
		return operations.TakedownRequest{}, operations.ErrConflict
	}
	if err := applyTakedown(ctx, tx, item.EntityType, item.EntityID, takedownID, now); err != nil {
		return operations.TakedownRequest{}, err
	}
	err = tx.QueryRow(ctx, `
UPDATE audit.takedown_requests
SET request_status = 'completed', assigned_to = $2::uuid, decided_at = $3,
    completed_at = $3, result_summary = $4
WHERE takedown_request_id = $1::uuid
RETURNING request_status, assigned_to::text, decided_at, completed_at, result_summary, updated_at`,
		takedownID, actorID, now, resultSummary).Scan(&item.Status, &item.AssignedTo, &item.DecidedAt,
		&item.CompletedAt, &item.ResultSummary, &item.UpdatedAt)
	if err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("complete takedown request: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "takedown.complete", "takedown_request", takedownID,
		map[string]any{"status": "received"}, map[string]any{"status": "completed", "entity_type": item.EntityType, "entity_id": item.EntityID}, resultSummary, requestID, now); err != nil {
		return operations.TakedownRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.TakedownRequest{}, fmt.Errorf("commit complete takedown: %w", err)
	}
	return item, nil
}

func applyTakedown(ctx context.Context, tx pgx.Tx, entityType, entityID, takedownID string, now time.Time) error {
	if entityType == "media" {
		command, err := tx.Exec(ctx, `UPDATE platform.media_assets SET rights_status = 'takedown', asset_status = 'takedown' WHERE asset_id = $1::uuid`, entityID)
		if err != nil || command.RowsAffected() == 0 {
			if err != nil {
				return fmt.Errorf("takedown media asset: %w", err)
			}
			return operations.ErrNotFound
		}
		_, err = tx.Exec(ctx, `UPDATE platform.entity_media SET publication_status = 'takedown' WHERE asset_id = $1::uuid`, entityID)
		if err != nil {
			return fmt.Errorf("takedown media links: %w", err)
		}
	} else {
		if err := setEntityStatus(ctx, tx, entityType, entityID, "takedown"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
UPDATE platform.publications SET publication_status = 'takedown', published_at = NULL,
    hidden_at = NULL, takedown_at = $3
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main'`, entityType, entityID, now)
		if err != nil {
			return fmt.Errorf("takedown publication: %w", err)
		}
		// Drafts can already own prepared media. The parent existence was
		// checked by setEntityStatus; a missing public snapshot must not block
		// its rights takedown or cause us to fabricate a publication record.
		_, err = tx.Exec(ctx, `
UPDATE platform.entity_media SET publication_status = 'takedown'
WHERE entity_type = $1 AND entity_id = $2::uuid`, entityType, entityID)
		if err != nil {
			return fmt.Errorf("takedown entity media links: %w", err)
		}
		_, err = tx.Exec(ctx, `
UPDATE platform.media_assets asset SET rights_status = 'takedown', asset_status = 'takedown'
WHERE asset.asset_id IN (
  SELECT target.asset_id FROM platform.entity_media target
  WHERE target.entity_type = $1 AND target.entity_id = $2::uuid
)
AND NOT EXISTS (
  SELECT 1 FROM platform.entity_media other
  WHERE other.asset_id = asset.asset_id AND other.publication_status = 'published'
)`, entityType, entityID)
		if err != nil {
			return fmt.Errorf("takedown unshared media assets: %w", err)
		}
	}
	rows, err := tx.Query(ctx, `
SELECT asset.asset_id::text FROM platform.media_assets asset
WHERE asset.asset_id IN (
  SELECT $1::uuid WHERE $2 = 'media'
  UNION SELECT target.asset_id FROM platform.entity_media target
    WHERE target.entity_type = $2 AND target.entity_id = $1::uuid
      AND NOT EXISTS (
        SELECT 1 FROM platform.entity_media other
        WHERE other.asset_id = target.asset_id AND other.publication_status = 'published'
      )
)
ORDER BY asset.asset_id`, entityID, entityType)
	if err != nil {
		return fmt.Errorf("read takedown media assets: %w", err)
	}
	assetIDs := make([]string, 0)
	for rows.Next() {
		var assetID string
		if err := rows.Scan(&assetID); err != nil {
			rows.Close()
			return fmt.Errorf("scan takedown media asset: %w", err)
		}
		assetIDs = append(assetIDs, assetID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return fmt.Errorf("read takedown media assets: %w", err)
	}
	// Rights takedown overrides any private retention deadline immediately.
	if err := retireMediaObjects(ctx, tx, assetIDs, now, now); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1::text, $2::uuid, 'publication_takedown', jsonb_build_object('entity_type', $1::text, 'entity_id', $2::uuid), $3)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, entityType, entityID, takedownID)
	if err != nil {
		return fmt.Errorf("enqueue takedown side effects: %w", err)
	}
	return nil
}
