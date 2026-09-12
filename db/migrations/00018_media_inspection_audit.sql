-- +goose Up
CREATE TABLE audit.media_inspection_runs (
    run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_status text NOT NULL CHECK (run_status IN ('running', 'completed', 'failed')),
    display_origin text NOT NULL CHECK (length(display_origin) BETWEEN 1 AND 2048),
    object_count integer NOT NULL DEFAULT 0 CHECK (object_count BETWEEN 0 AND 10000),
    link_count integer NOT NULL DEFAULT 0 CHECK (link_count BETWEEN 0 AND 10000),
    publication_issues integer NOT NULL DEFAULT 0 CHECK (publication_issues BETWEEN 0 AND object_count + link_count),
    default_failures integer NOT NULL DEFAULT 0 CHECK (default_failures BETWEEN 0 AND 2),
    issue_samples jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(issue_samples) = 'object' AND octet_length(issue_samples::text) <= 16384),
    default_results jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(default_results) = 'array' AND octet_length(default_results::text) <= 4096),
    error_code text,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK (completed_at IS NULL OR completed_at >= started_at),
    CHECK (
        (run_status = 'running' AND completed_at IS NULL AND error_code IS NULL) OR
        (run_status = 'completed' AND completed_at IS NOT NULL AND error_code IS NULL AND jsonb_array_length(default_results) = 2) OR
        (run_status = 'failed' AND completed_at IS NOT NULL AND error_code IS NOT NULL AND error_code IN ('interrupted', 'snapshot_unavailable', 'inventory_too_large', 'invalid_default_report', 'completion_unavailable'))
    )
);

CREATE INDEX media_inspection_runs_started_idx ON audit.media_inspection_runs (started_at DESC);
CREATE INDEX media_inspection_runs_completed_idx ON audit.media_inspection_runs (completed_at DESC) WHERE run_status = 'completed';
GRANT SELECT, INSERT, UPDATE ON audit.media_inspection_runs TO audit_writer;

UPDATE platform.system_metadata SET schema_version = 18 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 17 WHERE singleton;
REVOKE ALL ON audit.media_inspection_runs FROM audit_writer;
DROP TABLE audit.media_inspection_runs;
