package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

const editorialRecommendationColumns = `
recommendation.recommendation_id::text,
recommendation.work_id::text,
work.canonical_code,
work.title,
published.canonical_slug,
recommendation.position,
recommendation.starts_at,
recommendation.ends_at,
recommendation.reason,
recommendation.status,
creator.normalized_email,
recommendation.created_at,
recommendation.updated_at`

func (store *Store) ListEditorialRecommendations(ctx context.Context, status string, limit int) ([]operations.EditorialRecommendation, error) {
	rows, err := store.pool.Query(ctx, `SELECT `+editorialRecommendationColumns+`
FROM platform.editorial_recommendations recommendation
JOIN platform.works work ON work.work_id = recommendation.work_id
LEFT JOIN platform.public_published_works published ON published.work_id = recommendation.work_id
JOIN platform.users creator ON creator.user_id = recommendation.created_by
WHERE $1 = 'all' OR recommendation.status = $1
ORDER BY CASE recommendation.status WHEN 'active' THEN 0 WHEN 'paused' THEN 1 ELSE 2 END,
         recommendation.position, recommendation.updated_at DESC, recommendation.recommendation_id
LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list editorial recommendations: %w", err)
	}
	defer rows.Close()
	items := make([]operations.EditorialRecommendation, 0)
	for rows.Next() {
		item, err := scanEditorialRecommendation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate editorial recommendations: %w", err)
	}
	return items, nil
}

func (store *Store) CreateEditorialRecommendation(ctx context.Context, input operations.EditorialRecommendationInput, actorID, requestID string, now time.Time) (operations.EditorialRecommendation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("begin editorial recommendation creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := requirePublishedWork(ctx, tx, input.WorkID); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if input.Status == "active" {
		if err := ensureNoEditorialOverlap(ctx, tx, input.WorkID, nil, input.StartsAt, input.EndsAt); err != nil {
			return operations.EditorialRecommendation{}, err
		}
	}
	var recommendationID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.editorial_recommendations
    (work_id, position, starts_at, ends_at, reason, status, created_by, created_at, updated_at)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8, $8)
RETURNING recommendation_id::text`, input.WorkID, input.Position, input.StartsAt, input.EndsAt,
		input.Reason, input.Status, actorID, now).Scan(&recommendationID)
	if err != nil {
		return operations.EditorialRecommendation{}, mapConstraintError(err)
	}
	after := map[string]any{"work_id": input.WorkID, "position": input.Position, "starts_at": input.StartsAt,
		"ends_at": input.EndsAt, "reason": input.Reason, "status": input.Status}
	if err := enqueueEditorialCacheInvalidation(ctx, tx, recommendationID, input.WorkID, "created", now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "editorial_recommendation.create", "editorial_recommendation",
		recommendationID, nil, after, input.ChangeReason, requestID, now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("commit editorial recommendation creation: %w", err)
	}
	return store.editorialRecommendationByID(ctx, recommendationID)
}

