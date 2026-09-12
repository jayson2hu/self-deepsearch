-- +goose Up
CREATE OR REPLACE FUNCTION platform.set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END
$$;

CREATE TRIGGER sources_set_updated_at
BEFORE UPDATE ON collector.sources
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER source_connectors_set_updated_at
BEFORE UPDATE ON collector.source_connectors
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER jobs_set_updated_at
BEFORE UPDATE ON collector.jobs
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER studios_set_updated_at
BEFORE UPDATE ON platform.studios
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER tags_set_updated_at
BEFORE UPDATE ON platform.tags
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER performers_set_updated_at
BEFORE UPDATE ON platform.performers
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TRIGGER works_set_updated_at
BEFORE UPDATE ON platform.works
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.users (
    user_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_email text NOT NULL CHECK (normalized_email = lower(btrim(normalized_email)) AND length(normalized_email) BETWEEN 3 AND 320),
    email_verified_at timestamptz,
    password_hash text NOT NULL CHECK (length(password_hash) BETWEEN 20 AND 500),
    role text NOT NULL DEFAULT 'user' CHECK (role IN ('owner', 'admin', 'editor', 'user')),
    account_status text NOT NULL DEFAULT 'pending' CHECK (account_status IN ('pending', 'active', 'locked', 'suspended', 'closed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    closed_at timestamptz,
    last_login_at timestamptz,
    CHECK ((account_status = 'closed' AND closed_at IS NOT NULL) OR (account_status <> 'closed' AND closed_at IS NULL)),
    CHECK (account_status <> 'active' OR email_verified_at IS NOT NULL)
);

CREATE UNIQUE INDEX users_open_email_uq
    ON platform.users (normalized_email)
    WHERE account_status <> 'closed';
CREATE INDEX users_status_idx ON platform.users (account_status, created_at DESC);

CREATE TRIGGER users_set_updated_at
BEFORE UPDATE ON platform.users
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.sessions (
    session_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE CASCADE,
    token_hash text NOT NULL UNIQUE CHECK (token_hash ~ '^[a-f0-9]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    revoke_reason text,
    CHECK (expires_at > created_at),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

CREATE INDEX sessions_active_user_idx ON platform.sessions (user_id, expires_at DESC)
    WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx ON platform.sessions (expires_at);

CREATE TABLE platform.email_challenges (
    challenge_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid REFERENCES platform.users(user_id) ON DELETE CASCADE,
    normalized_email text NOT NULL CHECK (normalized_email = lower(btrim(normalized_email)) AND length(normalized_email) BETWEEN 3 AND 320),
    purpose text NOT NULL CHECK (purpose IN ('signup', 'password_reset', 'account_close')),
    code_hash text NOT NULL CHECK (length(code_hash) BETWEEN 32 AND 500),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 5),
    sent_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    request_ip_hash text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > sent_at),
    CHECK (consumed_at IS NULL OR consumed_at >= sent_at),
    CHECK ((purpose = 'signup') OR user_id IS NOT NULL)
);

CREATE INDEX email_challenges_lookup_idx
    ON platform.email_challenges (normalized_email, purpose, sent_at DESC)
    WHERE consumed_at IS NULL;
CREATE INDEX email_challenges_expiry_idx ON platform.email_challenges (expires_at);

CREATE TABLE platform.security_rate_limits (
    rate_limit_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    action text NOT NULL CHECK (action IN ('signup_code', 'signup', 'login', 'password_code', 'password_reset', 'account_close', 'feedback')),
    dimension_type text NOT NULL CHECK (dimension_type IN ('email_hash', 'ip_hash', 'user_id')),
    dimension_hash text NOT NULL CHECK (length(dimension_hash) BETWEEN 16 AND 255),
    window_start timestamptz NOT NULL,
    window_seconds integer NOT NULL CHECK (window_seconds BETWEEN 1 AND 86400),
    request_count integer NOT NULL DEFAULT 0 CHECK (request_count >= 0),
    success_count integer NOT NULL DEFAULT 0 CHECK (success_count >= 0 AND success_count <= request_count),
    expires_at timestamptz NOT NULL,
    UNIQUE (action, dimension_type, dimension_hash, window_start, window_seconds)
);

CREATE INDEX security_rate_limits_expiry_idx ON platform.security_rate_limits (expires_at);

CREATE TABLE platform.favorites (
    favorite_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE CASCADE,
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    UNIQUE (user_id, work_id),
    CHECK ((is_deleted AND deleted_at IS NOT NULL) OR (NOT is_deleted AND deleted_at IS NULL))
);

CREATE INDEX favorites_visible_user_idx ON platform.favorites (user_id, created_at DESC)
    WHERE NOT is_deleted;

CREATE TABLE platform.follows (
    follow_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE CASCADE,
    performer_id uuid NOT NULL REFERENCES platform.performers(performer_id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    UNIQUE (user_id, performer_id),
    CHECK ((is_deleted AND deleted_at IS NOT NULL) OR (NOT is_deleted AND deleted_at IS NULL))
);

CREATE INDEX follows_visible_user_idx ON platform.follows (user_id, created_at DESC)
    WHERE NOT is_deleted;

CREATE TABLE platform.view_history (
    history_id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    content_type text NOT NULL CHECK (content_type IN ('work', 'performer', 'studio')),
    content_id uuid NOT NULL,
    viewed_at timestamptz NOT NULL DEFAULT now(),
    is_deleted boolean NOT NULL DEFAULT false,
    deleted_at timestamptz,
    archived_at timestamptz,
    CHECK ((is_deleted AND deleted_at IS NOT NULL) OR (NOT is_deleted AND deleted_at IS NULL)),
    CHECK (archived_at IS NULL OR is_deleted)
);

CREATE INDEX view_history_visible_user_idx ON platform.view_history (user_id, viewed_at DESC, history_id DESC)
    WHERE NOT is_deleted;
CREATE INDEX view_history_archive_idx ON platform.view_history (is_deleted, archived_at, viewed_at)
    WHERE is_deleted;

CREATE TABLE platform.hidden_preferences (
    preference_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE CASCADE,
    work_id uuid NOT NULL REFERENCES platform.works(work_id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, work_id)
);

CREATE TRIGGER hidden_preferences_set_updated_at
BEFORE UPDATE ON platform.hidden_preferences
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.feedback (
    feedback_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    content_type text CHECK (content_type IN ('work', 'performer', 'studio')),
    content_id uuid,
    feedback_type text NOT NULL CHECK (feedback_type IN ('correction', 'source_suggestion', 'rights', 'other')),
    message text NOT NULL CHECK (length(btrim(message)) BETWEEN 1 AND 4000),
    evidence_url text,
    review_status text NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending', 'reviewing', 'accepted', 'rejected', 'closed')),
    reviewer_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((content_type IS NULL AND content_id IS NULL) OR (content_type IS NOT NULL AND content_id IS NOT NULL)),
    CHECK (evidence_url IS NULL OR evidence_url ~* '^https?://'),
    CHECK ((review_status IN ('pending', 'reviewing') AND reviewed_at IS NULL)
        OR (review_status IN ('accepted', 'rejected', 'closed') AND reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL))
);

CREATE INDEX feedback_queue_idx ON platform.feedback (review_status, created_at);

CREATE TRIGGER feedback_set_updated_at
BEFORE UPDATE ON platform.feedback
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.content_metrics_hourly (
    content_type text NOT NULL CHECK (content_type IN ('work', 'performer', 'studio')),
    content_id uuid NOT NULL,
    hour_bucket timestamptz NOT NULL,
    page_views bigint NOT NULL DEFAULT 0 CHECK (page_views >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (content_type, content_id, hour_bucket),
    CHECK (hour_bucket = (date_trunc('hour', hour_bucket AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'))
);

CREATE INDEX content_metrics_hourly_rank_idx
    ON platform.content_metrics_hourly (hour_bucket DESC, page_views DESC);

CREATE TABLE platform.field_provenance (
    provenance_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio', 'media')),
    entity_id uuid NOT NULL,
    field_name text NOT NULL CHECK (length(field_name) BETWEEN 1 AND 100),
    source_id uuid REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    source_type text NOT NULL CHECK (length(source_type) BETWEEN 1 AND 100),
    source_url text,
    source_title text,
    checked_at timestamptz NOT NULL,
    operator_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    confidence numeric(4,3) NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    rights_status text NOT NULL CHECK (rights_status IN ('needs_review', 'allowed', 'restricted', 'takedown')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (source_url IS NULL OR source_url ~* '^https?://')
);

CREATE INDEX field_provenance_entity_idx ON platform.field_provenance (entity_type, entity_id, field_name, checked_at DESC);

CREATE TABLE platform.content_revisions (
    revision_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    entity_id uuid NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    revision_status text NOT NULL DEFAULT 'draft' CHECK (revision_status IN ('draft', 'reviewing', 'approved', 'rejected')),
    author_id uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    reviewer_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    created_at timestamptz NOT NULL DEFAULT now(),
    submitted_at timestamptz,
    reviewed_at timestamptz,
    UNIQUE (entity_type, entity_id, version),
    CHECK (reviewer_id IS NULL OR reviewer_id <> author_id),
    CHECK ((revision_status = 'draft' AND submitted_at IS NULL AND reviewed_at IS NULL) OR revision_status <> 'draft'),
    CHECK (revision_status = 'draft' OR submitted_at IS NOT NULL),
    CHECK ((revision_status IN ('approved', 'rejected') AND reviewer_id IS NOT NULL AND reviewed_at IS NOT NULL) OR revision_status IN ('draft', 'reviewing'))
);

CREATE INDEX content_revisions_entity_idx ON platform.content_revisions (entity_type, entity_id, version DESC);
CREATE INDEX content_revisions_review_idx ON platform.content_revisions (revision_status, submitted_at) WHERE revision_status = 'reviewing';

CREATE OR REPLACE FUNCTION platform.protect_revision_payload()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.entity_type IS DISTINCT FROM OLD.entity_type
        OR NEW.entity_id IS DISTINCT FROM OLD.entity_id
        OR NEW.version IS DISTINCT FROM OLD.version
        OR NEW.payload IS DISTINCT FROM OLD.payload
        OR NEW.author_id IS DISTINCT FROM OLD.author_id
        OR NEW.reason IS DISTINCT FROM OLD.reason
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'revision identity and payload are immutable';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER content_revisions_protect_payload
BEFORE UPDATE ON platform.content_revisions
FOR EACH ROW EXECUTE FUNCTION platform.protect_revision_payload();

CREATE TABLE platform.publications (
    publication_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    entity_id uuid NOT NULL,
    site text NOT NULL DEFAULT 'main' CHECK (site ~ '^[a-z0-9_-]+$'),
    current_revision_id uuid NOT NULL REFERENCES platform.content_revisions(revision_id) ON DELETE RESTRICT,
    publication_status text NOT NULL CHECK (publication_status IN ('published', 'hidden', 'takedown')),
    canonical_slug text NOT NULL CHECK (canonical_slug ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    published_at timestamptz,
    hidden_at timestamptz,
    takedown_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id, site),
    UNIQUE (site, entity_type, canonical_slug),
    CHECK ((publication_status = 'published' AND published_at IS NOT NULL AND hidden_at IS NULL AND takedown_at IS NULL)
        OR (publication_status = 'hidden' AND hidden_at IS NOT NULL AND takedown_at IS NULL)
        OR (publication_status = 'takedown' AND takedown_at IS NOT NULL))
);

CREATE INDEX publications_public_idx ON platform.publications (entity_type, published_at DESC)
    WHERE publication_status = 'published';

CREATE OR REPLACE FUNCTION platform.validate_publication_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    revision platform.content_revisions%ROWTYPE;
BEGIN
    SELECT * INTO revision
    FROM platform.content_revisions
    WHERE revision_id = NEW.current_revision_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'publication revision does not exist';
    END IF;
    IF revision.entity_type <> NEW.entity_type OR revision.entity_id <> NEW.entity_id THEN
        RAISE EXCEPTION 'publication entity does not match revision';
    END IF;
    IF revision.revision_status <> 'approved' THEN
        RAISE EXCEPTION 'only approved revisions can be published';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER publications_validate_revision
BEFORE INSERT OR UPDATE OF current_revision_id, entity_type, entity_id ON platform.publications
FOR EACH ROW EXECUTE FUNCTION platform.validate_publication_revision();

CREATE TRIGGER publications_set_updated_at
BEFORE UPDATE ON platform.publications
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE platform.redirects (
    redirect_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_path text NOT NULL CHECK (source_path LIKE '/%'),
    target_path text NOT NULL CHECK (target_path LIKE '/%'),
    status_code smallint NOT NULL DEFAULT 308 CHECK (status_code IN (301, 308)),
    active boolean NOT NULL DEFAULT true,
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_path),
    CHECK (source_path <> target_path)
);

CREATE TABLE platform.search_documents (
    search_document_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio')),
    entity_id uuid NOT NULL,
    canonical_code text,
    compact_code text,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 500),
    names text NOT NULL DEFAULT '',
    aliases text NOT NULL DEFAULT '',
    studio_name text NOT NULL DEFAULT '',
    search_text text GENERATED ALWAYS AS (
        coalesce(canonical_code, '') || ' ' || coalesce(compact_code, '') || ' ' || title || ' ' || names || ' ' || aliases || ' ' || studio_name
    ) STORED,
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple'::regconfig, coalesce(canonical_code, '') || ' ' || coalesce(compact_code, '') || ' ' || title || ' ' || names || ' ' || aliases || ' ' || studio_name)
    ) STORED,
    publication_id uuid NOT NULL UNIQUE REFERENCES platform.publications(publication_id) ON DELETE CASCADE,
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (entity_type, entity_id)
);

CREATE INDEX search_documents_code_idx ON platform.search_documents (canonical_code);
CREATE INDEX search_documents_compact_code_idx ON platform.search_documents (compact_code);
CREATE INDEX search_documents_vector_idx ON platform.search_documents USING gin (search_vector);
CREATE INDEX search_documents_trgm_idx ON platform.search_documents USING gin (search_text gin_trgm_ops);

CREATE TABLE platform.site_content_rules (
    rule_key text PRIMARY KEY CHECK (rule_key ~ '^[a-z0-9_.-]+$'),
    value jsonb NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    updated_by uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO platform.site_content_rules (rule_key, value)
VALUES ('home.discovery_mix', '{"work_slots":4,"performer_slots":1,"repeat_window":10}'::jsonb);

CREATE TABLE platform.outbox_events (
    event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type text NOT NULL CHECK (length(aggregate_type) BETWEEN 1 AND 100),
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('publication_changed', 'publication_hidden', 'publication_takedown', 'search_refresh', 'cache_purge', 'sitemap_refresh', 'email_send', 'history_archive', 'media_delete')),
    payload jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    dedupe_key text NOT NULL CHECK (length(dedupe_key) BETWEEN 1 AND 512),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'retry', 'completed', 'dead')),
    attempt_count integer NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 20),
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_expires_at timestamptz,
    last_error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (event_type, dedupe_key),
    CHECK ((status = 'running' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL) OR status <> 'running')
);

CREATE INDEX outbox_events_claim_idx ON platform.outbox_events (status, available_at, created_at)
    WHERE status IN ('pending', 'retry');

CREATE TABLE collector.review_tasks (
    review_task_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_type text NOT NULL CHECK (task_type IN ('new_entity', 'field_review', 'duplicate', 'conflict', 'media', 'rights', 'publication')),
    entity_type text CHECK (entity_type IN ('work', 'performer', 'studio', 'media')),
    entity_id uuid,
    normalized_record_id uuid REFERENCES collector.normalized_records(normalized_record_id) ON DELETE RESTRICT,
    priority smallint NOT NULL DEFAULT 100 CHECK (priority BETWEEN 1 AND 1000),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'approved', 'rejected', 'cancelled')),
    assignee_id uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    due_at timestamptz,
    claimed_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (entity_id IS NOT NULL OR normalized_record_id IS NOT NULL),
    CHECK ((status = 'claimed' AND assignee_id IS NOT NULL AND claimed_at IS NOT NULL) OR status <> 'claimed')
);

