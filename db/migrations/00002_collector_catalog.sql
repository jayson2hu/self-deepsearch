-- +goose Up
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'platform_api') THEN
        CREATE ROLE platform_api NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'platform_worker') THEN
        CREATE ROLE platform_worker NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'collector_ingest') THEN
        CREATE ROLE collector_ingest NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'public_reader') THEN
        CREATE ROLE public_reader NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'audit_writer') THEN
        CREATE ROLE audit_writer NOLOGIN;
    END IF;
END
$$;

CREATE TABLE collector.sources (
    source_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    source_type text NOT NULL CHECK (source_type IN ('manual', 'public_web', 'official_api', 'rss', 'sitemap', 'fixture')),
    base_url text,
    priority smallint NOT NULL DEFAULT 100 CHECK (priority BETWEEN 1 AND 1000),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused', 'disabled', 'blocked')),
    rights_status text NOT NULL DEFAULT 'needs_review' CHECK (rights_status IN ('needs_review', 'allowed', 'restricted', 'takedown')),
    checked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT sources_base_url_http_check CHECK (base_url IS NULL OR base_url ~* '^https?://')
);

CREATE UNIQUE INDEX sources_name_normalized_uq ON collector.sources (lower(btrim(name)));

CREATE TABLE collector.source_connectors (
    connector_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id uuid NOT NULL REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    region text NOT NULL CHECK (region IN ('beijing', 'japan')),
    connector_type text NOT NULL CHECK (connector_type IN ('manual', 'fixture', 'http_api', 'html', 'rss', 'sitemap')),
    connector_version text NOT NULL CHECK (length(btrim(connector_version)) BETWEEN 1 AND 200),
    cursor jsonb NOT NULL DEFAULT '{}'::jsonb,
    schedule text,
    enabled boolean NOT NULL DEFAULT false,
    last_probe_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, region, connector_version)
);

CREATE TABLE collector.publication_batches (
    batch_id uuid PRIMARY KEY,
    source_id uuid NOT NULL REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    region text NOT NULL CHECK (region IN ('beijing', 'japan')),
    idempotency_key text NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 512),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[a-f0-9]{64}$'),
    input_count integer NOT NULL DEFAULT 0 CHECK (input_count >= 0),
    accepted_count integer NOT NULL DEFAULT 0 CHECK (accepted_count >= 0),
    rejected_count integer NOT NULL DEFAULT 0 CHECK (rejected_count >= 0),
    status text NOT NULL DEFAULT 'receiving' CHECK (status IN ('receiving', 'accepted', 'partial', 'rejected', 'failed')),
    summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    received_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK (accepted_count + rejected_count <= input_count)
);

CREATE TABLE collector.source_records (
    source_record_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id uuid REFERENCES collector.publication_batches(batch_id) ON DELETE RESTRICT,
    source_id uuid NOT NULL REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    external_id text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 255),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    connector_version text NOT NULL CHECK (length(connector_version) BETWEEN 1 AND 200),
    idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 512),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[a-f0-9]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    source_updated_at timestamptz,
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, entity_type, external_id, content_hash),
    UNIQUE (source_id, idempotency_key)
);

CREATE INDEX source_records_batch_idx ON collector.source_records (batch_id);
CREATE INDEX source_records_received_idx ON collector.source_records (received_at DESC);

CREATE TABLE collector.raw_snapshot_refs (
    snapshot_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_record_id uuid NOT NULL REFERENCES collector.source_records(source_record_id) ON DELETE CASCADE,
    storage_path text NOT NULL CHECK (length(storage_path) BETWEEN 1 AND 1024),
    sha256 text NOT NULL CHECK (sha256 ~ '^[a-f0-9]{64}$'),
    byte_size bigint NOT NULL CHECK (byte_size > 0),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (storage_path),
    UNIQUE (sha256, byte_size)
);

CREATE INDEX raw_snapshot_refs_expiry_idx ON collector.raw_snapshot_refs (expires_at);

CREATE TABLE collector.normalized_records (
    normalized_record_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_record_id uuid NOT NULL UNIQUE REFERENCES collector.source_records(source_record_id) ON DELETE CASCADE,
    parser_version text NOT NULL CHECK (length(parser_version) BETWEEN 1 AND 200),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    normalized_payload jsonb NOT NULL CHECK (jsonb_typeof(normalized_payload) = 'object'),
    validation_errors jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(validation_errors) = 'array'),
    candidate_status text NOT NULL DEFAULT 'pending' CHECK (candidate_status IN ('pending', 'valid', 'invalid', 'duplicate', 'conflict', 'reviewing', 'accepted', 'rejected')),
    normalized_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX normalized_records_status_idx ON collector.normalized_records (candidate_status, normalized_at);

