package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/operations"
)

const usageReviewColumns = `review_id::text,action,status,created_at,expires_at,completed_at,error_code,reason,next_period_start,next_period_end`

func scanUsageReview(row pgx.Row) (operations.MediaUsageReview, error) {
	var r operations.MediaUsageReview
	err := row.Scan(&r.ID, &r.Action, &r.Status, &r.CreatedAt, &r.ExpiresAt, &r.CompletedAt, &r.ErrorCode, &r.Reason, &r.NextPeriodStart, &r.NextPeriodEnd)
	return r, err
}

func (store *Store) GetMediaUsage(ctx context.Context, now time.Time) (operations.MediaUsageOverview, error) {
	result := operations.MediaUsageOverview{Reviews: []operations.MediaUsageReview{}, Enforcement: "not_connected"}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var s operations.MediaUsageState
	err = tx.QueryRow(ctx, `SELECT config_digest,period_start,period_end,updated_at,last_success_at,last_until,
 high_class_a::text,high_class_b::text,review_required,review_reason,stop_recommended,last_status,active_run_id IS NOT NULL,last_review_id::text
 FROM platform.media_usage_state WHERE singleton`).Scan(&s.ConfigDigest, &s.PeriodStart, &s.PeriodEnd, &s.UpdatedAt, &s.LastSuccessAt, &s.LastUntil,
		&s.ClassAHighwater, &s.ClassBHighwater, &s.ReviewRequired, &s.ReviewReason, &s.StopRecommended, &s.LastStatus, &s.ActiveRun, &s.LastReviewID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if err == nil {
		s.ObservationFresh = s.LastSuccessAt != nil && s.LastUntil != nil && !now.Before(*s.LastSuccessAt) && !now.Before(*s.LastUntil) &&
			now.Sub(*s.LastSuccessAt) <= 30*time.Minute && now.Sub(*s.LastUntil) <= 30*time.Minute && !now.Before(s.PeriodStart) && now.Before(s.PeriodEnd)
		result.State = &s
	}
	rows, err := tx.Query(ctx, `SELECT `+usageReviewColumns+` FROM audit.media_usage_reviews ORDER BY created_at DESC,review_id DESC LIMIT 10`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanUsageReview(rows)
		if err != nil {
			return result, err
		}
		result.Reviews = append(result.Reviews, r)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return operations.MediaUsageOverview{}, err
	}
	return result, nil
}

func (store *Store) SubmitMediaUsageReview(ctx context.Context, input operations.MediaUsageReviewInput, actorID, requestID string, now time.Time) (operations.MediaUsageReview, error) {
	return submitMediaUsageReview(ctx, store.pool.BeginTx, input, actorID, requestID, now)
}

func submitMediaUsageReview(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), input operations.MediaUsageReviewInput, actorID, requestID string, now time.Time) (operations.MediaUsageReview, error) {
	empty := operations.MediaUsageReview{}
	if !input.Valid() || now.IsZero() {
		return empty, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-usage'))`); err != nil {
		return empty, err
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT platform.lock_media_usage_reviewer($1::uuid)`, actorID).Scan(&allowed); err != nil {
		return empty, err
	}
	if !allowed {
		return empty, operations.ErrForbidden
	}
	// Check idempotency before current-state CAS: a lost response can be retried
	// after the Worker has applied it, but altered payloads cannot reuse the key.
	var priorID string
	var matches bool
	err = tx.QueryRow(ctx, `SELECT review_id::text, action=$3 AND expected_digest=$4 AND expected_updated_at=$5 AND
 next_period_start IS NOT DISTINCT FROM $6::timestamptz AND next_period_end IS NOT DISTINCT FROM $7::timestamptz AND reason=$8
 FROM audit.media_usage_reviews WHERE actor_id=$1::uuid AND idempotency_key=$2::uuid`, actorID, input.IdempotencyKey, input.Action, input.ExpectedDigest,
		input.ExpectedUpdatedAt, input.NextPeriodStart, input.NextPeriodEnd, input.Reason).Scan(&priorID, &matches)
	if err == nil {
		if !matches {
			return empty, operations.ErrConflict
		}
		item, err := scanUsageReview(tx.QueryRow(ctx, `SELECT `+usageReviewColumns+` FROM audit.media_usage_reviews WHERE review_id=$1::uuid`, priorID))
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
	var digest string
	var updated, oldEnd time.Time
	var active bool
	// The API has no UPDATE privilege on state; only this fixed-projection
	// SECURITY DEFINER helper may acquire its singleton row lock.
	err = tx.QueryRow(ctx, `SELECT config_digest,updated_at,period_end,active_run FROM platform.lock_media_usage_review_state()`).Scan(&digest, &updated, &oldEnd, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, operations.ErrNotFound
	}
	if err != nil {
		return empty, err
	}
	if digest != input.ExpectedDigest || !updated.Equal(input.ExpectedUpdatedAt) || active {
		return empty, operations.ErrConflict
	}
	if input.Action == "rotate_period" && (input.NextPeriodStart.Before(oldEnd) || now.Before(*input.NextPeriodStart) || !now.Before(*input.NextPeriodEnd)) {
		return empty, operations.ErrInvalidInput
	}
	item, err := scanUsageReview(tx.QueryRow(ctx, `INSERT INTO audit.media_usage_reviews
 (actor_id,idempotency_key,action,expected_digest,expected_updated_at,next_period_start,next_period_end,reason,request_id,created_at,expires_at)
 VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+usageReviewColumns, actorID, strings.ToLower(input.IdempotencyKey), input.Action, input.ExpectedDigest,
		input.ExpectedUpdatedAt, input.NextPeriodStart, input.NextPeriodEnd, input.Reason, requestID, now, now.Add(5*time.Minute)))
	if err != nil {
		return empty, mapConstraintError(err)
	}
	if err = insertAudit(ctx, tx, actorID, "media_usage.review_requested", "media_usage_review", item.ID,
		map[string]any{"config_digest": digest, "updated_at": updated}, map[string]any{"action": input.Action, "status": "pending", "enforcement": "not_connected"}, input.Reason, requestID, now); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return item, nil
}
