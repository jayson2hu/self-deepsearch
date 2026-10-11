package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

const manualCSVSourceID = "81000000-0000-4000-8000-000000000001"

func (store *Store) ImportWorks(ctx context.Context, input operations.WorkImportInput, actorID, requestID string, now time.Time) (operations.WorkImportBatch, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.WorkImportBatch{}, fmt.Errorf("begin work import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing operations.WorkImportBatch
	var existingHash string
	var summary []byte
	err = tx.QueryRow(ctx, `SELECT batch_id::text, request_hash, status, input_count, accepted_count, rejected_count, summary, received_at, completed_at
FROM collector.publication_batches WHERE idempotency_key = $1 FOR UPDATE`, input.IdempotencyKey).
		Scan(&existing.ID, &existingHash, &existing.Status, &existing.InputCount, &existing.AcceptedCount, &existing.RejectedCount, &summary, &existing.ReceivedAt, &existing.CompletedAt)
	if err == nil {
		if existingHash != input.RequestHash {
			return operations.WorkImportBatch{}, operations.ErrConflict
		}
		var saved struct {
			EntityIDs []string `json:"entity_ids"`
		}
		if err := json.Unmarshal(summary, &saved); err != nil {
			return operations.WorkImportBatch{}, fmt.Errorf("decode import summary: %w", err)
		}
		existing.EntityIDs, existing.IdempotentReplay = saved.EntityIDs, true
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return operations.WorkImportBatch{}, fmt.Errorf("read work import batch: %w", err)
	}
	var batch operations.WorkImportBatch
	batch.InputCount, batch.Status = len(input.Rows), "accepted"
	err = tx.QueryRow(ctx, `INSERT INTO collector.publication_batches
(batch_id, source_id, region, idempotency_key, request_hash, input_count, status)
VALUES (gen_random_uuid(), $1::uuid, 'japan', $2, $3, $4, 'receiving')
ON CONFLICT (idempotency_key) DO NOTHING RETURNING batch_id::text`,
		manualCSVSourceID, input.IdempotencyKey, input.RequestHash, len(input.Rows)).Scan(&batch.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		var replay operations.WorkImportBatch
		var replayHash string
		var replaySummary []byte
		if err := tx.QueryRow(ctx, `SELECT batch_id::text, request_hash, status, input_count, accepted_count, rejected_count,
summary, received_at, completed_at FROM collector.publication_batches WHERE idempotency_key = $1`, input.IdempotencyKey).
			Scan(&replay.ID, &replayHash, &replay.Status, &replay.InputCount, &replay.AcceptedCount, &replay.RejectedCount,
				&replaySummary, &replay.ReceivedAt, &replay.CompletedAt); err != nil {
			return operations.WorkImportBatch{}, fmt.Errorf("read concurrent import batch: %w", err)
		}
		if replayHash != input.RequestHash {
			return operations.WorkImportBatch{}, operations.ErrConflict
		}
		var saved struct {
			EntityIDs []string `json:"entity_ids"`
		}
		if err := json.Unmarshal(replaySummary, &saved); err != nil {
			return operations.WorkImportBatch{}, fmt.Errorf("decode concurrent import summary: %w", err)
		}
		replay.EntityIDs, replay.IdempotentReplay = saved.EntityIDs, true
		return replay, nil
	}
	if err != nil {
		return operations.WorkImportBatch{}, mapConstraintError(err)
	}
	batch.EntityIDs = make([]string, 0, len(input.Rows))
	for _, row := range input.Rows {
		sourceID := manualCSVSourceID
		row.Input.Sources = []operations.SourceEvidenceInput{{
			SourceID: &sourceID, SourceType: "manual_csv", SourceTitle: stringPointer("Manual CSV import"), CheckedAt: now,
		}}
		entity, payload, err := createWorkTx(ctx, tx, row.Input, actorID, requestID, now)
		if err != nil {
			return operations.WorkImportBatch{}, err
		}
		encoded, _ := json.Marshal(payload)
		contentHash := "sha256:" + sha256Hex(encoded)
		var sourceRecordID string
		if err := tx.QueryRow(ctx, `INSERT INTO collector.source_records
(batch_id, source_id, external_id, entity_type, connector_version, idempotency_key, content_hash, payload)
VALUES ($1::uuid, $2::uuid, $3, 'work', 'manual-csv-v1', $4, $5, $6::jsonb) RETURNING source_record_id::text`,
			batch.ID, manualCSVSourceID, row.Input.Code, input.IdempotencyKey+":"+fmt.Sprint(row.RowNumber), contentHash, encoded).Scan(&sourceRecordID); err != nil {
			return operations.WorkImportBatch{}, mapConstraintError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO collector.normalized_records
(source_record_id, parser_version, entity_type, normalized_payload, candidate_status)
VALUES ($1::uuid, 'manual-csv-v1', 'work', $2::jsonb, 'reviewing')`, sourceRecordID, encoded); err != nil {
			return operations.WorkImportBatch{}, mapConstraintError(err)
		}
		batch.EntityIDs = append(batch.EntityIDs, entity.ID)
	}
	batch.AcceptedCount, batch.ReceivedAt, batch.CompletedAt = len(batch.EntityIDs), now, now
	summaryJSON, _ := json.Marshal(map[string]any{"entity_ids": batch.EntityIDs})
	if _, err := tx.Exec(ctx, `UPDATE collector.publication_batches SET accepted_count = $2, status = 'accepted', summary = $3::jsonb, completed_at = $4 WHERE batch_id = $1::uuid`,
		batch.ID, batch.AcceptedCount, summaryJSON, now); err != nil {
		return operations.WorkImportBatch{}, fmt.Errorf("complete work import batch: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "work.import", "publication_batch", batch.ID, nil,
		map[string]any{"accepted_count": batch.AcceptedCount, "entity_ids": batch.EntityIDs}, input.Reason, requestID, now); err != nil {
		return operations.WorkImportBatch{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.WorkImportBatch{}, fmt.Errorf("commit work import: %w", err)
	}
	return batch, nil
}

func (store *Store) ListWorkImportBatches(ctx context.Context, limit int) ([]operations.WorkImportBatch, error) {
	rows, err := store.pool.Query(ctx, `SELECT batch_id::text, status, input_count, accepted_count, rejected_count,
summary, received_at, completed_at FROM collector.publication_batches
WHERE source_id = $1::uuid ORDER BY received_at DESC, batch_id DESC LIMIT $2`, manualCSVSourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list work import batches: %w", err)
	}
	defer rows.Close()
	items := make([]operations.WorkImportBatch, 0)
	for rows.Next() {
		var item operations.WorkImportBatch
		var summary []byte
		if err := rows.Scan(&item.ID, &item.Status, &item.InputCount, &item.AcceptedCount, &item.RejectedCount,
			&summary, &item.ReceivedAt, &item.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan work import batch: %w", err)
		}
		var saved struct {
			EntityIDs []string `json:"entity_ids"`
		}
		if err := json.Unmarshal(summary, &saved); err != nil {
			return nil, fmt.Errorf("decode work import batch: %w", err)
		}
		item.EntityIDs = saved.EntityIDs
		items = append(items, item)
	}
	return items, rows.Err()
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest)
}