CREATE TABLE collector.jobs (
    job_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    job_type text NOT NULL CHECK (job_type IN ('source_probe', 'work_incremental', 'performer_incremental', 'work_detail', 'image_candidates', 'reconcile_source', 'retry_dead_job')),
    region text NOT NULL CHECK (region IN ('beijing', 'japan')),
    source_id uuid REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    idempotency_key text NOT NULL UNIQUE CHECK (length(idempotency_key) BETWEEN 1 AND 512),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'retry', 'completed', 'dead', 'cancelled')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts integer NOT NULL DEFAULT 5 CHECK (max_attempts BETWEEN 1 AND 20),
    next_run_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_expires_at timestamptz,
    last_error_code text,
    last_error_summary text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CHECK ((status = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL) OR status <> 'running'),
    CHECK (attempt_count <= max_attempts)
);

CREATE INDEX jobs_lease_idx ON collector.jobs (region, status, next_run_at, created_at)
    WHERE status IN ('pending', 'retry');
CREATE INDEX jobs_expired_lease_idx ON collector.jobs (lease_expires_at)
    WHERE status = 'running';

CREATE TABLE platform.studios (
    studio_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    normalized_name text NOT NULL CHECK (length(normalized_name) BETWEEN 1 AND 200),
    canonical_slug text NOT NULL CHECK (canonical_slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    publication_status text NOT NULL DEFAULT 'draft' CHECK (publication_status IN ('draft', 'reviewing', 'approved', 'published', 'hidden', 'takedown', 'merged')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (normalized_name),
    UNIQUE (canonical_slug)
);

CREATE TABLE platform.tags (
    tag_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL CHECK (slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 100),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'hidden', 'retired')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (slug)
);

CREATE TABLE platform.performers (
    performer_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
    name_original text,
    romanized_name text,
    canonical_slug text NOT NULL CHECK (canonical_slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    adult_status text NOT NULL DEFAULT 'unverified' CHECK (adult_status IN ('unverified', 'verified', 'restricted')),
    activity_status text NOT NULL DEFAULT 'unknown' CHECK (activity_status IN ('active', 'retired', 'unknown')),
    publication_status text NOT NULL DEFAULT 'draft' CHECK (publication_status IN ('draft', 'reviewing', 'approved', 'published', 'hidden', 'takedown', 'merged')),
    agency text,
    birth_year smallint CHECK (birth_year BETWEEN 1900 AND 2200),
    height_cm smallint CHECK (height_cm BETWEEN 100 AND 250),
    measurements text,
    debut_year smallint CHECK (debut_year BETWEEN 1900 AND 2200),
    official_links jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(official_links) = 'array'),
    merged_into uuid REFERENCES platform.performers(performer_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (canonical_slug),
    CHECK (merged_into IS NULL OR merged_into <> performer_id),
    CHECK (publication_status <> 'published' OR adult_status = 'verified')
);

CREATE TABLE platform.works (
    work_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_code text NOT NULL CHECK (length(btrim(canonical_code)) BETWEEN 1 AND 100),
    compact_code text NOT NULL CHECK (compact_code ~ '^[A-Z0-9]+$'),
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 500),
    title_original text,
    release_date date,
    studio_id uuid REFERENCES platform.studios(studio_id) ON DELETE RESTRICT,
    edition_of uuid REFERENCES platform.works(work_id) ON DELETE RESTRICT,
    summary text CHECK (summary IS NULL OR length(summary) <= 5000),
    publication_status text NOT NULL DEFAULT 'draft' CHECK (publication_status IN ('draft', 'reviewing', 'approved', 'published', 'hidden', 'takedown', 'merged')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (edition_of IS NULL OR edition_of <> work_id)
);

CREATE INDEX works_canonical_code_idx ON platform.works (canonical_code);
CREATE INDEX works_compact_code_idx ON platform.works (compact_code);
CREATE INDEX works_release_date_idx ON platform.works (release_date DESC NULLS LAST, work_id);
CREATE INDEX works_studio_idx ON platform.works (studio_id, release_date DESC NULLS LAST);

CREATE TABLE platform.work_aliases (
    work_alias_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE CASCADE,
    alias_type text NOT NULL CHECK (alias_type IN ('code', 'title', 'translation', 'legacy_slug')),
    raw_value text NOT NULL CHECK (length(btrim(raw_value)) BETWEEN 1 AND 500),
    normalized_value text NOT NULL CHECK (length(normalized_value) BETWEEN 1 AND 500),
    language_tag text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (work_id, alias_type, normalized_value)
);

CREATE INDEX work_aliases_normalized_idx ON platform.work_aliases (normalized_value);

CREATE TABLE platform.performer_aliases (
    performer_alias_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    performer_id uuid NOT NULL REFERENCES platform.performers(performer_id) ON DELETE CASCADE,
    alias_type text NOT NULL CHECK (alias_type IN ('stage_name', 'reading', 'romanized', 'translation', 'legacy_slug')),
    raw_value text NOT NULL CHECK (length(btrim(raw_value)) BETWEEN 1 AND 300),
    normalized_value text NOT NULL CHECK (length(normalized_value) BETWEEN 1 AND 300),
    language_tag text,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (performer_id, alias_type, normalized_value)
);

CREATE INDEX performer_aliases_normalized_idx ON platform.performer_aliases (normalized_value);

CREATE TABLE platform.performer_source_mappings (
    mapping_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    performer_id uuid NOT NULL REFERENCES platform.performers(performer_id) ON DELETE CASCADE,
    source_id uuid NOT NULL REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    external_id text NOT NULL CHECK (length(external_id) BETWEEN 1 AND 255),
    checked_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, external_id),
    UNIQUE (performer_id, source_id)
);

CREATE TABLE platform.work_performers (
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE CASCADE,
    performer_id uuid NOT NULL REFERENCES platform.performers(performer_id) ON DELETE RESTRICT,
    role text NOT NULL DEFAULT 'performer' CHECK (role IN ('performer', 'featured', 'cameo')),
    position smallint NOT NULL DEFAULT 0 CHECK (position >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (work_id, performer_id, role)
);

CREATE INDEX work_performers_performer_idx ON platform.work_performers (performer_id, work_id);

CREATE TABLE platform.work_tags (
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE CASCADE,
    tag_id uuid NOT NULL REFERENCES platform.tags(tag_id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (work_id, tag_id)
);

CREATE INDEX work_tags_tag_idx ON platform.work_tags (tag_id, work_id);

UPDATE platform.system_metadata SET schema_version = 2 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 1 WHERE singleton;

DROP TABLE IF EXISTS platform.work_tags;
DROP TABLE IF EXISTS platform.work_performers;
DROP TABLE IF EXISTS platform.performer_source_mappings;
DROP TABLE IF EXISTS platform.performer_aliases;
DROP TABLE IF EXISTS platform.work_aliases;
DROP TABLE IF EXISTS platform.works;
DROP TABLE IF EXISTS platform.performers;
DROP TABLE IF EXISTS platform.tags;
DROP TABLE IF EXISTS platform.studios;
DROP TABLE IF EXISTS collector.jobs;
DROP TABLE IF EXISTS collector.normalized_records;
DROP TABLE IF EXISTS collector.raw_snapshot_refs;
DROP TABLE IF EXISTS collector.source_records;
DROP TABLE IF EXISTS collector.publication_batches;
DROP TABLE IF EXISTS collector.source_connectors;
DROP TABLE IF EXISTS collector.sources;

REVOKE ALL ON SCHEMA audit FROM audit_writer;
REVOKE ALL ON SCHEMA platform FROM public_reader, platform_api, platform_worker;
REVOKE ALL ON SCHEMA collector FROM collector_ingest, platform_api;

REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA audit FROM audit_writer, platform_api, platform_worker;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA platform FROM public_reader, platform_api, platform_worker;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA collector FROM collector_ingest, platform_api, platform_worker;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA audit FROM audit_writer, platform_api, platform_worker;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA platform FROM public_reader, platform_api, platform_worker;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA collector FROM collector_ingest, platform_api, platform_worker;

DROP ROLE IF EXISTS audit_writer;
DROP ROLE IF EXISTS public_reader;
DROP ROLE IF EXISTS collector_ingest;
DROP ROLE IF EXISTS platform_worker;
DROP ROLE IF EXISTS platform_api;
