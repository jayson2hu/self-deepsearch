-- +goose Up
CREATE TABLE platform.seo_scan_runs (
    scan_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_source text NOT NULL CHECK (scan_source IN ('manual', 'scheduled')),
    status text NOT NULL CHECK (status IN ('running', 'completed', 'failed')),
    scanned_count integer NOT NULL DEFAULT 0 CHECK (scanned_count BETWEEN 0 AND 1000),
    issue_count integer NOT NULL DEFAULT 0 CHECK (issue_count >= 0),
    skipped_count integer NOT NULL DEFAULT 0 CHECK (skipped_count >= 0),
    truncated boolean NOT NULL DEFAULT false,
    error_summary text CHECK (error_summary IS NULL OR length(error_summary) <= 1000),
    requested_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK ((status = 'running' AND completed_at IS NULL) OR (status <> 'running' AND completed_at IS NOT NULL))
);

CREATE UNIQUE INDEX seo_scan_runs_one_active_idx ON platform.seo_scan_runs ((true)) WHERE status = 'running';
CREATE INDEX seo_scan_runs_started_idx ON platform.seo_scan_runs (started_at DESC);

CREATE TABLE platform.seo_issues (
    issue_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_code text NOT NULL CHECK (rule_code ~ '^SEO_[A-Z0-9_]{3,80}$'),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    entity_id uuid NOT NULL,
    url_path text NOT NULL CHECK (url_path ~ '^/(works|performers|studios)/'),
    priority text NOT NULL CHECK (priority IN ('P0', 'P1', 'P2')),
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'retest', 'closed')),
    evidence jsonb NOT NULL CHECK (jsonb_typeof(evidence) = 'object'),
    assignee_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    due_at timestamptz,
    resolution_note text CHECK (resolution_note IS NULL OR length(btrim(resolution_note)) BETWEEN 2 AND 2000),
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    last_scan_id uuid NOT NULL REFERENCES platform.seo_scan_runs(scan_id) ON DELETE RESTRICT,
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((status = 'closed' AND closed_at IS NOT NULL) OR (status <> 'closed' AND closed_at IS NULL))
);

CREATE UNIQUE INDEX seo_issues_active_rule_url_idx ON platform.seo_issues (rule_code, url_path) WHERE status <> 'closed';
CREATE INDEX seo_issues_queue_idx ON platform.seo_issues (status, priority, last_seen_at DESC);
CREATE TRIGGER seo_issues_set_updated_at BEFORE UPDATE ON platform.seo_issues
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.seo_issue_events (
    event_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issue_id uuid NOT NULL REFERENCES platform.seo_issues(issue_id) ON DELETE RESTRICT,
    event_type text NOT NULL CHECK (event_type IN ('detected', 'rediscovered', 'assigned', 'status_changed', 'retest_failed', 'closed')),
    from_status text,
    to_status text,
    note text CHECK (note IS NULL OR length(note) <= 2000),
    actor_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    scan_id uuid REFERENCES platform.seo_scan_runs(scan_id) ON DELETE RESTRICT,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX seo_issue_events_issue_idx ON platform.seo_issue_events (issue_id, occurred_at DESC, event_id DESC);

GRANT SELECT, INSERT, UPDATE ON platform.seo_scan_runs, platform.seo_issues TO platform_api;
GRANT SELECT, INSERT ON platform.seo_issue_events TO platform_api;
GRANT USAGE, SELECT ON SEQUENCE platform.seo_issue_events_event_id_seq TO platform_api;

UPDATE platform.system_metadata SET schema_version = 25 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 24 WHERE singleton;
DROP TABLE platform.seo_issue_events;
DROP TRIGGER seo_issues_set_updated_at ON platform.seo_issues;
DROP TABLE platform.seo_issues;
DROP TABLE platform.seo_scan_runs;
