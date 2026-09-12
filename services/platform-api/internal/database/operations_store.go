package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"self-deepsearch/services/platform-api/internal/operations"
)

func (store *Store) ListReviewTasks(ctx context.Context, status string, limit int) ([]operations.ReviewTask, error) {
	rows, err := store.pool.Query(ctx, `
SELECT task.review_task_id::text, task.task_type, task.entity_type,
       task.entity_id::text, task.priority, task.status, task.assignee_id::text,
       account.normalized_email,
       CASE task.entity_type
         WHEN 'work' THEN (SELECT work.canonical_code || ' / ' || work.title FROM platform.works work WHERE work.work_id = task.entity_id)
         WHEN 'performer' THEN (SELECT performer.display_name FROM platform.performers performer WHERE performer.performer_id = task.entity_id)
         WHEN 'studio' THEN (SELECT studio.name FROM platform.studios studio WHERE studio.studio_id = task.entity_id)
         ELSE task.entity_type
       END,
       task.due_at, task.claimed_at, task.completed_at, task.created_at, task.updated_at
FROM collector.review_tasks task
LEFT JOIN platform.users account ON account.user_id = task.assignee_id
WHERE (($1 = 'open' AND task.status IN ('pending', 'claimed')) OR task.status = $1)
  AND task.task_type <> 'conflict'
  AND ($1 <> 'approved' OR EXISTS (
    SELECT 1 FROM platform.content_revisions revision
    WHERE revision.entity_type = task.entity_type AND revision.entity_id = task.entity_id
      AND revision.revision_status = 'approved' AND revision.reviewed_at = task.completed_at
      AND NOT EXISTS (
        SELECT 1 FROM platform.publications publication
        WHERE publication.entity_type = revision.entity_type AND publication.entity_id = revision.entity_id
          AND publication.site = 'main' AND publication.current_revision_id = revision.revision_id
      )
  ))
ORDER BY task.priority, task.created_at, task.review_task_id
LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list review tasks: %w", err)
	}
	defer rows.Close()
	items := make([]operations.ReviewTask, 0)
	for rows.Next() {
		var item operations.ReviewTask
		if err := rows.Scan(&item.ID, &item.TaskType, &item.EntityType, &item.EntityID, &item.Priority,
			&item.Status, &item.AssigneeID, &item.Assignee, &item.TargetLabel, &item.DueAt,
			&item.ClaimedAt, &item.CompletedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan review task: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

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

func (store *Store) ClaimReviewTask(ctx context.Context, taskID, actorID, requestID string, now time.Time) (operations.ReviewTask, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("begin claim review task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	command, err := tx.Exec(ctx, `
UPDATE collector.review_tasks
SET status = 'claimed', assignee_id = $2::uuid, claimed_at = $3, completed_at = NULL
WHERE review_task_id = $1::uuid AND status = 'pending' AND task_type <> 'conflict'`, taskID, actorID, now)
	if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("claim review task: %w", err)
	}
	if command.RowsAffected() == 0 {
		var assigneeID *string
		var status string
		if err := tx.QueryRow(ctx, `SELECT assignee_id::text, status FROM collector.review_tasks WHERE review_task_id = $1::uuid AND task_type <> 'conflict'`, taskID).Scan(&assigneeID, &status); errors.Is(err, pgx.ErrNoRows) {
			return operations.ReviewTask{}, operations.ErrNotFound
		} else if err != nil {
			return operations.ReviewTask{}, fmt.Errorf("read review task state: %w", err)
		}
		if status != "claimed" || assigneeID == nil || *assigneeID != actorID {
			return operations.ReviewTask{}, operations.ErrConflict
		}
	}
	if err := insertAudit(ctx, tx, actorID, "review_task.claim", "review_task", taskID, nil, map[string]any{"status": "claimed"}, "", requestID, now); err != nil {
		return operations.ReviewTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.ReviewTask{}, fmt.Errorf("commit claim review task: %w", err)
	}
	return store.reviewTaskByID(ctx, taskID)
}

func (store *Store) ReassignReviewTask(ctx context.Context, taskID, assigneeID, actorID, reason, requestID string, now time.Time) (operations.ReviewTask, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("begin reassign review task: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var previousAssigneeID, previousAssignee *string
	var status string
	var claimedAt *time.Time
	if err := tx.QueryRow(ctx, `
SELECT task.assignee_id::text, account.normalized_email, task.status, task.claimed_at
FROM collector.review_tasks task
LEFT JOIN platform.users account ON account.user_id = task.assignee_id
WHERE task.review_task_id = $1::uuid
  AND task.task_type <> 'conflict'
FOR UPDATE OF task`, taskID).Scan(&previousAssigneeID, &previousAssignee, &status, &claimedAt); errors.Is(err, pgx.ErrNoRows) {
		return operations.ReviewTask{}, operations.ErrNotFound
	} else if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("lock review task for reassignment: %w", err)
	}
	if status != "claimed" || previousAssigneeID == nil || claimedAt == nil {
		return operations.ReviewTask{}, operations.ErrConflict
	}
	if *previousAssigneeID == assigneeID {
		return operations.ReviewTask{}, operations.ErrConflict
	}

	var actorRole, actorStatus string
	if err := tx.QueryRow(ctx, `
SELECT role, account_status FROM platform.users
WHERE user_id = $1::uuid FOR SHARE`, actorID).Scan(&actorRole, &actorStatus); errors.Is(err, pgx.ErrNoRows) {
		return operations.ReviewTask{}, operations.ErrForbidden
	} else if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("read reassignment actor: %w", err)
	}
	if !operations.CanReceiveReviewTask(actorRole, actorStatus) {
		return operations.ReviewTask{}, operations.ErrForbidden
	}

	var nextAssignee string
	var nextRole, nextStatus string
	if err := tx.QueryRow(ctx, `
SELECT normalized_email, role, account_status FROM platform.users
WHERE user_id = $1::uuid FOR SHARE`, assigneeID).Scan(&nextAssignee, &nextRole, &nextStatus); errors.Is(err, pgx.ErrNoRows) {
		return operations.ReviewTask{}, operations.ErrNotFound
	} else if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("read reassignment target: %w", err)
	}
	if !operations.CanReceiveReviewTask(nextRole, nextStatus) {
		return operations.ReviewTask{}, operations.ErrForbidden
	}

	if _, err := tx.Exec(ctx, `
UPDATE collector.review_tasks
SET assignee_id = $2::uuid, claimed_at = $3, updated_at = $3
WHERE review_task_id = $1::uuid`, taskID, assigneeID, now); err != nil {
		return operations.ReviewTask{}, fmt.Errorf("reassign review task: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "review_task.reassign", "review_task", taskID,
		map[string]any{"status": status, "assignee_id": *previousAssigneeID, "assignee": previousAssignee, "claimed_at": claimedAt},
		map[string]any{"status": status, "assignee_id": assigneeID, "assignee": nextAssignee, "claimed_at": now},
		reason, requestID, now); err != nil {
		return operations.ReviewTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.ReviewTask{}, fmt.Errorf("commit review task reassignment: %w", err)
	}
	return store.reviewTaskByID(ctx, taskID)
}

func (store *Store) DecideReviewTask(ctx context.Context, taskID, actorID string, approve bool, reason, requestID string, now time.Time) (operations.ReviewTask, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("begin review decision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entityType *string
	var entityID *string
	var assigneeID *string
	var status string
	if err := tx.QueryRow(ctx, `
SELECT entity_type, entity_id::text, assignee_id::text, status
FROM collector.review_tasks WHERE review_task_id = $1::uuid AND task_type <> 'conflict' FOR UPDATE`, taskID).
		Scan(&entityType, &entityID, &assigneeID, &status); errors.Is(err, pgx.ErrNoRows) {
		return operations.ReviewTask{}, operations.ErrNotFound
	} else if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("lock review task: %w", err)
	}
	assignedTo := ""
	if assigneeID != nil {
		assignedTo = *assigneeID
	}
	if err := operations.ReviewDecisionAccess(status, assignedTo, actorID); err != nil {
		return operations.ReviewTask{}, err
	}
	decision := "rejected"
	revisionStatus := "rejected"
	entityStatus := "draft"
	if approve {
		decision, revisionStatus, entityStatus = "approved", "approved", "approved"
	}
	if entityType != nil && entityID != nil && *entityType != "media" {
		currentStatus, err := readEntityStatus(ctx, tx, *entityType, *entityID)
		if err != nil {
			return operations.ReviewTask{}, err
		}
		if currentStatus == "published" || currentStatus == "hidden" {
			entityStatus = currentStatus
		}
		var revisionID string
		var authorID string
		err = tx.QueryRow(ctx, `
SELECT revision_id::text, author_id::text
FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid AND revision_status = 'reviewing'
ORDER BY version DESC LIMIT 1 FOR UPDATE`, *entityType, *entityID).Scan(&revisionID, &authorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return operations.ReviewTask{}, operations.ErrConflict
		}
		if err != nil {
			return operations.ReviewTask{}, fmt.Errorf("lock reviewing revision: %w", err)
		}
		if authorID == actorID {
			return operations.ReviewTask{}, operations.ErrForbidden
		}
		if _, err := tx.Exec(ctx, `
UPDATE platform.content_revisions
SET revision_status = $2, reviewer_id = $3::uuid, reviewed_at = $4
WHERE revision_id = $1::uuid`, revisionID, revisionStatus, actorID, now); err != nil {
			return operations.ReviewTask{}, fmt.Errorf("decide revision: %w", err)
		}
		if err := setEntityStatus(ctx, tx, *entityType, *entityID, entityStatus); err != nil {
			return operations.ReviewTask{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE collector.review_tasks SET status = $2, completed_at = $3
WHERE review_task_id = $1::uuid`, taskID, decision, now); err != nil {
		return operations.ReviewTask{}, fmt.Errorf("complete review task: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "review_task."+decision, "review_task", taskID,
		map[string]any{"status": status}, map[string]any{"status": decision}, reason, requestID, now); err != nil {
		return operations.ReviewTask{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.ReviewTask{}, fmt.Errorf("commit review decision: %w", err)
	}
	return store.reviewTaskByID(ctx, taskID)
}

func (store *Store) CreateWork(ctx context.Context, input operations.WorkInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create work: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	entity, _, err := createWorkTx(ctx, tx, input, actorID, requestID, now)
	if err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create work: %w", err)
	}
	return entity, nil
}

func createWorkTx(ctx context.Context, tx pgx.Tx, input operations.WorkInput, actorID, requestID string, now time.Time) (operations.Entity, map[string]any, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, nil, operations.ErrInvalidInput
	}
	payload := map[string]any{"canonical_code": input.Code, "compact_code": compactIdentifier(input.Code), "title": input.Title, "performer_ids": input.PerformerIDs}
	putOptional(payload, "title_original", input.TitleOriginal)
	putOptional(payload, "summary", input.Summary)
	putOptional(payload, "studio_id", input.StudioID)
	if input.ReleaseDate != nil {
		payload["release_date"] = input.ReleaseDate.Format(time.DateOnly)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, nil, fmt.Errorf("encode work revision: %w", err)
	}
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
INSERT INTO platform.works (canonical_code, compact_code, title, title_original, release_date, studio_id, summary, publication_status)
VALUES ($1, $2, $3, $4, $5, $6::uuid, $7, 'reviewing')
RETURNING work_id::text, created_at`, input.Code, compactIdentifier(input.Code), input.Title,
		input.TitleOriginal, input.ReleaseDate, input.StudioID, input.Summary).Scan(&entity.ID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, nil, mapConstraintError(err)
	}
	entity.EntityType, entity.Status, entity.RevisionStatus = "work", "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('work', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, nil, fmt.Errorf("create work revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "work", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, nil, err
	}
	for position, performerID := range input.PerformerIDs {
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.work_performers (work_id, performer_id, role, position)
VALUES ($1::uuid, $2::uuid, 'performer', $3)`, entity.ID, performerID, position); err != nil {
			return operations.Entity{}, nil, mapConstraintError(err)
		}
	}
	if err := createReviewTask(ctx, tx, "work", entity.ID, now); err != nil {
		return operations.Entity{}, nil, err
	}
	if err := insertAudit(ctx, tx, actorID, "work.create", "work", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, nil, err
	}
	return entity, payload, nil
}

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

