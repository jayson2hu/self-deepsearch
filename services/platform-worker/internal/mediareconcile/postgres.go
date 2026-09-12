package mediareconcile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maximumObjects = 10000

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("media reconciliation database pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (repository *PostgresRepository) Prepare(ctx context.Context, now time.Time, minimumInterval time.Duration) (Run, error) {
	return prepareReconciliation(ctx, repository.pool.BeginTx, now, minimumInterval)
}

func prepareReconciliation(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), now time.Time, minimumInterval time.Duration) (Run, error) {
	// The schedule read must take a fresh snapshot AFTER the advisory lock.
	// REPEATABLE READ can retain a pre-wait snapshot and miss the winning
	// scheduler's newly committed run. Inventory itself is one SELECT snapshot.
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Run{}, fmt.Errorf("begin media reconciliation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-reconciliation'))`); err != nil {
		return Run{}, fmt.Errorf("lock media reconciliation schedule: %w", err)
	}
	var notDue bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM audit.media_reconciliation_runs
  WHERE started_at > $1 - make_interval(secs => $2)
    AND NOT (run_status = 'failed' AND error_code = 'usage_guard_denied')
)`, now, int(minimumInterval.Seconds())).Scan(&notDue); err != nil {
		return Run{}, fmt.Errorf("read media reconciliation schedule: %w", err)
	}
	if notDue {
		return Run{}, ErrNotDue
	}
	var run Run
	if err := tx.QueryRow(ctx, `
INSERT INTO audit.media_reconciliation_runs (run_status, started_at)
VALUES ('running', $1) RETURNING run_id::text`, now).Scan(&run.ID); err != nil {
		return Run{}, fmt.Errorf("create media reconciliation run: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT storage_scope, storage_key, backup_path, sha256, byte_size
FROM platform.media_objects
WHERE object_status <> 'deleted'
ORDER BY storage_scope, storage_key
LIMIT $1`, maximumObjects+1)
	if err != nil {
		return Run{}, fmt.Errorf("read media reconciliation inventory: %w", err)
	}
	defer rows.Close()
	run.Objects = make([]Object, 0)
	for rows.Next() {
		var object Object
		if err := rows.Scan(&object.StorageScope, &object.StorageKey, &object.BackupPath, &object.SHA256, &object.ByteSize); err != nil {
			return Run{}, fmt.Errorf("scan media reconciliation inventory: %w", err)
		}
		run.Objects = append(run.Objects, object)
	}
	if err := rows.Err(); err != nil {
		return Run{}, fmt.Errorf("iterate media reconciliation inventory: %w", err)
	}
	rows.Close()
	if len(run.Objects) > maximumObjects {
		if _, err := tx.Exec(ctx, `
UPDATE audit.media_reconciliation_runs
SET run_status = 'failed', completed_at = $2, error_code = 'inventory_too_large'
WHERE run_id = $1::uuid AND run_status = 'running'`, run.ID, now); err != nil {
			return Run{}, fmt.Errorf("record oversized media inventory: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return Run{}, fmt.Errorf("commit oversized media inventory: %w", err)
		}
		return Run{}, errors.New("media reconciliation inventory exceeds 10000 objects")
	}
	if _, err := tx.Exec(ctx, `UPDATE audit.media_reconciliation_runs SET expected_count = $2 WHERE run_id = $1::uuid`, run.ID, len(run.Objects)); err != nil {
		return Run{}, fmt.Errorf("record media inventory count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("commit media reconciliation inventory: %w", err)
	}
	return run, nil
}

func (repository *PostgresRepository) Complete(ctx context.Context, report Report, now time.Time) error {
	samples, err := json.Marshal(report.IssueSamples)
	if err != nil {
		return fmt.Errorf("encode media reconciliation samples: %w", err)
	}
	command, err := repository.pool.Exec(ctx, `
UPDATE audit.media_reconciliation_runs
SET run_status = 'completed', missing_s3_count = $2, corrupt_s3_count = $3,
    missing_backup_count = $4, corrupt_backup_count = $5, orphan_s3_count = $6,
    orphan_backup_count = $7, issue_samples = $8::jsonb, completed_at = $9
WHERE run_id = $1::uuid AND run_status = 'running' AND expected_count = $10`,
		report.RunID, report.MissingS3Count, report.CorruptS3Count, report.MissingBackupCount,
		report.CorruptBackupCount, report.OrphanS3Count, report.OrphanBackupCount, samples, now, report.ExpectedCount)
	if err != nil {
		return fmt.Errorf("complete media reconciliation: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("media reconciliation run is no longer completable")
	}
	return nil
}

func (repository *PostgresRepository) Fail(ctx context.Context, runID, errorCode string, now time.Time) error {
	command, err := repository.pool.Exec(ctx, `
UPDATE audit.media_reconciliation_runs
SET run_status = 'failed', error_code = $2, completed_at = $3
WHERE run_id = $1::uuid AND run_status = 'running'`, runID, errorCode, now)
	if err != nil {
		return fmt.Errorf("fail media reconciliation: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("media reconciliation run is no longer fail-able")
	}
	return nil
}
