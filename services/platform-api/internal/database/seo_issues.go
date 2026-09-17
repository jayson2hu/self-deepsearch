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

func (store *Store) RunSEOScan(ctx context.Context, actorID, requestID string, now time.Time) (operations.SEOScan, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.SEOScan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var scan operations.SEOScan
	err = tx.QueryRow(ctx, `
INSERT INTO platform.seo_scan_runs (scan_source,status,requested_by,started_at)
VALUES ('manual','running',$1::uuid,$2)
RETURNING scan_id::text,scan_source,status,scanned_count,issue_count,skipped_count,truncated,error_summary,started_at,completed_at`, actorID, now).
		Scan(&scan.ID, &scan.Source, &scan.Status, &scan.ScannedCount, &scan.IssueCount, &scan.SkippedCount,
			&scan.Truncated, &scan.ErrorSummary, &scan.StartedAt, &scan.CompletedAt)
	if err != nil {
		return operations.SEOScan{}, mapConstraintError(err)
	}
	rows, err := tx.Query(ctx, seoInventoryCTE+`
SELECT entity_type,entity_id::text,'/'||entity_type||'s/'||canonical_slug,coalesce(exclusion_reason,'')
FROM classified
WHERE publication_status='published'
ORDER BY entity_type,entity_id
LIMIT 1001`)
	if err != nil {
		return operations.SEOScan{}, fmt.Errorf("read seo scan targets: %w", err)
	}
	type target struct{ entityType, entityID, path, reason string }
	targets := make([]target, 0, 1001)
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.entityType, &item.entityID, &item.path, &item.reason); err != nil {
			rows.Close()
			return operations.SEOScan{}, err
		}
		targets = append(targets, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return operations.SEOScan{}, err
	}
	if len(targets) > 1000 {
		scan.Truncated = true
		targets = targets[:1000]
	}
	for _, item := range targets {
		if item.reason == "" {
			scan.SkippedCount++
			continue
		}
		rule, priority := seoRule(item.reason)
		if err := upsertSEOIssue(ctx, tx, scan.ID, item.entityType, item.entityID, item.path, rule, priority, item.reason, now); err != nil {
			return operations.SEOScan{}, err
		}
		scan.IssueCount++
	}
	scan.ScannedCount = len(targets)
	completed := now
	scan.Status, scan.CompletedAt = "completed", &completed
	if _, err := tx.Exec(ctx, `UPDATE platform.seo_scan_runs SET status='completed',scanned_count=$2,issue_count=$3,
skipped_count=$4,truncated=$5,completed_at=$6 WHERE scan_id=$1::uuid`, scan.ID, scan.ScannedCount, scan.IssueCount, scan.SkippedCount, scan.Truncated, completed); err != nil {
		return operations.SEOScan{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "seo.scan", "seo_scan", scan.ID, nil,
		map[string]any{"scanned": scan.ScannedCount, "issues": scan.IssueCount, "truncated": scan.Truncated}, "manual bounded SEO scan", requestID, now); err != nil {
		return operations.SEOScan{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.SEOScan{}, err
	}
	return scan, nil
}

func seoRule(reason string) (string, string) {
	switch reason {
	case "classification_unknown":
		return "SEO_CLASSIFICATION_UNKNOWN", "P1"
	case "quality_ineligible":
		return "SEO_QUALITY_INELIGIBLE", "P2"
	default:
		return "SEO_QUALITY_UNKNOWN", "P1"
	}
}

func upsertSEOIssue(ctx context.Context, tx pgx.Tx, scanID, entityType, entityID, path, rule, priority, reason string, now time.Time) error {
	var issueID string
	err := tx.QueryRow(ctx, `SELECT issue_id::text FROM platform.seo_issues
WHERE rule_code=$1 AND url_path=$2 AND status<>'closed' FOR UPDATE`, rule, path).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) {
		evidence, _ := json.Marshal(map[string]any{"exclusion_reason": reason})
		if err := tx.QueryRow(ctx, `INSERT INTO platform.seo_issues
(rule_code,entity_type,entity_id,url_path,priority,evidence,first_seen_at,last_seen_at,last_scan_id)
VALUES ($1,$2,$3::uuid,$4,$5,$6::jsonb,$7,$7,$8::uuid) RETURNING issue_id::text`,
			rule, entityType, entityID, path, priority, evidence, now, scanID).Scan(&issueID); err != nil {
			return mapConstraintError(err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO platform.seo_issue_events(issue_id,event_type,scan_id,occurred_at)
VALUES ($1::uuid,'detected',$2::uuid,$3)`, issueID, scanID, now)
		return err
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.seo_issues SET last_seen_at=$2,last_scan_id=$3::uuid,
evidence=jsonb_build_object('exclusion_reason',$4::text) WHERE issue_id=$1::uuid`, issueID, now, scanID, reason); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO platform.seo_issue_events(issue_id,event_type,scan_id,occurred_at)
VALUES ($1::uuid,'rediscovered',$2::uuid,$3)`, issueID, scanID, now)
	return err
}

func (store *Store) ListSEOScans(ctx context.Context, limit int) ([]operations.SEOScan, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	rows, err := store.pool.Query(ctx, `SELECT scan_id::text,scan_source,status,scanned_count,issue_count,skipped_count,
truncated,error_summary,started_at,completed_at FROM platform.seo_scan_runs ORDER BY started_at DESC,scan_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]operations.SEOScan, 0)
	for rows.Next() {
		var item operations.SEOScan
		if err := rows.Scan(&item.ID, &item.Source, &item.Status, &item.ScannedCount, &item.IssueCount,
			&item.SkippedCount, &item.Truncated, &item.ErrorSummary, &item.StartedAt, &item.CompletedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *Store) ListSEOIssues(ctx context.Context, status string, limit int) ([]operations.SEOIssue, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `SELECT issue.issue_id::text,issue.rule_code,issue.entity_type,issue.entity_id::text,
issue.url_path,issue.priority,issue.status,issue.evidence,issue.assignee_id::text,account.normalized_email,
issue.due_at,issue.resolution_note,issue.first_seen_at,issue.last_seen_at,issue.closed_at,issue.updated_at
FROM platform.seo_issues issue LEFT JOIN platform.users account ON account.user_id=issue.assignee_id
WHERE $1='all' OR issue.status=$1
ORDER BY CASE issue.priority WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 ELSE 2 END,issue.last_seen_at DESC,issue.issue_id
LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]operations.SEOIssue, 0)
	for rows.Next() {
		item, err := scanSEOIssue(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type seoIssueScanner interface{ Scan(...any) error }

func scanSEOIssue(row seoIssueScanner) (operations.SEOIssue, error) {
	var item operations.SEOIssue
	var evidence []byte
	err := row.Scan(&item.ID, &item.RuleCode, &item.EntityType, &item.EntityID, &item.URLPath, &item.Priority,
		&item.Status, &evidence, &item.AssigneeID, &item.Assignee, &item.DueAt, &item.ResolutionNote,
		&item.FirstSeenAt, &item.LastSeenAt, &item.ClosedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(evidence, &item.Evidence)
	}
	return item, err
}

func (store *Store) UpdateSEOIssue(ctx context.Context, issueID string, input operations.SEOIssueUpdate, actorID, requestID string, now time.Time) (operations.SEOIssue, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return operations.SEOIssue{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := loadSEOIssue(ctx, tx, issueID)
	if err != nil {
		return operations.SEOIssue{}, err
	}
	if current.Status == "closed" {
		return operations.SEOIssue{}, operations.ErrConflict
	}
	allowed := current.Status == input.Status || (current.Status == "open" && input.Status == "in_progress") ||
		(current.Status == "in_progress" && input.Status == "retest") || (current.Status == "retest" && input.Status == "closed")
	if !allowed {
		return operations.SEOIssue{}, operations.ErrConflict
	}
	if input.AssigneeID != nil {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM platform.users WHERE user_id=$1::uuid AND role IN ('owner','admin') AND account_status='active')`, *input.AssigneeID).Scan(&valid); err != nil || !valid {
			return operations.SEOIssue{}, operations.ErrInvalidInput
		}
	}
	if input.Status == "closed" {
		var resolved bool
		if err := tx.QueryRow(ctx, `SELECT publication_status='published' AND content_classification<>'unknown' AND seo_quality_status='eligible'
FROM platform.publications WHERE site='main' AND entity_type=$1 AND entity_id=$2::uuid`, current.EntityType, current.EntityID).Scan(&resolved); errors.Is(err, pgx.ErrNoRows) {
			resolved = false
		} else if err != nil {
			return operations.SEOIssue{}, err
		}
		if !resolved {
			if _, err := tx.Exec(ctx, `UPDATE platform.seo_issues SET status='in_progress',resolution_note=$2,assignee_id=$3::uuid,due_at=$4 WHERE issue_id=$1::uuid`, issueID, input.Note, input.AssigneeID, input.DueAt); err != nil {
				return operations.SEOIssue{}, mapConstraintError(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO platform.seo_issue_events(issue_id,event_type,from_status,to_status,note,actor_id,occurred_at)
VALUES ($1::uuid,'retest_failed','retest','in_progress',$2,$3::uuid,$4)`, issueID, input.Note, actorID, now); err != nil {
				return operations.SEOIssue{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return operations.SEOIssue{}, err
			}
			return operations.SEOIssue{}, operations.ErrConflict
		}
	}
	closedAt := (*time.Time)(nil)
	if input.Status == "closed" {
		closedAt = &now
	}
	if _, err := tx.Exec(ctx, `UPDATE platform.seo_issues SET status=$2,assignee_id=$3::uuid,due_at=$4,resolution_note=$5,closed_at=$6 WHERE issue_id=$1::uuid`,
		issueID, input.Status, input.AssigneeID, input.DueAt, input.Note, closedAt); err != nil {
		return operations.SEOIssue{}, mapConstraintError(err)
	}
	eventType := "status_changed"
	if input.Status == "closed" {
		eventType = "closed"
	} else if current.Status == input.Status && current.AssigneeID != input.AssigneeID {
		eventType = "assigned"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO platform.seo_issue_events(issue_id,event_type,from_status,to_status,note,actor_id,occurred_at)
VALUES ($1::uuid,$2,$3,$4,$5,$6::uuid,$7)`, issueID, eventType, current.Status, input.Status, input.Note, actorID, now); err != nil {
		return operations.SEOIssue{}, err
	}
	if err := insertAudit(ctx, tx, actorID, "seo.issue_update", "seo_issue", issueID,
		map[string]any{"status": current.Status, "assignee_id": current.AssigneeID},
		map[string]any{"status": input.Status, "assignee_id": input.AssigneeID}, input.Note, requestID, now); err != nil {
		return operations.SEOIssue{}, err
	}
	updated, err := loadSEOIssue(ctx, tx, issueID)
	if err != nil {
		return operations.SEOIssue{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return operations.SEOIssue{}, err
	}
	return updated, nil
}

func loadSEOIssue(ctx context.Context, tx pgx.Tx, issueID string) (operations.SEOIssue, error) {
	item, err := scanSEOIssue(tx.QueryRow(ctx, `SELECT issue.issue_id::text,issue.rule_code,issue.entity_type,issue.entity_id::text,
issue.url_path,issue.priority,issue.status,issue.evidence,issue.assignee_id::text,account.normalized_email,
issue.due_at,issue.resolution_note,issue.first_seen_at,issue.last_seen_at,issue.closed_at,issue.updated_at
FROM platform.seo_issues issue LEFT JOIN platform.users account ON account.user_id=issue.assignee_id
WHERE issue.issue_id=$1::uuid FOR UPDATE OF issue`, issueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return operations.SEOIssue{}, operations.ErrNotFound
	}
	return item, err
}