func (store *Store) UpdateEditorialRecommendation(ctx context.Context, recommendationID string, input operations.EditorialRecommendationInput, actorID, requestID string, now time.Time) (operations.EditorialRecommendation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("begin editorial recommendation update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := editorialRecommendationByIDWith(ctx, tx, recommendationID, true)
	if err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if current.Status == "removed" {
		return operations.EditorialRecommendation{}, operations.ErrConflict
	}
	if err := requirePublishedWork(ctx, tx, input.WorkID); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if input.Status == "active" {
		if err := ensureNoEditorialOverlap(ctx, tx, input.WorkID, &recommendationID, input.StartsAt, input.EndsAt); err != nil {
			return operations.EditorialRecommendation{}, err
		}
	}
	command, err := tx.Exec(ctx, `
UPDATE platform.editorial_recommendations
SET work_id = $2::uuid, position = $3, starts_at = $4, ends_at = $5,
    reason = $6, status = $7, updated_at = $8
WHERE recommendation_id = $1::uuid AND status <> 'removed'`, recommendationID, input.WorkID,
		input.Position, input.StartsAt, input.EndsAt, input.Reason, input.Status, now)
	if err != nil {
		return operations.EditorialRecommendation{}, mapConstraintError(err)
	}
	if command.RowsAffected() != 1 {
		return operations.EditorialRecommendation{}, operations.ErrConflict
	}
	before := map[string]any{"work_id": current.WorkID, "position": current.Position, "starts_at": current.StartsAt,
		"ends_at": current.EndsAt, "reason": current.Reason, "status": current.Status}
	after := map[string]any{"work_id": input.WorkID, "position": input.Position, "starts_at": input.StartsAt,
		"ends_at": input.EndsAt, "reason": input.Reason, "status": input.Status}
	if err := enqueueEditorialCacheInvalidation(ctx, tx, recommendationID, input.WorkID, "updated", now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "editorial_recommendation.update", "editorial_recommendation",
		recommendationID, before, after, input.ChangeReason, requestID, now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("commit editorial recommendation update: %w", err)
	}
	return store.editorialRecommendationByID(ctx, recommendationID)
}

func (store *Store) RemoveEditorialRecommendation(ctx context.Context, recommendationID, actorID, reason, requestID string, now time.Time) (operations.EditorialRecommendation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("begin editorial recommendation removal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := editorialRecommendationByIDWith(ctx, tx, recommendationID, true)
	if err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if current.Status == "removed" {
		return operations.EditorialRecommendation{}, operations.ErrConflict
	}
	command, err := tx.Exec(ctx, `
UPDATE platform.editorial_recommendations SET status = 'removed', updated_at = $2
WHERE recommendation_id = $1::uuid AND status <> 'removed'`, recommendationID, now)
	if err != nil {
		return operations.EditorialRecommendation{}, mapConstraintError(err)
	}
	if command.RowsAffected() != 1 {
		return operations.EditorialRecommendation{}, operations.ErrConflict
	}
	if err := enqueueEditorialCacheInvalidation(ctx, tx, recommendationID, current.WorkID, "removed", now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "editorial_recommendation.remove", "editorial_recommendation",
		recommendationID, map[string]any{"status": current.Status}, map[string]any{"status": "removed"},
		reason, requestID, now); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("commit editorial recommendation removal: %w", err)
	}
	return store.editorialRecommendationByID(ctx, recommendationID)
}

func requirePublishedWork(ctx context.Context, tx pgx.Tx, workID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM platform.public_published_works WHERE work_id = $1::uuid
)`, workID).Scan(&exists); err != nil {
		return fmt.Errorf("check published work for recommendation: %w", err)
	}
	if !exists {
		return operations.ErrInvalidInput
	}
	return nil
}

func ensureNoEditorialOverlap(ctx context.Context, tx pgx.Tx, workID string, excludeID *string, startsAt time.Time, endsAt *time.Time) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, workID); err != nil {
		return fmt.Errorf("lock editorial recommendation schedule: %w", err)
	}
	var excluded any
	if excludeID != nil {
		excluded = *excludeID
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1 FROM platform.editorial_recommendations
    WHERE work_id = $1::uuid
      AND status = 'active'
      AND ($2::uuid IS NULL OR recommendation_id <> $2::uuid)
      AND tstzrange(starts_at, ends_at, '[)') && tstzrange($3, $4, '[)')
)`, workID, excluded, startsAt, endsAt).Scan(&exists); err != nil {
		return fmt.Errorf("check editorial recommendation overlap: %w", err)
	}
	if exists {
		return operations.ErrConflict
	}
	return nil
}

func enqueueEditorialCacheInvalidation(ctx context.Context, tx pgx.Tx, recommendationID, workID, action string, now time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ('editorial_recommendation', $1::uuid, 'cache_purge',
        jsonb_build_object('reason', 'editorial_recommendation_changed', 'work_id', $2::text, 'action', $3::text),
        $1::text || ':' || $3 || ':' || $4)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, recommendationID, workID, action, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("enqueue editorial recommendation cache invalidation: %w", err)
	}
	return nil
}

func (store *Store) editorialRecommendationByID(ctx context.Context, recommendationID string) (operations.EditorialRecommendation, error) {
	return editorialRecommendationByIDWith(ctx, store.pool, recommendationID, false)
}

type editorialQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func editorialRecommendationByIDWith(ctx context.Context, queryer editorialQueryer, recommendationID string, lock bool) (operations.EditorialRecommendation, error) {
	query := `SELECT ` + editorialRecommendationColumns + `
FROM platform.editorial_recommendations recommendation
JOIN platform.works work ON work.work_id = recommendation.work_id
LEFT JOIN platform.public_published_works published ON published.work_id = recommendation.work_id
JOIN platform.users creator ON creator.user_id = recommendation.created_by
WHERE recommendation.recommendation_id = $1::uuid`
	if lock {
		query += " FOR UPDATE OF recommendation"
	}
	item, err := scanEditorialRecommendation(queryer.QueryRow(ctx, query, recommendationID))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.EditorialRecommendation{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.EditorialRecommendation{}, fmt.Errorf("read editorial recommendation: %w", err)
	}
	return item, nil
}

type editorialRecommendationScanner interface {
	Scan(...any) error
}

func scanEditorialRecommendation(scanner editorialRecommendationScanner) (operations.EditorialRecommendation, error) {
	var item operations.EditorialRecommendation
	if err := scanner.Scan(&item.ID, &item.WorkID, &item.WorkCode, &item.WorkTitle, &item.WorkSlug,
		&item.Position, &item.StartsAt, &item.EndsAt, &item.Reason, &item.Status, &item.CreatedBy,
		&item.CreatedAt, &item.UpdatedAt); err != nil {
		return operations.EditorialRecommendation{}, err
	}
	return item, nil
}
