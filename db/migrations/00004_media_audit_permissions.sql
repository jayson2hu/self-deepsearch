-- +goose Up
CREATE TABLE collector.media_staging (
    media_staging_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id uuid REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    source_record_id uuid REFERENCES collector.source_records(source_record_id) ON DELETE RESTRICT,
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer')),
    entity_id uuid,
    candidate_url text,
    source_type text NOT NULL CHECK (length(source_type) BETWEEN 1 AND 100),
    rights_status text NOT NULL DEFAULT 'needs_review' CHECK (rights_status IN ('needs_review', 'allowed', 'restricted', 'takedown')),
    download_status text NOT NULL DEFAULT 'pending' CHECK (download_status IN ('pending', 'downloading', 'downloaded', 'rejected', 'failed', 'cancelled')),
    review_status text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'reviewing', 'approved', 'rejected')),
    content_hash text CHECK (content_hash IS NULL OR content_hash ~ '^sha256:[a-f0-9]{64}$'),
    mime_type text,
    byte_size bigint CHECK (byte_size IS NULL OR byte_size > 0),
    error_code text,
    checked_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (candidate_url IS NULL OR candidate_url ~* '^https?://'),
    CHECK (candidate_url IS NOT NULL OR source_type IN ('manual', 'fixture'))
);

CREATE INDEX media_staging_queue_idx ON collector.media_staging (review_status, created_at);

