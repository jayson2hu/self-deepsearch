package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"self-deepsearch/services/platform-api/internal/operations"
)

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
       (SELECT extract(epoch FROM ($1::timestamptz - max(coalesce(restore_verified_at, copied_at, completed_at))))
        FROM audit.backup_runs WHERE backup_status = 'verified'),
       (SELECT extract(epoch FROM ($1::timestamptz - max(completed_at)))
        FROM audit.media_reconciliation_runs WHERE run_status = 'completed'),
       coalesce((SELECT missing_s3_count::bigint + corrupt_s3_count + missing_backup_count +
                        corrupt_backup_count + orphan_s3_count + orphan_backup_count
                 FROM audit.media_reconciliation_runs
                 WHERE run_status = 'completed' ORDER BY completed_at DESC LIMIT 1), 0),
       (SELECT count(*) FROM audit.media_reconciliation_runs
        WHERE run_status = 'failed' AND started_at >= $1::timestamptz - interval '24 hours'),
       (SELECT greatest(0, extract(epoch FROM ($1::timestamptz - completed_at)))
        FROM audit.media_inspection_runs WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1),
       coalesce((SELECT publication_issues FROM audit.media_inspection_runs
        WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1), 0),
       coalesce((SELECT default_failures FROM audit.media_inspection_runs
        WHERE run_status = 'completed' ORDER BY completed_at DESC, run_id DESC LIMIT 1), 0),
       (SELECT count(*) FROM audit.media_inspection_runs
        WHERE run_status = 'failed' AND completed_at >= $1::timestamptz - interval '24 hours'),
       coalesce((SELECT sum(search_requests) FROM platform.search_metrics_hourly
        WHERE hour_bucket >= date_trunc('hour', $1::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '24 hours'), 0),
       coalesce((SELECT sum(zero_result_requests) FROM platform.search_metrics_hourly
        WHERE hour_bucket >= date_trunc('hour', $1::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '24 hours'), 0)
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
