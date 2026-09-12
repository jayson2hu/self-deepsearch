-- +goose Up
CREATE TABLE audit.media_reconciliation_runs (
    run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_status text NOT NULL CHECK (run_status IN ('running', 'completed', 'failed')),
    expected_count integer NOT NULL DEFAULT 0 CHECK (expected_count BETWEEN 0 AND 10000),
    missing_s3_count integer NOT NULL DEFAULT 0 CHECK (missing_s3_count >= 0),
    corrupt_s3_count integer NOT NULL DEFAULT 0 CHECK (corrupt_s3_count >= 0),
    missing_backup_count integer NOT NULL DEFAULT 0 CHECK (missing_backup_count >= 0),
    corrupt_backup_count integer NOT NULL DEFAULT 0 CHECK (corrupt_backup_count >= 0),
    orphan_s3_count integer NOT NULL DEFAULT 0 CHECK (orphan_s3_count >= 0),
    orphan_backup_count integer NOT NULL DEFAULT 0 CHECK (orphan_backup_count >= 0),
    issue_samples jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(issue_samples) = 'object'),
    error_code text,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (run_status = 'running' AND completed_at IS NULL AND error_code IS NULL) OR
        (run_status = 'completed' AND completed_at IS NOT NULL AND error_code IS NULL) OR
        (run_status = 'failed' AND completed_at IS NOT NULL AND length(error_code) BETWEEN 1 AND 100)
    )
);

CREATE INDEX media_reconciliation_runs_status_idx
    ON audit.media_reconciliation_runs (run_status, started_at DESC);

GRANT SELECT, INSERT, UPDATE ON audit.media_reconciliation_runs TO audit_writer;

UPDATE platform.system_metadata SET schema_version = 10 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 9 WHERE singleton;

REVOKE ALL ON audit.media_reconciliation_runs FROM audit_writer;
DROP TABLE IF EXISTS audit.media_reconciliation_runs;
