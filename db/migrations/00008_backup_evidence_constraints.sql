-- +goose Up
ALTER TABLE audit.backup_runs
    ADD CONSTRAINT backup_runs_completed_evidence_check CHECK (
        backup_status IN ('running', 'failed') OR (
            file_reference IS NOT NULL AND byte_size IS NOT NULL AND sha256 IS NOT NULL
        )
    ),
    ADD CONSTRAINT backup_runs_verified_evidence_check CHECK (
        backup_status <> 'verified' OR (
            copied_at IS NOT NULL AND restore_verified_at IS NOT NULL
        )
    ),
    ADD CONSTRAINT backup_runs_restore_after_copy_check CHECK (
        restore_verified_at IS NULL OR copied_at IS NOT NULL
    );

UPDATE platform.system_metadata SET schema_version = 8 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 7 WHERE singleton;

ALTER TABLE audit.backup_runs
    DROP CONSTRAINT IF EXISTS backup_runs_restore_after_copy_check,
    DROP CONSTRAINT IF EXISTS backup_runs_verified_evidence_check,
    DROP CONSTRAINT IF EXISTS backup_runs_completed_evidence_check;