func (store *Store) CreatePerformer(ctx context.Context, input operations.PerformerInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	payload := map[string]any{"display_name": input.DisplayName, "adult_status": input.AdultStatus, "activity_status": input.ActivityStatus}
	payload["aliases"] = input.Aliases
	putOptional(payload, "name_original", input.NameOriginal)
	putOptional(payload, "romanized_name", input.RomanizedName)
	putOptional(payload, "agency", input.Agency)
	putOptional(payload, "birth_year", input.BirthYear)
	putOptional(payload, "height_cm", input.HeightCM)
	putOptional(payload, "measurements", input.Measurements)
	putOptional(payload, "debut_year", input.DebutYear)
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, fmt.Errorf("encode performer revision: %w", err)
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create performer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
WITH identity AS (SELECT gen_random_uuid() AS id)
INSERT INTO platform.performers (
    performer_id, display_name, name_original, romanized_name, canonical_slug, adult_status,
    activity_status, publication_status, agency, birth_year, height_cm, measurements, debut_year
)
SELECT id, $1, $2, $3, 'performer-' || replace(id::text, '-', ''), $4, $5, 'reviewing', $6, $7, $8, $9, $10 FROM identity
RETURNING performer_id::text, canonical_slug, created_at`, input.DisplayName, input.NameOriginal, input.RomanizedName,
		input.AdultStatus, input.ActivityStatus, input.Agency, input.BirthYear, input.HeightCM, input.Measurements, input.DebutYear).
		Scan(&entity.ID, &entity.CanonicalSlug, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	entity.EntityType, entity.Status, entity.RevisionStatus = "performer", "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('performer', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("create performer revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "performer", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if err := replacePerformerAliases(ctx, tx, entity.ID, input.Aliases); err != nil {
		return operations.Entity{}, err
	}
	if err := createReviewTask(ctx, tx, "performer", entity.ID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "performer.create", "performer", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create performer: %w", err)
	}
	return entity, nil
}

func (store *Store) CreateStudio(ctx context.Context, input operations.StudioInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	payload := map[string]any{"name": input.Name, "normalized_name": strings.ToLower(strings.TrimSpace(input.Name))}
	data, err := json.Marshal(payload)
	if err != nil {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create studio: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var entity operations.Entity
	err = tx.QueryRow(ctx, `
INSERT INTO platform.studios (name, normalized_name, canonical_slug, publication_status)
VALUES ($1, $2, $3, 'reviewing')
RETURNING studio_id::text, created_at`, input.Name, payload["normalized_name"], input.CanonicalSlug).Scan(&entity.ID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	entity.EntityType, entity.CanonicalSlug, entity.Status, entity.RevisionStatus = "studio", input.CanonicalSlug, "reviewing", "reviewing"
	if err := tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
VALUES ('studio', $1::uuid, 1, $2::jsonb, 'reviewing', $3::uuid, $4, $5)
RETURNING revision_id::text`, entity.ID, data, actorID, input.Reason, now).Scan(&entity.RevisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("create studio revision: %w", err)
	}
	if err := insertRevisionProvenance(ctx, tx, "studio", entity.ID, entity.RevisionID, payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if err := createReviewTask(ctx, tx, "studio", entity.ID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "studio.create", "studio", entity.ID, nil, payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create studio: %w", err)
	}
	return entity, nil
}

