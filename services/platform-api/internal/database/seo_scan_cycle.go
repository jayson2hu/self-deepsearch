package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"self-deepsearch/services/platform-api/internal/operations"
)

// Each persisted scan is still <=1000 rows (schema 27). A cycle consumes a
// single cursor snapshot across as many batches as needed, atomically. Failure
// rolls back all batches; retry restarts the cycle, never skips its tail.
func (store *Store) runSEOScanCycle(ctx context.Context, actorID, requestID string, now time.Time, source string) (operations.SEOScan, error) {
	var result operations.SEOScan
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return result, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(20260919,607)`).Scan(&locked); err != nil {
		return result, err
	}
	if !locked {
		return result, operations.ErrConflict
	}
	var allowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM platform.users WHERE user_id=$1::uuid AND role IN ('owner','admin') AND account_status='active')`, actorID).Scan(&allowed); err != nil {
		return result, err
	}
	if !allowed {
		return result, operations.ErrForbidden
	}
	if source == "scheduled" {
		zone := time.FixedZone("Asia/Shanghai", 8*60*60)
		local := now.In(zone)
		day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, zone)
		var done bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM platform.seo_scan_runs WHERE scan_source='scheduled' AND status='completed' AND started_at >= $1 AND started_at < $2)`, day, day.AddDate(0, 0, 1)).Scan(&done); err != nil {
			return result, err
		}
		if done {
			return result, nil
		}
	}
	if _, err = tx.Exec(ctx, `DECLARE seo_cycle_targets NO SCROLL CURSOR FOR `+seoInventoryCTE+`
 SELECT entity_type,entity_id::text,'/'||entity_type||'s/'||canonical_slug,coalesce(exclusion_reason,'')
 FROM classified WHERE publication_status='published' ORDER BY entity_type,entity_id`); err != nil {
		return result, fmt.Errorf("declare seo cycle cursor: %w", err)
	}
	type target struct{ kind, id, path, reason string }
	cycleID := ""
	batches, total, issues := 0, 0, 0
	for {
		rows, err := tx.Query(ctx, `FETCH FORWARD 1000 FROM seo_cycle_targets`)
		if err != nil {
			return result, err
		}
		targets := make([]target, 0, 1000)
		for rows.Next() {
			var item target
			if err = rows.Scan(&item.kind, &item.id, &item.path, &item.reason); err != nil {
				rows.Close()
				return result, err
			}
			targets = append(targets, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, err
		}
		if len(targets) == 0 && batches > 0 {
			break
		}
		var scan operations.SEOScan
		err = tx.QueryRow(ctx, `INSERT INTO platform.seo_scan_runs(scan_source,status,requested_by,started_at)
   VALUES($1,'running',$2::uuid,$3) RETURNING scan_id::text`, source, actorID, now).Scan(&scan.ID)
		if err != nil {
			return result, mapConstraintError(err)
		}
		if cycleID == "" {
			cycleID = scan.ID
		}
		batches++
		scan.Source, scan.Status, scan.StartedAt = source, "completed", now
		scan.CompletedAt = &now
		for _, item := range targets {
			if item.reason == "" {
				scan.SkippedCount++
				continue
			}
			rule, priority := seoRule(item.reason)
			if err = upsertSEOIssue(ctx, tx, scan.ID, item.kind, item.id, item.path, rule, priority, item.reason, now); err != nil {
				return result, err
			}
			scan.IssueCount++
		}
		scan.ScannedCount = len(targets)
		total += scan.ScannedCount
		issues += scan.IssueCount
		if _, err = tx.Exec(ctx, `UPDATE platform.seo_scan_runs SET status='completed',scanned_count=$2,issue_count=$3,skipped_count=$4,truncated=false,completed_at=$5 WHERE scan_id=$1::uuid`, scan.ID, scan.ScannedCount, scan.IssueCount, scan.SkippedCount, now); err != nil {
			return result, err
		}
		metadata := map[string]any{"cycle_id": cycleID, "batch_index": batches, "source": source, "requested_by": actorID, "scanned": scan.ScannedCount, "issues": scan.IssueCount, "truncated": false}
		if source == "scheduled" {
			encoded, _ := json.Marshal(metadata)
			_, err = tx.Exec(ctx, `INSERT INTO audit.audit_logs(actor_type,action,object_type,object_id,after_version,reason,request_id,occurred_at)
    VALUES('system','seo.scan.scheduled','seo_scan',$1::uuid,$2::jsonb,'configured daily internal SEO scan',$3,$4)`, scan.ID, encoded, requestID, now)
		} else {
			err = insertAudit(ctx, tx, actorID, "seo.scan", "seo_scan", scan.ID, nil, metadata, "manual cursor-batched SEO scan", requestID, now)
		}
		if err != nil {
			return result, err
		}
		result = scan
		if len(targets) < 1000 {
			break
		}
	}
	result.CycleID, result.BatchCount, result.TotalScannedCount, result.TotalIssueCount = cycleID, batches, total, issues
	if err = tx.Commit(ctx); err != nil {
		return operations.SEOScan{}, err
	}
	return result, nil
}

func (store *Store) RunScheduledSEOScan(ctx context.Context, actorID string, now time.Time) (operations.SEOScan, error) {
	return store.runSEOScanCycle(ctx, actorID, "seo-daily-"+now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.DateOnly), now, "scheduled")
}
