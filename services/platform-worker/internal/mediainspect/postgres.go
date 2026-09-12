package mediainspect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) (*PostgresRepository, error) {
	if pool == nil {
		return nil, errors.New("media inspection database pool is required")
	}
	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) Prepare(ctx context.Context, now time.Time, origin string) (string, error) {
	return prepareInspection(ctx, r.pool.BeginTx, now, origin)
}

func prepareInspection(ctx context.Context, begin func(context.Context, pgx.TxOptions) (pgx.Tx, error), now time.Time, origin string) (string, error) {
	tx, err := begin(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return "", fmt.Errorf("begin inspection schedule: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('self-deepsearch-media-inspection'))`); err != nil {
		return "", err
	}
	// A crashed run is evidence of failure, never a completed zero-issue run.
	if _, err := tx.Exec(ctx, `UPDATE audit.media_inspection_runs SET run_status = 'failed',
 error_code = 'interrupted', completed_at = $1
 WHERE run_status = 'running' AND started_at < $1 - interval '5 minutes'`, now); err != nil {
		return "", err
	}
	var notDue bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM audit.media_inspection_runs
 WHERE started_at > $1 - interval '24 hours')`, now).Scan(&notDue); err != nil {
		return "", err
	}
	if notDue {
		if err := tx.Commit(ctx); err != nil {
			return "", err
		}
		return "", ErrNotDue
	}
	var id string
	if err := tx.QueryRow(ctx, `INSERT INTO audit.media_inspection_runs (run_status, started_at, display_origin)
 VALUES ('running', $1, $2) RETURNING run_id::text`, now, origin).Scan(&id); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (r *PostgresRepository) Snapshot(ctx context.Context) (Snapshot, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	snapshot, err := readSnapshot(ctx, tx)
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

// Both inventories use the same read-only snapshot. No catalog locks and no
// database connection is held while requesting display default images.
func readSnapshot(ctx context.Context, tx pgx.Tx) (Snapshot, error) {
	var snapshot Snapshot
	rows, err := tx.Query(ctx, `
SELECT object.media_object_id::text, object.asset_id::text, asset.asset_type,
 asset.asset_status, asset.rights_status, object.version, asset.current_version,
 object.storage_scope, object.storage_key, object.rendition, object.object_status, object.public_url,
 CASE WHEN object.storage_scope = 'public' AND object.object_status = 'hidden' THEN EXISTS (SELECT 1 FROM platform.outbox_events event
   WHERE event.event_type = 'media_delete' AND event.aggregate_type = 'media'
     AND event.aggregate_id = object.asset_id AND object.storage_provider = 's3'
     AND event.status IN ('pending', 'retry', 'running')
     AND event.payload->>'asset_id' = object.asset_id::text
     AND event.payload->>'storage_scope' = object.storage_scope
     AND event.payload->>'storage_key' = object.storage_key
     AND event.payload->>'backup_path' = object.backup_path
     AND (NOT (event.payload ? 'media_object_id') OR event.payload->>'media_object_id' = object.media_object_id::text)) ELSE false END
FROM platform.media_objects object JOIN platform.media_assets asset USING (asset_id)
ORDER BY object.media_object_id LIMIT $1`, maximumInventory+1)
	if err != nil {
		return Snapshot{}, err
	}
	for rows.Next() {
		var o Object
		if err := rows.Scan(&o.ID, &o.AssetID, &o.AssetType, &o.AssetStatus, &o.RightsStatus, &o.Version, &o.CurrentVersion,
			&o.Scope, &o.Key, &o.Rendition, &o.Status, &o.PublicURL, &o.DeletionQueued); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		snapshot.Objects = append(snapshot.Objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Snapshot{}, err
	}
	if len(snapshot.Objects) > maximumInventory {
		return Snapshot{}, ErrInventoryTooLarge
	}
	rows, err = tx.Query(ctx, `SELECT entity_media_id::text, asset_id::text, entity_type
 FROM platform.entity_media WHERE publication_status = 'published'
 ORDER BY entity_media_id LIMIT $1`, maximumInventory+1)
	if err != nil {
		return Snapshot{}, err
	}
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.ID, &link.AssetID, &link.EntityType); err != nil {
			rows.Close()
			return Snapshot{}, err
		}
		snapshot.Links = append(snapshot.Links, link)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Snapshot{}, err
	}
	if len(snapshot.Links) > maximumInventory {
		return Snapshot{}, ErrInventoryTooLarge
	}
	return snapshot, nil
}

func (r *PostgresRepository) Complete(ctx context.Context, report Report, now time.Time) error {
	if !validDefaultResults(report.Defaults) || report.ObjectCount < 0 || report.ObjectCount > maximumInventory ||
		report.LinkCount < 0 || report.LinkCount > maximumInventory || report.PublicationIssues < 0 ||
		report.PublicationIssues > report.ObjectCount+report.LinkCount {
		return errors.New("invalid inspection report")
	}
	failures := 0
	for _, result := range report.Defaults {
		if result.ErrorCode != "" {
			failures++
		}
	}
	if failures != report.DefaultFailures {
		return errors.New("invalid default failure count")
	}
	samples, err := json.Marshal(report.IssueSamples)
	if err != nil {
		return err
	}
	defaults, err := json.Marshal(report.Defaults)
	if err != nil {
		return err
	}
	command, err := r.pool.Exec(ctx, `UPDATE audit.media_inspection_runs SET run_status = 'completed',
 object_count = $2, link_count = $3, publication_issues = $4, default_failures = $5,
 issue_samples = $6::jsonb, default_results = $7::jsonb, completed_at = $8
 WHERE run_id = $1::uuid AND run_status = 'running'`, report.RunID, report.ObjectCount, report.LinkCount,
		report.PublicationIssues, report.DefaultFailures, samples, defaults, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("inspection is no longer completable")
	}
	return nil
}

func (r *PostgresRepository) Fail(ctx context.Context, id, code string, now time.Time) error {
	command, err := r.pool.Exec(ctx, `UPDATE audit.media_inspection_runs
 SET run_status = 'failed', error_code = $2, completed_at = $3
 WHERE run_id = $1::uuid AND run_status = 'running'`, id, code, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("inspection is no longer fail-able")
	}
	return nil
}