func (store *Store) CreateRevision(ctx context.Context, entityID string, input operations.RevisionInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	if len(input.Sources) == 0 {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	patch, err := json.Marshal(input.Payload)
	if err != nil {
		return operations.Entity{}, operations.ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin create revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockEntity(ctx, tx, input.EntityType, entityID); err != nil {
		return operations.Entity{}, err
	}
	currentStatus, err := readEntityStatus(ctx, tx, input.EntityType, entityID)
	if err != nil {
		return operations.Entity{}, err
	}
	if currentStatus == "takedown" || currentStatus == "merged" {
		return operations.Entity{}, operations.ErrForbidden
	}
	var reviewingExists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM platform.content_revisions revision
  LEFT JOIN platform.publications publication
    ON publication.entity_type = revision.entity_type AND publication.entity_id = revision.entity_id AND publication.site = 'main'
  WHERE revision.entity_type = $1 AND revision.entity_id = $2::uuid
    AND (
      revision.revision_status = 'reviewing'
      OR (revision.revision_status = 'approved'
        AND revision.version = (SELECT max(latest.version) FROM platform.content_revisions latest WHERE latest.entity_type = $1 AND latest.entity_id = $2::uuid)
        AND publication.current_revision_id IS DISTINCT FROM revision.revision_id)
    )
)`, input.EntityType, entityID).Scan(&reviewingExists); err != nil {
		return operations.Entity{}, fmt.Errorf("check pending revision: %w", err)
	}
	if reviewingExists {
		return operations.Entity{}, operations.ErrConflict
	}
	var previousRevisionID string
	if err := tx.QueryRow(ctx, `
SELECT revision_id::text FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid
ORDER BY version DESC LIMIT 1`, input.EntityType, entityID).Scan(&previousRevisionID); errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrNotFound
	} else if err != nil {
		return operations.Entity{}, fmt.Errorf("read previous revision: %w", err)
	}
	data, err := snapshotRevisionPayload(ctx, tx, input.EntityType, entityID, patch)
	if err != nil {
		return operations.Entity{}, err
	}
	var entity operations.Entity
	entity.ID, entity.EntityType, entity.Status, entity.RevisionStatus = entityID, input.EntityType, currentStatus, "reviewing"
	if currentStatus != "published" && currentStatus != "hidden" {
		entity.Status = "reviewing"
	}
	err = tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (entity_type, entity_id, version, payload, revision_status, author_id, reason, submitted_at)
SELECT $1, $2::uuid, coalesce(max(version), 0) + 1, $3::jsonb, 'reviewing', $4::uuid, $5, $6
FROM platform.content_revisions WHERE entity_type = $1 AND entity_id = $2::uuid
RETURNING revision_id::text, created_at`, input.EntityType, entityID, data, actorID, input.Reason, now).
		Scan(&entity.RevisionID, &entity.CreatedAt)
	if err != nil {
		return operations.Entity{}, fmt.Errorf("create revision: %w", err)
	}
	changedFields := sortedPayloadFields(input.Payload)
	if err := copyRevisionProvenance(ctx, tx, previousRevisionID, entity.RevisionID, changedFields); err != nil {
		return operations.Entity{}, err
	}
	if err := insertRevisionProvenance(ctx, tx, input.EntityType, entityID, entity.RevisionID, input.Payload, input.Sources, actorID); err != nil {
		return operations.Entity{}, err
	}
	if currentStatus != "published" && currentStatus != "hidden" {
		if err := setEntityStatus(ctx, tx, input.EntityType, entityID, "reviewing"); err != nil {
			return operations.Entity{}, err
		}
	}
	if err := createReviewTask(ctx, tx, input.EntityType, entityID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "revision.submit", input.EntityType, entityID, nil, input.Payload, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit create revision: %w", err)
	}
	return entity, nil
}

