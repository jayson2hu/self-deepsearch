package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ListConflictReviews(ctx context.Context, status string, limit int) ([]operations.ConflictReview, error) {
	rows, err := store.pool.Query(ctx, `
SELECT conflict.conflict_review_id::text, conflict.review_task_id::text,
       conflict.entity_type, conflict.entity_id::text, conflict.field_name,
       conflict.current_value, conflict.candidate_value,
       conflict.source_id::text, source.name, conflict.source_priority,
       conflict.resolution, resolver.normalized_email, conflict.resolved_at, conflict.created_at
FROM collector.conflict_reviews conflict
LEFT JOIN collector.sources source ON source.source_id = conflict.source_id
LEFT JOIN platform.users resolver ON resolver.user_id = conflict.resolved_by
WHERE ($1 = 'all')
   OR ($1 = 'open' AND conflict.resolution IS NULL)
   OR ($1 = 'resolved' AND conflict.resolution IS NOT NULL)
ORDER BY conflict.resolution NULLS FIRST, conflict.created_at, conflict.conflict_review_id
LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list conflict reviews: %w", err)
	}
	defer rows.Close()
	items := make([]operations.ConflictReview, 0)
	for rows.Next() {
		item, err := scanConflictReview(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conflict reviews: %w", err)
	}
	return items, nil
}

func (store *Store) ResolveConflictReview(ctx context.Context, conflictID, resolution, actorID, reason, requestID string, now time.Time) (operations.ConflictReview, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.ConflictReview{}, fmt.Errorf("begin resolve conflict: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row := tx.QueryRow(ctx, `
SELECT conflict.conflict_review_id::text, conflict.review_task_id::text,
       conflict.entity_type, conflict.entity_id::text, conflict.field_name,
       conflict.current_value, conflict.candidate_value,
       conflict.source_id::text, source.name, conflict.source_priority,
       conflict.resolution, resolver.normalized_email, conflict.resolved_at, conflict.created_at
FROM collector.conflict_reviews conflict
LEFT JOIN collector.sources source ON source.source_id = conflict.source_id
LEFT JOIN platform.users resolver ON resolver.user_id = conflict.resolved_by
WHERE conflict.conflict_review_id = $1::uuid
FOR UPDATE OF conflict`, conflictID)
	item, err := scanConflictReview(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.ConflictReview{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.ConflictReview{}, err
	}
	if item.Resolution != nil {
		return operations.ConflictReview{}, operations.ErrConflict
	}
	var taskType, taskStatus string
	if err := tx.QueryRow(ctx, `
SELECT task_type, status FROM collector.review_tasks
WHERE review_task_id = $1::uuid FOR UPDATE`, item.ReviewTaskID).Scan(&taskType, &taskStatus); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("lock conflict review task: %w", err)
	}
	if taskType != "conflict" || (taskStatus != "pending" && taskStatus != "claimed") {
		return operations.ConflictReview{}, operations.ErrConflict
	}
	command, err := tx.Exec(ctx, `
UPDATE collector.conflict_reviews
SET resolution = $2, resolved_by = $3::uuid, resolved_at = $4
WHERE conflict_review_id = $1::uuid AND resolution IS NULL`, conflictID, resolution, actorID, now)
	if err != nil {
		return operations.ConflictReview{}, mapConstraintError(err)
	}
	if command.RowsAffected() != 1 {
		return operations.ConflictReview{}, operations.ErrConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE collector.review_tasks task
SET status = 'approved', completed_at = $2, updated_at = $2
WHERE task.review_task_id = $1::uuid AND task.task_type = 'conflict'
  AND task.status IN ('pending', 'claimed')
  AND NOT EXISTS (
    SELECT 1 FROM collector.conflict_reviews conflict
    WHERE conflict.review_task_id = task.review_task_id AND conflict.resolution IS NULL
  )`, item.ReviewTaskID, now); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("complete conflict review task: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "conflict.resolve", "conflict_review", conflictID,
		map[string]any{"resolution": nil}, map[string]any{"resolution": resolution}, reason, requestID, now); err != nil {
		return operations.ConflictReview{}, err
	}
	var resolverEmail string
	if err := tx.QueryRow(ctx, `SELECT normalized_email FROM platform.users WHERE user_id = $1::uuid`, actorID).Scan(&resolverEmail); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("read conflict resolver: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("commit conflict resolution: %w", err)
	}
	item.Resolution = &resolution
	item.ResolvedBy = &resolverEmail
	item.ResolvedAt = &now
	return item, nil
}

type conflictReviewScanner interface {
	Scan(...any) error
}

func scanConflictReview(scanner conflictReviewScanner) (operations.ConflictReview, error) {
	var item operations.ConflictReview
	var currentJSON, candidateJSON []byte
	if err := scanner.Scan(&item.ID, &item.ReviewTaskID, &item.EntityType, &item.EntityID, &item.FieldName,
		&currentJSON, &candidateJSON, &item.SourceID, &item.SourceName, &item.SourcePriority,
		&item.Resolution, &item.ResolvedBy, &item.ResolvedAt, &item.CreatedAt); err != nil {
		return operations.ConflictReview{}, err
	}
	var err error
	if item.CurrentValue, err = decodeConflictValue(currentJSON); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("decode current conflict value: %w", err)
	}
	if item.CandidateValue, err = decodeConflictValue(candidateJSON); err != nil {
		return operations.ConflictReview{}, fmt.Errorf("decode candidate conflict value: %w", err)
	}
	return item, nil
}

func decodeConflictValue(data []byte) (any, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}