CREATE INDEX review_tasks_queue_idx ON collector.review_tasks (status, priority, created_at);

CREATE TRIGGER review_tasks_set_updated_at
BEFORE UPDATE ON collector.review_tasks
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

CREATE TABLE collector.conflict_reviews (
    conflict_review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_task_id uuid NOT NULL REFERENCES collector.review_tasks(review_task_id) ON DELETE CASCADE,
    entity_type text NOT NULL CHECK (entity_type IN ('work', 'performer', 'studio', 'media')),
    entity_id uuid NOT NULL,
    field_name text NOT NULL CHECK (length(field_name) BETWEEN 1 AND 100),
    current_value jsonb,
    candidate_value jsonb,
    source_id uuid REFERENCES collector.sources(source_id) ON DELETE RESTRICT,
    source_priority smallint CHECK (source_priority BETWEEN 1 AND 1000),
    resolution text CHECK (resolution IN ('keep_current', 'accept_candidate', 'mark_unknown', 'merge', 'reject')),
    resolved_by uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    resolved_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((resolution IS NULL AND resolved_by IS NULL AND resolved_at IS NULL)
        OR (resolution IS NOT NULL AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL))
);

CREATE INDEX conflict_reviews_open_idx ON collector.conflict_reviews (created_at)
    WHERE resolution IS NULL;