func (store *Store) ListEntities(ctx context.Context, entityType, status string, limit int) ([]operations.CatalogEntity, error) {
	rows, err := store.pool.Query(ctx, `
WITH entities AS (
  SELECT work_id AS entity_id, 'work'::text AS entity_type,
         canonical_code || ' / ' || title AS label, publication_status, updated_at
  FROM platform.works
  UNION ALL
  SELECT performer_id, 'performer', display_name, publication_status, updated_at FROM platform.performers
  UNION ALL
  SELECT studio_id, 'studio', name, publication_status, updated_at FROM platform.studios
)
SELECT entity.entity_id::text, entity.entity_type, entity.label, entity.publication_status,
       publication.canonical_slug, publication.current_revision_id::text, entity.updated_at
FROM entities entity
LEFT JOIN platform.publications publication
  ON publication.entity_type = entity.entity_type AND publication.entity_id = entity.entity_id AND publication.site = 'main'
WHERE ($1 = 'all' OR entity.entity_type = $1)
  AND ($2 = 'all' OR entity.publication_status = $2)
ORDER BY entity.updated_at DESC, entity.entity_type, entity.entity_id
LIMIT $3`, entityType, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close()
	items := make([]operations.CatalogEntity, 0)
	for rows.Next() {
		var item operations.CatalogEntity
		if err := rows.Scan(&item.ID, &item.EntityType, &item.Label, &item.Status,
			&item.CanonicalSlug, &item.CurrentRevisionID, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan entity: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ListRevisions(ctx context.Context, entityID, entityType string, limit int) ([]operations.Revision, error) {
	rows, err := store.pool.Query(ctx, `
SELECT revision.revision_id::text, revision.entity_type, revision.entity_id::text, revision.version,
       revision.payload, revision.revision_status, author.normalized_email,
       reviewer.normalized_email, revision.reason, revision.created_at, revision.reviewed_at,
	       coalesce((
	         SELECT jsonb_agg(jsonb_build_object(
	           'field_name', provenance.field_name,
	           'source_type', provenance.source_type,
	           'source_url', provenance.source_url,
	           'source_title', provenance.source_title,
	           'checked_at', provenance.checked_at,
	           'operator', operator.normalized_email,
	           'confidence', provenance.confidence,
	           'rights_status', provenance.rights_status
	         ) ORDER BY provenance.field_name, provenance.checked_at DESC, provenance.provenance_id)
	         FROM platform.field_provenance provenance
	         LEFT JOIN platform.users operator ON operator.user_id = provenance.operator_id
	         WHERE provenance.revision_id = revision.revision_id
	       ), '[]'::jsonb),
	       coalesce(publication.current_revision_id = revision.revision_id, false), publication.canonical_slug
FROM platform.content_revisions revision
JOIN platform.users author ON author.user_id = revision.author_id
LEFT JOIN platform.users reviewer ON reviewer.user_id = revision.reviewer_id
LEFT JOIN platform.publications publication
  ON publication.entity_type = revision.entity_type AND publication.entity_id = revision.entity_id AND publication.site = 'main'
WHERE revision.entity_type = $1 AND revision.entity_id = $2::uuid
ORDER BY revision.version DESC LIMIT $3`, entityType, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("list revisions: %w", err)
	}
	defer rows.Close()
	items := make([]operations.Revision, 0)
	for rows.Next() {
		var item operations.Revision
		var payload, sources []byte
		if err := rows.Scan(&item.ID, &item.EntityType, &item.EntityID, &item.Version, &payload,
			&item.Status, &item.Author, &item.Reviewer, &item.Reason, &item.CreatedAt,
			&item.ReviewedAt, &sources, &item.IsCurrent, &item.CanonicalSlug); err != nil {
			return nil, fmt.Errorf("scan revision: %w", err)
		}
		if err := json.Unmarshal(payload, &item.Payload); err != nil {
			return nil, fmt.Errorf("decode revision payload: %w", err)
		}
		if err := json.Unmarshal(sources, &item.Sources); err != nil {
			return nil, fmt.Errorf("decode revision provenance: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) PublishEntity(ctx context.Context, entityID string, input operations.PublishInput, actorID, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin publish entity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockEntity(ctx, tx, input.EntityType, entityID); err != nil {
		return operations.Entity{}, err
	}
	table, idColumn, _ := entityTable(input.EntityType)
	statusQuery := fmt.Sprintf("SELECT publication_status FROM platform.%s WHERE %s = $1::uuid", table, idColumn)
	var entityStatus string
	if err := tx.QueryRow(ctx, statusQuery, entityID).Scan(&entityStatus); err != nil {
		return operations.Entity{}, fmt.Errorf("read publication status: %w", err)
	}
	if entityStatus == "takedown" {
		return operations.Entity{}, operations.ErrForbidden
	}
	var revisionID string
	var payload []byte
	var revisionAuthorID string
	query := `SELECT revision_id::text, payload FROM platform.content_revisions
WHERE entity_type = $1 AND entity_id = $2::uuid AND revision_status = 'approved'`
	args := []any{input.EntityType, entityID}
	if input.RevisionID != "" {
		query += ` AND revision_id = $3::uuid`
		args = append(args, input.RevisionID)
	}
	query = strings.Replace(query, `SELECT revision_id::text, payload`, `SELECT revision_id::text, payload, author_id::text`, 1)
	query += ` ORDER BY version DESC LIMIT 1 FOR UPDATE`
	if err := tx.QueryRow(ctx, query, args...).Scan(&revisionID, &payload, &revisionAuthorID); errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrConflict
	} else if err != nil {
		return operations.Entity{}, fmt.Errorf("lock approved revision: %w", err)
	}
	complete, err := revisionProvenanceComplete(ctx, tx, revisionID)
	if err != nil {
		return operations.Entity{}, err
	}
	if !complete {
		return operations.Entity{}, operations.ErrConflict
	}
	if err := applyRevision(ctx, tx, input.EntityType, entityID, payload); err != nil {
		return operations.Entity{}, err
	}
	if input.EntityType == "performer" || input.EntityType == "studio" {
		table, idColumn, _ := entityTable(input.EntityType)
		query := fmt.Sprintf("UPDATE platform.%s SET canonical_slug = $2 WHERE %s = $1::uuid", table, idColumn)
		if _, err := tx.Exec(ctx, query, entityID, input.CanonicalSlug); err != nil {
			return operations.Entity{}, mapConstraintError(err)
		}
	}
	var previousRevisionID *string
	if err := tx.QueryRow(ctx, `
SELECT current_revision_id::text FROM platform.publications
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main'`, input.EntityType, entityID).Scan(&previousRevisionID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, fmt.Errorf("read current publication revision: %w", err)
	}
	rollbackSourceRevisionID := ""
	isRollback := input.RevisionID != "" && previousRevisionID != nil && *previousRevisionID != revisionID
	if isRollback {
		// A rollback creates a new immutable approved revision. Preserve the
		// selected revision's author as provenance and record the operator as
		// reviewer. The schema intentionally forbids author == reviewer, so do
		// not let an operator approve their own historical revision.
		if revisionAuthorID == actorID {
			return operations.Entity{}, operations.ErrForbidden
		}
		rollbackSourceRevisionID = revisionID
		err = tx.QueryRow(ctx, `
INSERT INTO platform.content_revisions (
  entity_type, entity_id, version, payload, revision_status, author_id, reviewer_id,
  reason, submitted_at, reviewed_at
)
SELECT $1, $2::uuid, coalesce(max(version), 0) + 1, $3::jsonb, 'approved',
       $4::uuid, $5::uuid, $6, $7, $7
FROM platform.content_revisions WHERE entity_type = $1 AND entity_id = $2::uuid
RETURNING revision_id::text`, input.EntityType, entityID, payload, revisionAuthorID, actorID, input.Reason, now).Scan(&revisionID)
		if err != nil {
			return operations.Entity{}, fmt.Errorf("create rollback revision: %w", err)
		}
		if err := copyRevisionProvenance(ctx, tx, rollbackSourceRevisionID, revisionID, nil); err != nil {
			return operations.Entity{}, err
		}
	}
	var publicationID string
	err = tx.QueryRow(ctx, `
INSERT INTO platform.publications (
    entity_type, entity_id, site, current_revision_id, publication_status, canonical_slug, published_at
) VALUES ($1, $2::uuid, 'main', $3::uuid, 'published', $4, $5)
ON CONFLICT (entity_type, entity_id, site) DO UPDATE
SET current_revision_id = EXCLUDED.current_revision_id,
    publication_status = 'published', canonical_slug = EXCLUDED.canonical_slug,
    published_at = EXCLUDED.published_at, hidden_at = NULL, takedown_at = NULL
RETURNING publication_id::text`, input.EntityType, entityID, revisionID, input.CanonicalSlug, now).Scan(&publicationID)
	if err != nil {
		return operations.Entity{}, mapConstraintError(err)
	}
	if err := refreshSearchDocument(ctx, tx, input.EntityType, entityID, publicationID, now); err != nil {
		return operations.Entity{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1, $2::uuid, 'publication_changed', jsonb_build_object('entity_type', $1, 'entity_id', $2, 'slug', $3), $4)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, input.EntityType, entityID, input.CanonicalSlug, revisionID); err != nil {
		return operations.Entity{}, fmt.Errorf("enqueue publication side effects: %w", err)
	}
	action := "entity.publish"
	if isRollback {
		action = "entity.rollback"
	}
	if err := insertAudit(ctx, tx, actorID, action, input.EntityType, entityID,
		map[string]any{"revision_id": previousRevisionID},
		map[string]any{"revision_id": revisionID, "rollback_source_revision_id": rollbackSourceRevisionID, "canonical_slug": input.CanonicalSlug}, input.Reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit publish entity: %w", err)
	}
	return operations.Entity{ID: entityID, EntityType: input.EntityType, CanonicalSlug: input.CanonicalSlug,
		Status: "published", RevisionID: revisionID, RevisionStatus: "approved", CreatedAt: now}, nil
}

func (store *Store) HideEntity(ctx context.Context, entityID, entityType, actorID, reason, requestID string, now time.Time) (operations.Entity, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.Entity{}, fmt.Errorf("begin hide entity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var revisionID, slug string
	err = tx.QueryRow(ctx, `
UPDATE platform.publications
SET publication_status = 'hidden', hidden_at = $3, published_at = NULL, takedown_at = NULL
WHERE entity_type = $1 AND entity_id = $2::uuid AND site = 'main' AND publication_status = 'published'
RETURNING current_revision_id::text, canonical_slug`, entityType, entityID, now).Scan(&revisionID, &slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.Entity{}, operations.ErrConflict
	}
	if err != nil {
		return operations.Entity{}, fmt.Errorf("hide publication: %w", err)
	}
	if err := setEntityStatus(ctx, tx, entityType, entityID, "hidden"); err != nil {
		return operations.Entity{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ($1, $2::uuid, 'publication_hidden', jsonb_build_object('entity_type', $1, 'entity_id', $2), $3)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, entityType, entityID, revisionID+":"+now.Format(time.RFC3339Nano)); err != nil {
		return operations.Entity{}, fmt.Errorf("enqueue hide side effects: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "entity.hide", entityType, entityID,
		map[string]any{"status": "published"}, map[string]any{"status": "hidden"}, reason, requestID, now); err != nil {
		return operations.Entity{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.Entity{}, fmt.Errorf("commit hide entity: %w", err)
	}
	return operations.Entity{ID: entityID, EntityType: entityType, CanonicalSlug: slug,
		Status: "hidden", RevisionID: revisionID, RevisionStatus: "approved", CreatedAt: now}, nil
}

func (store *Store) ListUsers(ctx context.Context, limit int) ([]operations.User, error) {
	rows, err := store.pool.Query(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at
FROM platform.users ORDER BY created_at DESC, user_id LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	items := make([]operations.User, 0)
	for rows.Next() {
		var item operations.User
		if err := rows.Scan(&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ChangeUserRole(ctx context.Context, userID, role, actorID, requestID string, now time.Time) (operations.User, error) {
	return changeUserRole(ctx, store.pool.BeginTx, userID, role, actorID, requestID, now)
}

// Keep transaction orchestration testable without granting tests a live
// database. SQL and locking semantics still need the PostgreSQL contract.
func changeUserRole(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), userID, role, actorID, requestID string, now time.Time) (operations.User, error) {
	if userID == actorID || role == "owner" {
		return operations.User{}, operations.ErrForbidden
	}
	if role != "admin" && role != "editor" && role != "user" {
		return operations.User{}, operations.ErrInvalidInput
	}
	tx, err := begin(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.User{}, fmt.Errorf("begin role change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item operations.User
	var oldRole, actorRole string
	if err = tx.QueryRow(ctx, `
SELECT role FROM platform.users
WHERE user_id = $1::uuid AND account_status <> 'closed'
FOR UPDATE`, userID).Scan(&oldRole); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	} else if err != nil {
		return operations.User{}, fmt.Errorf("load target user role: %w", err)
	}
	// Keep the owner boundary in the repository as well as the HTTP layer. This
	// prevents a future internal caller from changing an owner or using a stale
	// operator session to mutate roles.
	if err = tx.QueryRow(ctx, `
SELECT role FROM platform.users
WHERE user_id = $1::uuid AND account_status = 'active'`, actorID).Scan(&actorRole); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrForbidden
	} else if err != nil {
		return operations.User{}, fmt.Errorf("load actor role: %w", err)
	}
	if !operations.CanManageUserRole(actorRole, oldRole, userID == actorID) {
		return operations.User{}, operations.ErrForbidden
	}
	if oldRole == role {
		return operations.User{}, operations.ErrConflict
	}
	err = tx.QueryRow(ctx, `
UPDATE platform.users SET role = $2
WHERE user_id = $1::uuid AND account_status <> 'closed'
RETURNING user_id::text, normalized_email, role, account_status, created_at, last_login_at`, userID, role).Scan(
		&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.User{}, mapConstraintError(err)
	}
	// CreateSession locks this same account row before verifying the role
	// snapshot and issuing a token. A login issued first is revoked here; a
	// stale login waiting behind this transaction cannot issue a new token.
	// Preserve earlier revocation evidence and the session time constraint.
	revoked, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'role_change'
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, now)
	if err != nil {
		return operations.User{}, fmt.Errorf("revoke role-change sessions: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "user.role_change", "user", userID,
		map[string]any{"role": oldRole}, map[string]any{"role": role, "sessions_revoked": revoked.RowsAffected()}, "owner role management", requestID, now); err != nil {
		return operations.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.User{}, fmt.Errorf("commit role change: %w", err)
	}
	return item, nil
}

func (store *Store) ChangeUserStatus(ctx context.Context, userID, status, actorID, actorRole, reason, requestID string, now time.Time) (operations.User, error) {
	if userID == actorID {
		return operations.User{}, operations.ErrForbidden
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return operations.User{}, fmt.Errorf("begin user status change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item operations.User
	var previousStatus string
	if err := tx.QueryRow(ctx, `
SELECT user_id::text, normalized_email, role, account_status, created_at, last_login_at
FROM platform.users WHERE user_id = $1::uuid AND account_status <> 'closed' FOR UPDATE`, userID).Scan(
		&item.ID, &item.Email, &item.Role, &previousStatus, &item.CreatedAt, &item.LastLoginAt,
	); errors.Is(err, pgx.ErrNoRows) {
		return operations.User{}, operations.ErrNotFound
	} else if err != nil {
		return operations.User{}, err
	}
	if !operations.CanManageUserStatus(actorRole, item.Role, userID == actorID) ||
		(previousStatus != "active" && previousStatus != "locked" && previousStatus != "suspended") {
		return operations.User{}, operations.ErrForbidden
	}
	if previousStatus == status {
		return operations.User{}, operations.ErrConflict
	}
	if err := tx.QueryRow(ctx, `
UPDATE platform.users SET account_status = $2
WHERE user_id = $1::uuid
RETURNING user_id::text, normalized_email, role, account_status, created_at, last_login_at`, userID, status).Scan(
		&item.ID, &item.Email, &item.Role, &item.AccountStatus, &item.CreatedAt, &item.LastLoginAt,
	); err != nil {
		return operations.User{}, err
	}
	if status != "active" {
		if _, err := tx.Exec(ctx, `
UPDATE platform.sessions SET revoked_at = greatest($2, created_at), revoke_reason = 'account_' || $3
WHERE user_id = $1::uuid AND revoked_at IS NULL`, userID, now, status); err != nil {
			return operations.User{}, err
		}
	}
	if err := insertAudit(ctx, tx, actorID, "user.status_change", "user", userID,
		map[string]any{"account_status": previousStatus}, map[string]any{"account_status": status}, reason, requestID, now); err != nil {
		return operations.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.User{}, fmt.Errorf("commit user status change: %w", err)
	}
	return item, nil
}

func (store *Store) SystemHealth(ctx context.Context, now time.Time) (operations.SystemHealth, error) {
	var result operations.SystemHealth
	result.CheckedAt = now
	err := store.pool.QueryRow(ctx, `
SELECT metadata.schema_version,
       (SELECT count(*) FROM collector.review_tasks WHERE status IN ('pending', 'claimed')),
       (SELECT count(*) FROM platform.content_revisions WHERE revision_status = 'reviewing'),
       (SELECT count(*) FROM platform.outbox_events WHERE status IN ('pending', 'retry', 'running')),
       (SELECT count(*) FROM platform.outbox_events WHERE status = 'dead'),
       (SELECT coalesce(sum(byte_size), 0) FROM platform.media_objects WHERE object_status <> 'deleted'),
       (SELECT extract(epoch FROM ($1 - max(coalesce(restore_verified_at, copied_at, completed_at))))
        FROM audit.backup_runs WHERE backup_status = 'verified'),
       (SELECT extract(epoch FROM ($1 - max(completed_at)))
        FROM audit.media_reconciliation_runs WHERE run_status = 'completed'),
       coalesce((SELECT missing_s3_count::bigint + corrupt_s3_count + missing_backup_count +
                        corrupt_backup_count + orphan_s3_count + orphan_backup_count
                 FROM audit.media_reconciliation_runs
                 WHERE run_status = 'completed' ORDER BY completed_at DESC LIMIT 1), 0),
       (SELECT count(*) FROM audit.media_reconciliation_runs
        WHERE run_status = 'failed' AND started_at >= $1 - interval '24 hours'),
       (SELECT greatest(0, extract(epoch FROM ($1 - completed_at)))
        FROM audit.media_inspection_runs WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1),
       coalesce((SELECT publication_issues FROM audit.media_inspection_runs
        WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1), 0),
       coalesce((SELECT default_failures FROM audit.media_inspection_runs
        WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1), 0),
       (SELECT count(*) FROM audit.media_inspection_runs
        WHERE run_status = 'failed' AND completed_at >= $1 - interval '24 hours'),
       coalesce((SELECT sum(search_requests) FROM platform.search_metrics_hourly
        WHERE hour_bucket >= date_trunc('hour', $1 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '24 hours'), 0),
       coalesce((SELECT sum(zero_result_requests) FROM platform.search_metrics_hourly
        WHERE hour_bucket >= date_trunc('hour', $1 AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '24 hours'), 0)
FROM platform.system_metadata metadata WHERE singleton`, now).Scan(&result.SchemaVersion, &result.PendingReviewTasks,
		&result.ReviewingRevisions, &result.PendingOutboxEvents, &result.FailedOutboxEvents,
		&result.MediaBytes, &result.VerifiedBackupAgeSeconds, &result.MediaReconcileAgeSeconds,
		&result.MediaReconcileIssues, &result.FailedMediaReconciles,
		&result.MediaInspectionAgeSeconds, &result.MediaPublicationIssues, &result.DefaultImageFailures, &result.FailedMediaInspections,
		&result.SearchRequests24h, &result.SearchZeroResults24h)
	if err != nil {
		return operations.SystemHealth{}, fmt.Errorf("read system health: %w", err)
	}
	return result, nil
}

func (store *Store) ListAuditLogs(ctx context.Context, requestID string, limit int) ([]operations.AuditLog, error) {
	if strings.TrimSpace(requestID) == "" || limit < 1 || limit > 201 {
		return nil, operations.ErrInvalidInput
	}
	rows, err := store.pool.Query(ctx, `
SELECT log.audit_id::text, log.actor_type, log.actor_id::text, actor.normalized_email,
       log.action, log.object_type, log.object_id::text, left(log.reason, 1000), log.request_id, log.occurred_at
FROM audit.audit_logs log
LEFT JOIN platform.users actor ON actor.user_id = log.actor_id
WHERE log.request_id = $1
ORDER BY log.occurred_at, log.audit_id
LIMIT $2`, requestID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()
	items := make([]operations.AuditLog, 0)
	for rows.Next() {
		var item operations.AuditLog
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.Actor, &item.Action,
			&item.ObjectType, &item.ObjectID, &item.Reason, &item.RequestID, &item.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit logs: %w", err)
	}
	return items, nil
}

func (store *Store) RegisterMediaManifest(ctx context.Context, input operations.MediaManifestInput, actorID, requestID string, now time.Time) (operations.MediaManifest, error) {
	return registerMediaManifest(ctx, store.pool.BeginTx, input, actorID, requestID, now)
}

func registerMediaManifest(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), input operations.MediaManifestInput, actorID, requestID string, now time.Time) (operations.MediaManifest, error) {
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
    confidence, rights_status, asset_status
) VALUES ($1::uuid, $2, 1, $3, $4, $5, $6::uuid, $7, 'allowed', 'published')
RETURNING asset_id::text, created_at`, input.AssetID, input.AssetType, input.SourceType, input.SourceURL,
		input.CheckedAt, actorID, input.Confidence).Scan(&result.AssetID, &result.CreatedAt)
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
) VALUES ($1::uuid, $2::uuid, 'webp', jsonb_build_object('rendition', $3), $4)`,
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
        jsonb_build_object('entity_type', $1, 'entity_id', $2, 'asset_id', $3::text), 'media-publish:' || $3::text)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, input.EntityType, input.EntityID, result.AssetID); err != nil {
		return operations.MediaManifest{}, fmt.Errorf("enqueue media publication refresh: %w", err)
	}
	if err := insertAudit(ctx, tx, actorID, "media.manifest_publish", "media_asset", result.AssetID, nil,
		map[string]any{"entity_type": input.EntityType, "entity_id": input.EntityID, "object_count": len(input.Objects)}, input.Reason, requestID, now); err != nil {
		return operations.MediaManifest{}, err
	}
	return result, nil
}

func validMediaAssociation(input operations.MediaManifestInput) bool {
	return (input.EntityType == "work" && input.AssetType == "work_image" && input.Purpose == "cover" && input.IsPrimary && input.Position == 0) ||
		(input.EntityType == "work" && input.AssetType == "work_image" && input.Purpose == "gallery" && !input.IsPrimary && input.Position >= 1 && input.Position <= 3) ||
		(input.EntityType == "performer" && input.AssetType == "performer_avatar" && input.Purpose == "avatar" && input.IsPrimary && input.Position == 0)
}

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
VALUES ($1, $2::uuid, 'publication_takedown', jsonb_build_object('entity_type', $1, 'entity_id', $2), $3)
ON CONFLICT (event_type, dedupe_key) DO NOTHING`, entityType, entityID, takedownID)
	if err != nil {
		return fmt.Errorf("enqueue takedown side effects: %w", err)
	}
	return nil
}

func (store *Store) reviewTaskByID(ctx context.Context, taskID string) (operations.ReviewTask, error) {
	var item operations.ReviewTask
	err := store.pool.QueryRow(ctx, `
SELECT task.review_task_id::text, task.task_type, task.entity_type,
       task.entity_id::text, task.priority, task.status, task.assignee_id::text,
       account.normalized_email,
       coalesce(CASE task.entity_type
         WHEN 'work' THEN (SELECT work.canonical_code || ' / ' || work.title FROM platform.works work WHERE work.work_id = task.entity_id)
         WHEN 'performer' THEN (SELECT performer.display_name FROM platform.performers performer WHERE performer.performer_id = task.entity_id)
         WHEN 'studio' THEN (SELECT studio.name FROM platform.studios studio WHERE studio.studio_id = task.entity_id)
         ELSE task.entity_type
       END, ''),
       task.due_at, task.claimed_at, task.completed_at, task.created_at, task.updated_at
FROM collector.review_tasks task
LEFT JOIN platform.users account ON account.user_id = task.assignee_id
WHERE task.review_task_id = $1::uuid AND task.task_type <> 'conflict'`, taskID).Scan(
		&item.ID, &item.TaskType, &item.EntityType, &item.EntityID, &item.Priority,
		&item.Status, &item.AssigneeID, &item.Assignee, &item.TargetLabel, &item.DueAt,
		&item.ClaimedAt, &item.CompletedAt, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.ReviewTask{}, operations.ErrNotFound
	}
	if err != nil {
		return operations.ReviewTask{}, fmt.Errorf("read review task: %w", err)
	}
	return item, nil
}

func createReviewTask(ctx context.Context, tx pgx.Tx, entityType, entityID string, now time.Time) error {
	_, err := tx.Exec(ctx, `
INSERT INTO collector.review_tasks (task_type, entity_type, entity_id, priority, status, created_at, updated_at)
VALUES ('publication', $1, $2::uuid, 100, 'pending', $3, $3)`, entityType, entityID, now)
	if err != nil {
		return fmt.Errorf("create review task: %w", err)
	}
	return nil
}

func lockEntity(ctx context.Context, tx pgx.Tx, entityType, entityID string) error {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return err
	}
	var id string
	query := fmt.Sprintf("SELECT %s::text FROM platform.%s WHERE %s = $1::uuid FOR UPDATE", idColumn, table, idColumn)
	if err := tx.QueryRow(ctx, query, entityID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return operations.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("lock %s: %w", entityType, err)
	}
	return nil
}

func setEntityStatus(ctx context.Context, tx pgx.Tx, entityType, entityID, status string) error {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return err
	}
	query := fmt.Sprintf("UPDATE platform.%s SET publication_status = $2 WHERE %s = $1::uuid", table, idColumn)
	command, err := tx.Exec(ctx, query, entityID, status)
	if err != nil {
		return fmt.Errorf("set %s status: %w", entityType, err)
	}
	if command.RowsAffected() == 0 {
		return operations.ErrNotFound
	}
	return nil
}

func readEntityStatus(ctx context.Context, tx pgx.Tx, entityType, entityID string) (string, error) {
	table, idColumn, err := entityTable(entityType)
	if err != nil {
		return "", err
	}
	query := fmt.Sprintf("SELECT publication_status FROM platform.%s WHERE %s = $1::uuid", table, idColumn)
	var status string
	if err := tx.QueryRow(ctx, query, entityID).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		return "", operations.ErrNotFound
	} else if err != nil {
		return "", fmt.Errorf("read %s status: %w", entityType, err)
	}
	return status, nil
}

func entityTable(entityType string) (string, string, error) {
	switch entityType {
	case "work":
		return "works", "work_id", nil
	case "performer":
		return "performers", "performer_id", nil
	case "studio":
		return "studios", "studio_id", nil
	default:
		return "", "", operations.ErrInvalidInput
	}
}

func snapshotRevisionPayload(ctx context.Context, tx pgx.Tx, entityType, entityID string, patch []byte) ([]byte, error) {
	var query string
	switch entityType {
	case "work":
		query = `SELECT jsonb_build_object(
  'canonical_code', canonical_code, 'compact_code', compact_code, 'title', title,
  'title_original', title_original, 'release_date', release_date,
  'studio_id', studio_id, 'summary', summary,
  'performer_ids', coalesce((SELECT jsonb_agg(link.performer_id ORDER BY link.position, link.performer_id)
    FROM platform.work_performers link WHERE link.work_id = works.work_id), '[]'::jsonb)
) || $2::jsonb FROM platform.works WHERE work_id = $1::uuid`
	case "performer":
		query = `SELECT jsonb_build_object(
  'display_name', display_name, 'name_original', name_original, 'romanized_name', romanized_name,
  'adult_status', adult_status, 'activity_status', activity_status, 'agency', agency,
  'birth_year', birth_year, 'height_cm', height_cm, 'measurements', measurements, 'debut_year', debut_year,
  'aliases', coalesce((SELECT jsonb_agg(alias.raw_value ORDER BY alias.raw_value)
    FROM platform.performer_aliases alias WHERE alias.performer_id = performers.performer_id), '[]'::jsonb)
) || $2::jsonb FROM platform.performers WHERE performer_id = $1::uuid`
	case "studio":
		query = `SELECT jsonb_build_object('name', name, 'normalized_name', normalized_name) || $2::jsonb
FROM platform.studios WHERE studio_id = $1::uuid`
	default:
		return nil, operations.ErrInvalidInput
	}
	var snapshot []byte
	if err := tx.QueryRow(ctx, query, entityID, patch).Scan(&snapshot); errors.Is(err, pgx.ErrNoRows) {
		return nil, operations.ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("snapshot %s revision: %w", entityType, err)
	}
	return snapshot, nil
}

func applyRevision(ctx context.Context, tx pgx.Tx, entityType, entityID string, payload []byte) error {
	var rowsAffected int64
	switch entityType {
	case "work":
		command, err := tx.Exec(ctx, `
UPDATE platform.works SET
 canonical_code = coalesce($2::jsonb->>'canonical_code', canonical_code),
 compact_code = coalesce($2::jsonb->>'compact_code', compact_code),
 title = coalesce($2::jsonb->>'title', title),
 title_original = CASE WHEN $2::jsonb ? 'title_original' THEN nullif($2::jsonb->>'title_original', '') ELSE title_original END,
 release_date = CASE WHEN $2::jsonb ? 'release_date' THEN nullif($2::jsonb->>'release_date', '')::date ELSE release_date END,
 studio_id = CASE WHEN $2::jsonb ? 'studio_id' THEN nullif($2::jsonb->>'studio_id', '')::uuid ELSE studio_id END,
 summary = CASE WHEN $2::jsonb ? 'summary' THEN nullif($2::jsonb->>'summary', '') ELSE summary END,
 publication_status = 'published'
WHERE work_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
		if rowsAffected > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM platform.work_performers WHERE work_id = $1::uuid AND $2::jsonb ? 'performer_ids'`, entityID, payload); err != nil {
				return fmt.Errorf("replace work performers: %w", err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO platform.work_performers (work_id, performer_id, role, position)
SELECT $1::uuid, value::uuid, 'performer', ordinal - 1
FROM jsonb_array_elements_text(coalesce($2::jsonb->'performer_ids', '[]'::jsonb)) WITH ORDINALITY entry(value, ordinal)`, entityID, payload); err != nil {
				return mapConstraintError(err)
			}
		}
	case "performer":
		command, err := tx.Exec(ctx, `
UPDATE platform.performers SET
 display_name = coalesce($2::jsonb->>'display_name', display_name),
 name_original = CASE WHEN $2::jsonb ? 'name_original' THEN nullif($2::jsonb->>'name_original', '') ELSE name_original END,
 romanized_name = CASE WHEN $2::jsonb ? 'romanized_name' THEN nullif($2::jsonb->>'romanized_name', '') ELSE romanized_name END,
 adult_status = coalesce($2::jsonb->>'adult_status', adult_status),
 activity_status = coalesce($2::jsonb->>'activity_status', activity_status),
 agency = CASE WHEN $2::jsonb ? 'agency' THEN nullif($2::jsonb->>'agency', '') ELSE agency END,
 birth_year = CASE WHEN $2::jsonb ? 'birth_year' THEN nullif($2::jsonb->>'birth_year', '')::smallint ELSE birth_year END,
 height_cm = CASE WHEN $2::jsonb ? 'height_cm' THEN nullif($2::jsonb->>'height_cm', '')::smallint ELSE height_cm END,
 measurements = CASE WHEN $2::jsonb ? 'measurements' THEN nullif($2::jsonb->>'measurements', '') ELSE measurements END,
 debut_year = CASE WHEN $2::jsonb ? 'debut_year' THEN nullif($2::jsonb->>'debut_year', '')::smallint ELSE debut_year END,
 publication_status = 'published'
WHERE performer_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
		if rowsAffected > 0 {
			var aliases []string
			var hasAliases bool
			var patch map[string]json.RawMessage
			if err := json.Unmarshal(payload, &patch); err != nil {
				return operations.ErrInvalidInput
			}
			if rawAliases, exists := patch["aliases"]; exists {
				hasAliases = true
				if err := json.Unmarshal(rawAliases, &aliases); err != nil {
					return operations.ErrInvalidInput
				}
			}
			if hasAliases {
				if err := replacePerformerAliases(ctx, tx, entityID, aliases); err != nil {
					return err
				}
			}
		}
	case "studio":
		command, err := tx.Exec(ctx, `
UPDATE platform.studios SET
 name = coalesce($2::jsonb->>'name', name),
 normalized_name = coalesce($2::jsonb->>'normalized_name', normalized_name),
 publication_status = 'published'
WHERE studio_id = $1::uuid`, entityID, payload)
		if err != nil {
			return mapConstraintError(err)
		}
		rowsAffected = command.RowsAffected()
	default:
		return operations.ErrInvalidInput
	}
	if rowsAffected == 0 {
		return operations.ErrNotFound
	}
	return nil
}

func replacePerformerAliases(ctx context.Context, tx pgx.Tx, performerID string, aliases []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM platform.performer_aliases WHERE performer_id = $1::uuid`, performerID); err != nil {
		return fmt.Errorf("replace performer aliases: %w", err)
	}
	for _, alias := range aliases {
		normalized := strings.ToLower(strings.Join(strings.Fields(alias), " "))
		if _, err := tx.Exec(ctx, `
INSERT INTO platform.performer_aliases (performer_id, alias_type, raw_value, normalized_value)
VALUES ($1::uuid, 'stage_name', $2, $3)`, performerID, alias, normalized); err != nil {
			return mapConstraintError(err)
		}
	}
	return nil
}

func insertRevisionProvenance(
	ctx context.Context,
	tx pgx.Tx,
	entityType, entityID, revisionID string,
	payload map[string]any,
	sources []operations.SourceEvidenceInput,
	actorID string,
) error {
	fields := sortedPayloadFields(payload)
	if len(fields) == 0 || len(sources) == 0 {
		return operations.ErrInvalidInput
	}
	for _, field := range fields {
		for _, source := range sources {
			if _, err := tx.Exec(ctx, `
INSERT INTO platform.field_provenance (
  entity_type, entity_id, revision_id, field_name, source_id, source_type,
  source_url, source_title, checked_at, operator_id, confidence, rights_status
) VALUES ($1, $2::uuid, $3::uuid, $4, $5::uuid, $6, $7, $8, $9, $10::uuid, 1, 'needs_review')`,
				entityType, entityID, revisionID, field, source.SourceID, source.SourceType,
				source.SourceURL, source.SourceTitle, source.CheckedAt, actorID); err != nil {
				return mapConstraintError(err)
			}
		}
	}
	complete, err := revisionProvenanceComplete(ctx, tx, revisionID)
	if err != nil {
		return err
	}
	if !complete {
		return operations.ErrInvalidInput
	}
	return nil
}

func copyRevisionProvenance(ctx context.Context, tx pgx.Tx, sourceRevisionID, targetRevisionID string, excludedFields []string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO platform.field_provenance (
  entity_type, entity_id, revision_id, field_name, source_id, source_type,
  source_url, source_title, checked_at, operator_id, confidence, rights_status
)
SELECT entity_type, entity_id, $2::uuid, field_name, source_id, source_type,
       source_url, source_title, checked_at, operator_id, confidence, rights_status
FROM platform.field_provenance
WHERE revision_id = $1::uuid
  AND NOT (field_name = ANY(coalesce($3::text[], ARRAY[]::text[])))`, sourceRevisionID, targetRevisionID, excludedFields); err != nil {
		return fmt.Errorf("copy revision provenance: %w", err)
	}
	return nil
}

func revisionProvenanceComplete(ctx context.Context, tx pgx.Tx, revisionID string) (bool, error) {
	var complete bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM platform.content_revisions WHERE revision_id = $1::uuid
) AND NOT EXISTS (
  SELECT 1
  FROM platform.content_revisions revision
  CROSS JOIN LATERAL jsonb_object_keys(revision.payload) field(field_name)
  WHERE revision.revision_id = $1::uuid
    AND NOT EXISTS (
      SELECT 1 FROM platform.field_provenance provenance
      WHERE provenance.revision_id = revision.revision_id
        AND provenance.field_name = field.field_name
    )
)`, revisionID).Scan(&complete); err != nil {
		return false, fmt.Errorf("check revision provenance: %w", err)
	}
	return complete, nil
}

func sortedPayloadFields(payload map[string]any) []string {
	fields := make([]string, 0, len(payload))
	for field := range payload {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}

func stringPointer(value string) *string {
	return &value
}

func refreshSearchDocument(ctx context.Context, tx pgx.Tx, entityType, entityID, publicationID string, now time.Time) error {
	var query string
	switch entityType {
	case "work":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, canonical_code, compact_code, title, names, aliases, studio_name, publication_id, updated_at)
SELECT 'work', work.work_id, work.canonical_code, work.compact_code, work.title,
       coalesce((SELECT string_agg(performer.display_name, ' ') FROM platform.work_performers link JOIN platform.performers performer ON performer.performer_id = link.performer_id WHERE link.work_id = work.work_id), ''),
       concat_ws(' ',
         coalesce((SELECT string_agg(alias.raw_value, ' ') FROM platform.work_aliases alias WHERE alias.work_id = work.work_id), ''),
         coalesce((SELECT string_agg(alias.raw_value, ' ')
                   FROM platform.work_performers link
                   JOIN platform.performer_aliases alias ON alias.performer_id = link.performer_id
                   WHERE link.work_id = work.work_id), '')
       ),
       coalesce(studio.name, ''), $2::uuid, $3
FROM platform.works work LEFT JOIN platform.studios studio ON studio.studio_id = work.studio_id
WHERE work.work_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET
 canonical_code = EXCLUDED.canonical_code, compact_code = EXCLUDED.compact_code, title = EXCLUDED.title,
 names = EXCLUDED.names, aliases = EXCLUDED.aliases, studio_name = EXCLUDED.studio_name,
 publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	case "performer":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, title, names, aliases, publication_id, updated_at)
SELECT 'performer', performer.performer_id, performer.display_name,
       concat_ws(' ', performer.display_name, performer.name_original, performer.romanized_name),
       coalesce((SELECT string_agg(alias.raw_value, ' ') FROM platform.performer_aliases alias WHERE alias.performer_id = performer.performer_id), ''),
       $2::uuid, $3 FROM platform.performers performer WHERE performer.performer_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET title = EXCLUDED.title, names = EXCLUDED.names,
 aliases = EXCLUDED.aliases, publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	case "studio":
		query = `
INSERT INTO platform.search_documents (entity_type, entity_id, title, studio_name, publication_id, updated_at)
SELECT 'studio', studio_id, name, name, $2::uuid, $3 FROM platform.studios WHERE studio_id = $1::uuid
ON CONFLICT (entity_type, entity_id) DO UPDATE SET title = EXCLUDED.title, studio_name = EXCLUDED.studio_name,
 publication_id = EXCLUDED.publication_id, updated_at = EXCLUDED.updated_at`
	default:
		return operations.ErrInvalidInput
	}
	if _, err := tx.Exec(ctx, query, entityID, publicationID, now); err != nil {
		return fmt.Errorf("refresh search document: %w", err)
	}
	return nil
}

func insertAudit(ctx context.Context, tx pgx.Tx, actorID, action, objectType, objectID string, before, after any, reason, requestID string, now time.Time) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("encode audit before state: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode audit after state: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit.audit_logs (actor_type, actor_id, action, object_type, object_id, before_version, after_version, reason, request_id, occurred_at)
VALUES ('user', $1::uuid, $2, $3, $4::uuid, $5::jsonb, $6::jsonb, nullif($7, ''), $8, $9)`,
		actorID, action, objectType, objectID, beforeJSON, afterJSON, reason, requestID, now)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

func mapConstraintError(err error) error {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		switch databaseError.Code {
		case "23505":
			return operations.ErrConflict
		case "23503", "23514", "22P02", "22007":
			return operations.ErrInvalidInput
		}
	}
	return err
}

func compactIdentifier(value string) string {
	var result strings.Builder
	for _, character := range strings.ToUpper(value) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
		}
	}
	return result.String()
}

func putOptional[T any](target map[string]any, key string, value *T) {
	if value != nil {
		target[key] = *value
	}
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest)
}
