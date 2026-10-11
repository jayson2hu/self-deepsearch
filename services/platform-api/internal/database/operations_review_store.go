package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

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