CREATE VIEW platform.public_published_works AS
SELECT
    work.work_id,
    work.canonical_code,
    work.compact_code,
    work.title,
    work.title_original,
    work.release_date,
    work.studio_id,
    work.edition_of,
    work.summary,
    publication.publication_id,
    publication.current_revision_id,
    publication.canonical_slug,
    publication.published_at,
    publication.updated_at
FROM platform.works AS work
JOIN platform.publications AS publication
  ON publication.entity_type = 'work'
 AND publication.entity_id = work.work_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE work.publication_status = 'published';

CREATE VIEW platform.public_published_performers AS
SELECT
    performer.performer_id,
    performer.display_name,
    performer.name_original,
    performer.romanized_name,
    performer.canonical_slug,
    performer.activity_status,
    performer.agency,
    performer.debut_year,
    publication.publication_id,
    publication.current_revision_id,
    publication.published_at,
    publication.updated_at
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer'
 AND publication.entity_id = performer.performer_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE performer.publication_status = 'published'
  AND performer.adult_status = 'verified';

CREATE VIEW platform.public_published_studios AS
SELECT
    studio.studio_id,
    studio.name,
    studio.normalized_name,
    studio.canonical_slug,
    publication.publication_id,
    publication.current_revision_id,
    publication.published_at,
    publication.updated_at
