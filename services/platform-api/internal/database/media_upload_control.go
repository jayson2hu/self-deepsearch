package database

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/operations"
)

const uploadCommandColumns = `command_id::text,actor_id::text,mode,reason,status,created_at,expires_at,first_dispatched_at,completed_at,dispatch_count,error_code,receipt`

func scanUploadCommand(row pgx.Row) (operations.MediaUploadCommand, error) {
	var c operations.MediaUploadCommand
	err := row.Scan(&c.ID, &c.ActorID, &c.Mode, &c.Reason, &c.Status, &c.CreatedAt, &c.ExpiresAt, &c.FirstDispatchedAt, &c.CompletedAt, &c.DispatchCount, &c.ErrorCode, &c.Receipt)
	return c, err
}

func (store *Store) GetMediaUploadControl(ctx context.Context, now time.Time) (operations.MediaUploadOverview, error) {
	result := operations.MediaUploadOverview{Commands: []operations.MediaUploadCommand{}}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var state operations.MediaUploadState
	err = tx.QueryRow(ctx, `SELECT target_ref,last_receipt,last_success_at,updated_at,last_error_code FROM platform.media_upload_control_state WHERE singleton`).Scan(
		&state.TargetRef, &state.Receipt, &state.LastSuccessAt, &state.UpdatedAt, &state.ErrorCode)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil {
		state.Fresh = state.LastSuccessAt != nil && state.ErrorCode == nil && !now.Before(*state.LastSuccessAt) && now.Sub(*state.LastSuccessAt) <= 2*time.Minute
		result.State = &state
	}
	rows, err := tx.Query(ctx, `SELECT `+uploadCommandColumns+` FROM audit.media_upload_commands ORDER BY created_at DESC,command_id DESC LIMIT 10`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		c, err := scanUploadCommand(rows)
		if err != nil {
			return result, err
		}
		result.Commands = append(result.Commands, c)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return operations.MediaUploadOverview{}, err
	}
	return result, nil
}

func (store *Store) SubmitMediaUploadCommand(ctx context.Context, in operations.MediaUploadInput, actorID, requestID string, now time.Time) (operations.MediaUploadCommand, error) {
	return submitMediaUploadCommand(ctx, store.pool.BeginTx, in, actorID, requestID, now)
}

func submitMediaUploadCommand(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), in operations.MediaUploadInput, actorID, requestID string, now time.Time) (operations.MediaUploadCommand, error) {
	empty := operations.MediaUploadCommand{}
	if !in.Valid() || now.IsZero() {
		return empty, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-upload-control'))`); err != nil {
		return empty, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT platform.lock_media_usage_reviewer($1::uuid)`, actorID).Scan(&allowed); err != nil {
		return empty, err
	}
	if !allowed {
		return empty, operations.ErrForbidden
	}
	var priorID string
	var matches bool
	err = tx.QueryRow(ctx, `SELECT command_id::text,target_ref=$3 AND expected_epoch=$4 AND expected_generation=$5 AND expected_observed_at=$6 AND mode=$7 AND reason=$8
 FROM audit.media_upload_commands WHERE actor_id=$1::uuid AND idempotency_key=$2::uuid`, actorID, in.IdempotencyKey, in.TargetRef, in.ExpectedEpoch, *in.ExpectedGeneration, in.ExpectedObservedAt, in.Mode, in.Reason).Scan(&priorID, &matches)
	if err == nil {
		if !matches {
			return empty, operations.ErrConflict
		}
		item, err := scanUploadCommand(tx.QueryRow(ctx, `SELECT `+uploadCommandColumns+` FROM audit.media_upload_commands WHERE command_id=$1::uuid`, priorID))
		if err != nil {
			return empty, err
		}
		item.IdempotentReplay = true
		if err = tx.Commit(ctx); err != nil {
			return empty, err
		}
		return item, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return empty, err
	}
	var target string
	var body []byte
	var observed *time.Time
	var errorCode *string
	var leaseLive bool
	err = tx.QueryRow(ctx, `SELECT target_ref,last_receipt,last_success_at,last_error_code,lease_live FROM platform.lock_media_upload_control_state()`).Scan(&target, &body, &observed, &errorCode, &leaseLive)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, operations.ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	var remote struct {
		Epoch      string `json:"epoch"`
		Generation int64  `json:"generation"`
		Mode       string `json:"mode"`
		Status     string `json:"status"`
	}
	if json.Unmarshal(body, &remote) != nil || target != in.TargetRef || observed == nil || !observed.Equal(in.ExpectedObservedAt) ||
		now.Before(*observed) || now.Sub(*observed) > 2*time.Minute || errorCode != nil || leaseLive || remote.Status != "observed" ||
		remote.Epoch != in.ExpectedEpoch || remote.Generation != *in.ExpectedGeneration {
		return empty, operations.ErrConflict
	}
	item, err := scanUploadCommand(tx.QueryRow(ctx, `INSERT INTO audit.media_upload_commands
 (actor_id,idempotency_key,target_ref,expected_epoch,expected_generation,expected_observed_at,mode,reason,request_id,created_at,expires_at)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+uploadCommandColumns,
		actorID, in.IdempotencyKey, in.TargetRef, in.ExpectedEpoch, *in.ExpectedGeneration, in.ExpectedObservedAt, in.Mode, in.Reason, requestID, now, now.UTC().Truncate(time.Second).Add(5*time.Minute)))
	if err != nil {
		return empty, mapConstraintError(err)
	}
	if err = insertAudit(ctx, tx, actorID, "media_upload.command_requested", "media_upload_command", item.ID,
		map[string]any{"target_ref": target, "epoch": remote.Epoch, "generation": remote.Generation, "observed_at": observed},
		map[string]any{"mode": in.Mode, "status": "pending"}, in.Reason, requestID, now); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return item, nil
}