CREATE TRIGGER media_staging_set_updated_at
BEFORE UPDATE ON collector.media_staging
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.media_assets (
    asset_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_type text NOT NULL CHECK (asset_type IN ('work_image', 'performer_avatar')),
    current_version integer NOT NULL DEFAULT 1 CHECK (current_version > 0),
    source_id uuid REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    source_type text NOT NULL CHECK (length(source_type) BETWEEN 1 AND 100),
    source_url text,
    checked_at timestamptz NOT NULL,
    operator_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    confidence numeric(4,3) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rights_status text NOT NULL CHECK (rights_status IN ('needs_review', 'allowed', 'restricted', 'takedown')),
    asset_status text NOT NULL DEFAULT 'staging' CHECK (asset_status IN ('staging', 'reviewing', 'ready', 'published', 'hidden', 'takedown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (source_url IS NULL OR source_url ~* '^https?://'),
    CHECK (asset_status <> 'published' OR rights_status = 'allowed')
);

CREATE TRIGGER media_assets_set_updated_at
BEFORE UPDATE ON platform.media_assets
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.media_objects (
    media_object_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id uuid NOT NULL REFERENCES platform.media_assets(asset_id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    rendition text NOT NULL CHECK (rendition IN ('quarantine', 'master', 'w320', 'w640', 'w960')),
    storage_provider text NOT NULL CHECK (storage_provider IN ('s3', 'beijing', 'japan_local')),
    storage_key text NOT NULL CHECK (length(storage_key) BETWEEN 1 AND 1024),
    backup_path text,
    public_url text,
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    mime_type text NOT NULL CHECK (mime_type IN ('image/jpeg', 'image/png', 'image/webp')),
    width integer NOT NULL CHECK (width BETWEEN 1 AND 20000),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 20000),
    byte_size bigint NOT NULL CHECK (byte_size BETWEEN 1 AND 10485760),
    object_status text NOT NULL DEFAULT 'pending' CHECK (object_status IN ('pending', 'ready', 'published', 'hidden', 'deleted', 'failed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz,
    UNIQUE (storage_provider, storage_key),
    UNIQUE (asset_id, version, rendition, storage_provider),
    CHECK (public_url IS NULL OR public_url ~* '^https?://'),
    CHECK ((object_status = 'deleted' AND deleted_at IS NOT NULL) OR (object_status <> 'deleted' AND deleted_at IS NULL)),
    CHECK (rendition IN ('w320', 'w640', 'w960') OR public_url IS NULL)
);

CREATE UNIQUE INDEX media_objects_public_url_uq ON platform.media_objects (public_url) WHERE public_url IS NOT NULL;
CREATE INDEX media_objects_hash_idx ON platform.media_objects (sha256, byte_size);

CREATE TABLE platform.media_derivations (
    derivation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_object_id uuid NOT NULL REFERENCES platform.media_objects(media_object_id) ON DELETE RESTRICT,
    child_object_id uuid NOT NULL UNIQUE REFERENCES platform.media_objects(media_object_id) ON DELETE CASCADE,
    format text NOT NULL CHECK (format IN ('jpeg', 'png', 'webp')),
    parameters jsonb NOT NULL CHECK (jsonb_typeof(parameters) = 'object'),
    tool_version text NOT NULL CHECK (length(tool_version) BETWEEN 1 AND 200),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (parent_object_id <> child_object_id)
);

CREATE TABLE platform.media_revisions (
    media_revision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    asset_id uuid NOT NULL REFERENCES platform.media_assets(asset_id) ON DELETE RESTRICT,
    version integer NOT NULL CHECK (version > 0),
    parent_version integer CHECK (parent_version > 0),
    editor_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    tool_version text NOT NULL CHECK (length(tool_version) BETWEEN 1 AND 200),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    manifest jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (asset_id, version),
    CHECK (parent_version IS NULL OR parent_version < version)
);

CREATE TABLE platform.entity_media (
    entity_media_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer')),
    entity_id uuid NOT NULL,
    asset_id uuid NOT NULL REFERENCES platform.media_assets(asset_id) ON DELETE RESTRICT,
    purpose text NOT NULL CHECK (purpose IN ('cover', 'gallery', 'avatar')),
    position smallint NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 100),
    is_primary boolean NOT NULL DEFAULT false,
    publication_status text NOT NULL DEFAULT 'draft' CHECK (publication_status IN ('draft', 'reviewing', 'published', 'hidden', 'takedown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id, asset_id, purpose),
    CHECK ((entity_type = 'performer' AND purpose = 'avatar') OR entity_type = 'work'),
    CHECK ((entity_type = 'work' AND purpose IN ('cover', 'gallery')) OR entity_type = 'performer')
);

CREATE UNIQUE INDEX entity_media_primary_uq
    ON platform.entity_media (entity_type, entity_id)
    WHERE is_primary AND publication_status = 'published';
CREATE INDEX entity_media_public_idx
    ON platform.entity_media (entity_type, entity_id, position)
    WHERE publication_status = 'published';

CREATE TRIGGER entity_media_set_updated_at
BEFORE UPDATE ON platform.entity_media
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE VIEW platform.public_entity_media AS
SELECT
    link.entity_type,
    link.entity_id,
    link.asset_id,
    link.purpose,
    link.position,
    link.is_primary,
    object.rendition,
    object.public_url,
    object.width,
    object.height,
    object.byte_size,
    object.mime_type
FROM platform.entity_media AS link
JOIN platform.media_assets AS asset
  ON asset.asset_id = link.asset_id
 AND asset.asset_status = 'published'
 AND asset.rights_status = 'allowed'
JOIN platform.media_objects AS object
  ON object.asset_id = asset.asset_id
 AND object.version = asset.current_version
 AND object.object_status = 'published'
 AND object.public_url IS NOT NULL
WHERE link.publication_status = 'published';

CREATE TABLE audit.audit_logs (
    audit_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_type text NOT NULL CHECK (actor_type IN ('user', 'worker', 'system')),
    actor_id uuid,
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 150),
    object_type text NOT NULL CHECK (length(object_type) BETWEEN 1 AND 100),
    object_id uuid,
    before_version jsonb,
    after_version jsonb,
    reason text,
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    occurred_at timestamptz NOT NULL DEFAULT now(),
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX audit_logs_object_idx ON audit.audit_logs (object_type, object_id, occurred_at DESC);
CREATE INDEX audit_logs_actor_idx ON audit.audit_logs (actor_type, actor_id, occurred_at DESC);
CREATE INDEX audit_logs_request_idx ON audit.audit_logs (request_id);

CREATE TABLE audit.login_events (
    login_event_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    result text NOT NULL CHECK (result IN ('success', 'invalid_credentials', 'locked', 'suspended', 'closed', 'rate_limited')),
    risk_reasons jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(risk_reasons) = 'array'),
    ip_hash text,
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_events_user_idx ON audit.login_events (user_id, occurred_at DESC);

CREATE TABLE audit.account_closure_events (
    closure_event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    challenge_id uuid NOT NULL REFERENCES platform.email_challenges(challenge_id) ON DELETE RESTRICT,
    notice_version text NOT NULL CHECK (length(notice_version) BETWEEN 1 AND 100),
    actor_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    closed_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id)
);

CREATE TABLE audit.takedown_requests (
    takedown_request_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_reference text NOT NULL CHECK (length(btrim(requester_reference)) BETWEEN 1 AND 500),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio', 'media')),
    entity_id uuid NOT NULL,
    evidence_reference text,
    request_status text NOT NULL DEFAULT 'received' CHECK (request_status IN ('received', 'validating', 'approved', 'rejected', 'completed')),
    assigned_to uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    received_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz,
    completed_at timestamptz,
    result_summary text,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((request_status IN ('approved', 'rejected', 'completed') AND decided_at IS NOT NULL) OR request_status IN ('received', 'validating')),
    CHECK (request_status <> 'completed' OR completed_at IS NOT NULL)
);

CREATE INDEX takedown_requests_queue_idx ON audit.takedown_requests (request_status, received_at);

CREATE TRIGGER takedown_requests_set_updated_at
BEFORE UPDATE ON audit.takedown_requests
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE audit.archived_history_manifests (
    archive_manifest_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_path text NOT NULL UNIQUE CHECK (length(storage_path) BETWEEN 1 AND 1024),
    schema_version integer NOT NULL CHECK (schema_version > 0),
    user_scope jsonb NOT NULL CHECK (jsonb_typeof(user_scope) = 'object'),
    range_start timestamptz NOT NULL,
    range_end timestamptz NOT NULL,
    record_count bigint NOT NULL CHECK (record_count > 0),
    byte_size bigint NOT NULL CHECK (byte_size > 0),
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (range_end >= range_start)
);

CREATE TABLE audit.backup_runs (
    backup_run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    backup_type text NOT NULL CHECK (backup_type IN ('daily', 'weekly', 'manual', 'restore_test')),
    backup_status text NOT NULL DEFAULT 'running' CHECK (backup_status IN ('running', 'completed', 'failed', 'verified')),
    source_region text NOT NULL CHECK (source_region IN ('japan', 'beijing')),
    destination_region text NOT NULL CHECK (destination_region IN ('japan', 'beijing')),
    file_reference text,
    byte_size bigint CHECK (byte_size IS NULL OR byte_size > 0),
    sha256 text CHECK (sha256 IS NULL OR sha256 ~ '^[a-f0-9]{64}$'),
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    copied_at timestamptz,
    restore_verified_at timestamptz,
    error_code text,
    summary jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(summary) = 'object'),
    CHECK (source_region <> destination_region OR backup_type = 'restore_test'),
    CHECK (backup_status = 'running' OR completed_at IS NOT NULL)
);

CREATE INDEX backup_runs_recent_idx ON audit.backup_runs (started_at DESC);

CREATE TABLE audit.notification_deliveries (
    notification_delivery_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    notification_type text NOT NULL CHECK (notification_type IN ('signup_code', 'password_code', 'account_close_code', 'account_closed', 'admin_alert')),
    provider_message_id text,
    delivery_status text NOT NULL DEFAULT 'queued' CHECK (delivery_status IN ('queued', 'sending', 'sent', 'delivered', 'bounced', 'failed', 'suppressed')),
    error_class text,
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 10),
    queued_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    completed_at timestamptz,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX notification_deliveries_queue_idx ON audit.notification_deliveries (delivery_status, queued_at)
    WHERE delivery_status IN ('queued', 'sending');

CREATE OR REPLACE FUNCTION audit.prevent_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit records are append-only';
END
$$;

CREATE TRIGGER audit_logs_append_only
BEFORE UPDATE OR DELETE ON audit.audit_logs
FOR EACH ROW EXECUTE FUNCTION audit.prevent_mutation();

CREATE TRIGGER login_events_append_only
BEFORE UPDATE OR DELETE ON audit.login_events
FOR EACH ROW EXECUTE FUNCTION audit.prevent_mutation();

CREATE TRIGGER account_closure_events_append_only
BEFORE UPDATE OR DELETE ON audit.account_closure_events
FOR EACH ROW EXECUTE FUNCTION audit.prevent_mutation();

CREATE TRIGGER archived_history_manifests_append_only
BEFORE UPDATE OR DELETE ON audit.archived_history_manifests
FOR EACH ROW EXECUTE FUNCTION audit.prevent_mutation();

REVOKE ALL ON ALL TABLES IN SCHEMA collector FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA platform FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA audit FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA collector FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA platform FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA audit FROM PUBLIC;

GRANT USAGE ON SCHEMA platform TO public_reader;
GRANT SELECT ON platform.public_published_works, platform.public_published_performers, platform.public_published_studios TO public_reader;
GRANT SELECT ON platform.public_search_documents, platform.public_entity_media, platform.content_metrics_hourly TO public_reader;

GRANT USAGE ON SCHEMA collector TO collector_ingest;
GRANT SELECT ON collector.sources, collector.source_connectors TO collector_ingest;
GRANT SELECT, INSERT, UPDATE ON collector.publication_batches, collector.source_records, collector.jobs TO collector_ingest;
GRANT INSERT, SELECT ON collector.raw_snapshot_refs, collector.normalized_records, collector.media_staging TO collector_ingest;

GRANT USAGE ON SCHEMA platform TO platform_api;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA platform TO platform_api;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA platform TO platform_api;
GRANT USAGE ON SCHEMA collector TO platform_api;
GRANT SELECT, INSERT, UPDATE ON collector.review_tasks, collector.conflict_reviews TO platform_api;

GRANT USAGE ON SCHEMA platform TO platform_worker;
GRANT SELECT ON platform.public_published_works, platform.public_published_performers, platform.public_published_studios TO platform_worker;
GRANT SELECT, INSERT, UPDATE ON platform.outbox_events, platform.content_metrics_hourly TO platform_worker;
GRANT SELECT, INSERT, UPDATE ON platform.media_assets, platform.media_objects, platform.media_derivations, platform.media_revisions, platform.entity_media TO platform_worker;

GRANT USAGE ON SCHEMA audit TO audit_writer;
GRANT INSERT, SELECT ON audit.audit_logs, audit.login_events, audit.account_closure_events, audit.archived_history_manifests TO audit_writer;
GRANT SELECT, INSERT, UPDATE ON audit.takedown_requests, audit.backup_runs, audit.notification_deliveries TO audit_writer;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA audit TO audit_writer;

GRANT public_reader TO platform_api;
GRANT collector_ingest TO platform_api;
GRANT audit_writer TO platform_api;
GRANT audit_writer TO platform_worker;

ALTER DEFAULT PRIVILEGES IN SCHEMA collector REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA platform REVOKE ALL ON TABLES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA audit REVOKE ALL ON TABLES FROM PUBLIC;

UPDATE platform.system_metadata SET schema_version = 4 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 3 WHERE singleton;

REVOKE audit_writer FROM platform_worker;
REVOKE audit_writer FROM platform_api;
REVOKE collector_ingest FROM platform_api;
REVOKE public_reader FROM platform_api;

REVOKE ALL ON SCHEMA audit FROM audit_writer;
REVOKE ALL ON SCHEMA collector FROM collector_ingest, platform_api;
REVOKE ALL ON SCHEMA platform FROM public_reader, platform_api, platform_worker;

DROP TRIGGER IF EXISTS archived_history_manifests_append_only ON audit.archived_history_manifests;
DROP TRIGGER IF EXISTS account_closure_events_append_only ON audit.account_closure_events;
DROP TRIGGER IF EXISTS login_events_append_only ON audit.login_events;
DROP TRIGGER IF EXISTS audit_logs_append_only ON audit.audit_logs;
DROP FUNCTION IF EXISTS audit.prevent_mutation();
DROP TABLE IF EXISTS audit.notification_deliveries;
DROP TABLE IF EXISTS audit.backup_runs;
DROP TABLE IF EXISTS audit.archived_history_manifests;
DROP TRIGGER IF EXISTS takedown_requests_set_updated_at ON audit.takedown_requests;
DROP TABLE IF EXISTS audit.takedown_requests;
DROP TABLE IF EXISTS audit.account_closure_events;
DROP TABLE IF EXISTS audit.login_events;
DROP TABLE IF EXISTS audit.audit_logs;
DROP VIEW IF EXISTS platform.public_entity_media;
DROP TRIGGER IF EXISTS entity_media_set_updated_at ON platform.entity_media;
DROP TABLE IF EXISTS platform.entity_media;
DROP TABLE IF EXISTS platform.media_revisions;
DROP TABLE IF EXISTS platform.media_derivations;
DROP TABLE IF EXISTS platform.media_objects;
DROP TRIGGER IF EXISTS media_assets_set_updated_at ON platform.media_assets;
DROP TABLE IF EXISTS platform.media_assets;
DROP TRIGGER IF EXISTS media_staging_set_updated_at ON collector.media_staging;
DROP TABLE IF EXISTS collector.media_staging;