FROM platform.studios AS studio
JOIN platform.publications AS publication
  ON publication.entity_type = 'studio'
 AND publication.entity_id = studio.studio_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE studio.publication_status = 'published';

CREATE VIEW platform.public_search_documents AS
SELECT
    document.search_document_id,
    document.entity_type,
    document.entity_id,
    document.canonical_code,
    document.compact_code,
    document.title,
    document.names,
    document.aliases,
    document.studio_name,
    document.search_text,
    document.search_vector,
    document.publication_id,
    document.updated_at
FROM platform.search_documents AS document
JOIN platform.publications AS publication
  ON publication.publication_id = document.publication_id
 AND publication.publication_status = 'published'
 AND publication.site = 'main';

UPDATE platform.system_metadata SET schema_version = 3 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 2 WHERE singleton;

DROP VIEW IF EXISTS platform.public_search_documents;
DROP VIEW IF EXISTS platform.public_published_studios;
DROP VIEW IF EXISTS platform.public_published_performers;
DROP VIEW IF EXISTS platform.public_published_works;
DROP TABLE IF EXISTS collector.conflict_reviews;
DROP TRIGGER IF EXISTS review_tasks_set_updated_at ON collector.review_tasks;
DROP TABLE IF EXISTS collector.review_tasks;
DROP TABLE IF EXISTS platform.outbox_events;
DROP TABLE IF EXISTS platform.site_content_rules;
DROP TABLE IF EXISTS platform.search_documents;
DROP TABLE IF EXISTS platform.redirects;
DROP TRIGGER IF EXISTS publications_set_updated_at ON platform.publications;
DROP TRIGGER IF EXISTS publications_validate_revision ON platform.publications;
DROP TABLE IF EXISTS platform.publications;
DROP FUNCTION IF EXISTS platform.validate_publication_revision();
DROP TRIGGER IF EXISTS content_revisions_protect_payload ON platform.content_revisions;
DROP TABLE IF EXISTS platform.content_revisions;
DROP FUNCTION IF EXISTS platform.protect_revision_payload();
DROP TABLE IF EXISTS platform.field_provenance;
DROP TABLE IF EXISTS platform.content_metrics_hourly;
DROP TRIGGER IF EXISTS feedback_set_updated_at ON platform.feedback;
DROP TABLE IF EXISTS platform.feedback;
DROP TRIGGER IF EXISTS hidden_preferences_set_updated_at ON platform.hidden_preferences;
DROP TABLE IF EXISTS platform.hidden_preferences;
DROP TABLE IF EXISTS platform.view_history;
DROP TABLE IF EXISTS platform.follows;
DROP TABLE IF EXISTS platform.favorites;
DROP TABLE IF EXISTS platform.security_rate_limits;
DROP TABLE IF EXISTS platform.email_challenges;
DROP TABLE IF EXISTS platform.sessions;
DROP TRIGGER IF EXISTS users_set_updated_at ON platform.users;
DROP TABLE IF EXISTS platform.users;
DROP TRIGGER IF EXISTS works_set_updated_at ON platform.works;
DROP TRIGGER IF EXISTS performers_set_updated_at ON platform.performers;
DROP TRIGGER IF EXISTS tags_set_updated_at ON platform.tags;
DROP TRIGGER IF EXISTS studios_set_updated_at ON platform.studios;
DROP TRIGGER IF EXISTS jobs_set_updated_at ON collector.jobs;
DROP TRIGGER IF EXISTS source_connectors_set_updated_at ON collector.source_connectors;
DROP TRIGGER IF EXISTS sources_set_updated_at ON collector.sources;
DROP FUNCTION IF EXISTS platform.set_updated_at();
