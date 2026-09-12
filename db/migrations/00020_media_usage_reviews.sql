-- +goose Up
CREATE TABLE audit.media_usage_reviews (
    review_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id uuid NOT NULL REFERENCES platform.users(user_id),
    idempotency_key uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('acknowledge','rotate_period')),
    expected_digest text NOT NULL CHECK (expected_digest ~ '^[a-f0-9]{64}$'),
    expected_updated_at timestamptz NOT NULL,
    next_period_start timestamptz,
    next_period_end timestamptz,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 2 AND 1000),
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','applied','rejected')),
    completed_at timestamptz,
    error_code text,
    before_state jsonb,
    after_state jsonb,
    UNIQUE (actor_id, idempotency_key),
    CHECK (expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
    CHECK ((action='acknowledge' AND next_period_start IS NULL AND next_period_end IS NULL) OR
           (action='rotate_period' AND next_period_start IS NOT NULL AND next_period_end IS NOT NULL AND
            next_period_end > next_period_start AND next_period_end <= next_period_start + interval '744 hours')),
    CHECK (completed_at IS NULL OR completed_at >= created_at),
    CHECK (before_state IS NULL OR (jsonb_typeof(before_state)='object' AND octet_length(before_state::text)<=8192)),
    CHECK (after_state IS NULL OR (jsonb_typeof(after_state)='object' AND octet_length(after_state::text)<=8192)),
    CHECK ((status='pending' AND completed_at IS NULL AND error_code IS NULL AND before_state IS NULL AND after_state IS NULL) OR
           (status='applied' AND completed_at IS NOT NULL AND error_code IS NULL AND before_state IS NOT NULL AND after_state IS NOT NULL) OR
           (status='rejected' AND completed_at IS NOT NULL AND error_code IS NOT NULL AND error_code ~ '^[a-z_]{1,64}$' AND before_state IS NULL AND after_state IS NULL))
);
CREATE UNIQUE INDEX media_usage_one_pending_review_idx ON audit.media_usage_reviews ((true)) WHERE status='pending';
CREATE INDEX media_usage_reviews_created_idx ON audit.media_usage_reviews (created_at DESC);
ALTER TABLE platform.media_usage_state ADD COLUMN last_review_id uuid REFERENCES audit.media_usage_reviews(review_id);

-- This fixed, boolean-only helper locks an actor without giving the Worker
-- SELECT on email/password/session data or UPDATE on users. No dynamic SQL.
-- +goose StatementBegin
CREATE FUNCTION platform.lock_media_usage_reviewer(actor uuid) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog AS $$
DECLARE allowed boolean;
BEGIN
    SELECT account_status='active' AND role IN ('owner','admin') INTO allowed
    FROM platform.users WHERE user_id=actor FOR SHARE;
    RETURN coalesce(allowed,false);
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.lock_media_usage_reviewer(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.lock_media_usage_reviewer(uuid) TO platform_api, platform_worker;

-- SELECT ... FOR UPDATE normally requires UPDATE privilege. Keep the API
-- state-read-only and expose only the fixed singleton lock/CAS projection.
-- +goose StatementBegin
CREATE FUNCTION platform.lock_media_usage_review_state()
RETURNS TABLE(config_digest text, updated_at timestamptz, period_end timestamptz, active_run boolean)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog AS $$
    SELECT state.config_digest,state.updated_at,state.period_end,state.active_run_id IS NOT NULL
    FROM platform.media_usage_state AS state WHERE state.singleton FOR UPDATE;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.lock_media_usage_review_state() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.lock_media_usage_review_state() TO platform_api;

-- +goose StatementBegin
CREATE FUNCTION audit.guard_media_usage_review() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status <> 'pending' THEN
            RAISE EXCEPTION 'review must start pending' USING ERRCODE='23514';
        END IF;
    ELSIF OLD.status <> 'pending' OR NEW.status='pending' OR
          (to_jsonb(NEW) - ARRAY['status','completed_at','error_code','before_state','after_state']) IS DISTINCT FROM
          (to_jsonb(OLD) - ARRAY['status','completed_at','error_code','before_state','after_state']) THEN
        RAISE EXCEPTION 'review evidence is immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER media_usage_review_guard BEFORE INSERT OR UPDATE ON audit.media_usage_reviews
FOR EACH ROW EXECUTE FUNCTION audit.guard_media_usage_review();
REVOKE ALL ON FUNCTION audit.guard_media_usage_review() FROM PUBLIC;

-- Existing normal Worker updates remain monotonic. A narrowly authorized
-- transition must match one API-created, immutable review and both snapshots.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.guard_media_usage_state() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review audit.media_usage_reviews%ROWTYPE;
BEGIN
    IF NEW.last_review_id IS DISTINCT FROM OLD.last_review_id THEN
        SELECT * INTO review FROM audit.media_usage_reviews WHERE review_id=NEW.last_review_id;
        IF NOT FOUND OR review.status <> 'applied' OR review.before_state IS DISTINCT FROM to_jsonb(OLD) OR
           review.after_state IS DISTINCT FROM to_jsonb(NEW) OR review.expected_digest <> OLD.config_digest OR
           review.expected_updated_at <> OLD.updated_at OR OLD.active_run_id IS NOT NULL OR
           EXISTS (SELECT 1 FROM audit.media_usage_runs WHERE run_status='running') OR
           review.created_at > clock_timestamp() OR review.expires_at <= clock_timestamp() OR
           NOT platform.lock_media_usage_reviewer(review.actor_id) THEN
            RAISE EXCEPTION 'review cannot authorize transition' USING ERRCODE='23514';
        END IF;
        IF review.action='acknowledge' THEN
            IF NOT OLD.review_required OR OLD.stop_recommended OR OLD.last_success_at IS NULL OR OLD.last_until IS NULL OR
               OLD.last_status NOT IN ('low_estimate','warning') OR OLD.last_until < clock_timestamp()-interval '30 minutes' OR
               (OLD.policy_snapshot->>'class_a_limit') IS NULL OR (OLD.policy_snapshot->>'class_b_limit') IS NULL OR
               OLD.high_class_a >= ceil((OLD.policy_snapshot->>'class_a_limit')::numeric*0.85) OR
               OLD.high_class_b >= ceil((OLD.policy_snapshot->>'class_b_limit')::numeric*0.85) OR
               OLD.last_success_at < clock_timestamp()-interval '30 minutes' OR OLD.last_until > clock_timestamp() OR
               OLD.last_success_at > clock_timestamp() OR OLD.period_start > clock_timestamp() OR OLD.period_end <= clock_timestamp() OR
               NEW.review_required OR NEW.review_since IS NOT NULL OR NEW.review_reason IS NOT NULL OR
               (to_jsonb(NEW)-ARRAY['last_review_id','review_required','review_since','review_reason','updated_at']) IS DISTINCT FROM
               (to_jsonb(OLD)-ARRAY['last_review_id','review_required','review_since','review_reason','updated_at']) THEN
                RAISE EXCEPTION 'review requires fresh non-stop evidence' USING ERRCODE='23514';
            END IF;
        ELSE
            IF NEW.account_ref <> OLD.account_ref OR NEW.policy_snapshot IS DISTINCT FROM OLD.policy_snapshot OR
               NEW.period_start IS DISTINCT FROM review.next_period_start OR NEW.period_end IS DISTINCT FROM review.next_period_end OR
               NEW.period_start < OLD.period_end OR NEW.period_start > clock_timestamp() OR NEW.period_end <= clock_timestamp() OR
               NEW.config_digest = OLD.config_digest OR NEW.high_class_a<>0 OR NEW.high_class_b<>0 OR NEW.high_free<>0 OR
               NEW.last_success_at IS NOT NULL OR NEW.last_until IS NOT NULL OR NOT NEW.review_required OR NEW.stop_recommended OR
               NEW.review_reason IS DISTINCT FROM 'usage_period_rotated' OR NEW.review_since IS DISTINCT FROM NEW.updated_at OR
               NEW.last_status <> 'unknown' OR NEW.last_reason <> 'usage_period_rotated' OR NEW.last_recommendation <> 'hold_for_review' OR
               NEW.next_poll_at IS DISTINCT FROM NEW.updated_at OR NEW.active_run_id IS NOT NULL OR NEW.lease_until IS NOT NULL THEN
                RAISE EXCEPTION 'period rotation must retain a review hold' USING ERRCODE='23514';
            END IF;
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.config_digest IS DISTINCT FROM OLD.config_digest OR NEW.account_ref IS DISTINCT FROM OLD.account_ref OR
       NEW.period_start IS DISTINCT FROM OLD.period_start OR NEW.period_end IS DISTINCT FROM OLD.period_end OR NEW.policy_snapshot IS DISTINCT FROM OLD.policy_snapshot OR
       NEW.high_class_a < OLD.high_class_a OR NEW.high_class_b < OLD.high_class_b OR NEW.high_free < OLD.high_free OR
       (OLD.review_required AND (NOT NEW.review_required OR NEW.review_since IS DISTINCT FROM OLD.review_since OR NEW.review_reason IS DISTINCT FROM OLD.review_reason)) OR
       (OLD.stop_recommended AND NOT NEW.stop_recommended) OR
       (OLD.last_success_at IS NOT NULL AND (NEW.last_success_at IS NULL OR NEW.last_success_at < OLD.last_success_at OR NEW.last_until < OLD.last_until)) THEN
        RAISE EXCEPTION 'media usage state cannot be implicitly reset' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
GRANT SELECT, INSERT ON audit.media_usage_reviews TO platform_api;
GRANT SELECT, UPDATE ON audit.media_usage_reviews TO platform_worker;
-- Neither code nor a runtime SQL writer may finalize a receipt without the
-- matching state transition in the same transaction.
-- +goose StatementBegin
CREATE FUNCTION audit.verify_media_usage_review_applied() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status='applied' AND NOT EXISTS (
        SELECT 1 FROM platform.media_usage_state state
        WHERE state.singleton AND state.last_review_id=NEW.review_id AND to_jsonb(state)=NEW.after_state
    ) THEN
        RAISE EXCEPTION 'applied review requires committed state evidence' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER media_usage_review_applied_check AFTER UPDATE ON audit.media_usage_reviews
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION audit.verify_media_usage_review_applied();
REVOKE ALL ON FUNCTION audit.verify_media_usage_review_applied() FROM PUBLIC;
UPDATE platform.system_metadata SET schema_version = 20 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 19 WHERE singleton;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION platform.guard_media_usage_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.config_digest IS DISTINCT FROM OLD.config_digest OR NEW.account_ref IS DISTINCT FROM OLD.account_ref OR
       NEW.period_start IS DISTINCT FROM OLD.period_start OR NEW.period_end IS DISTINCT FROM OLD.period_end OR NEW.policy_snapshot IS DISTINCT FROM OLD.policy_snapshot OR
       NEW.high_class_a < OLD.high_class_a OR NEW.high_class_b < OLD.high_class_b OR NEW.high_free < OLD.high_free OR
       (OLD.review_required AND (NOT NEW.review_required OR NEW.review_since IS DISTINCT FROM OLD.review_since OR NEW.review_reason IS DISTINCT FROM OLD.review_reason)) OR
       (OLD.stop_recommended AND NOT NEW.stop_recommended) OR
       (OLD.last_success_at IS NOT NULL AND (NEW.last_success_at IS NULL OR NEW.last_success_at < OLD.last_success_at OR NEW.last_until < OLD.last_until)) THEN
        RAISE EXCEPTION 'media usage state cannot be implicitly reset' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER TABLE platform.media_usage_state DROP COLUMN last_review_id;
DROP TABLE audit.media_usage_reviews;
DROP FUNCTION audit.guard_media_usage_review();
DROP FUNCTION audit.verify_media_usage_review_applied();
DROP FUNCTION platform.lock_media_usage_reviewer(uuid);
DROP FUNCTION platform.lock_media_usage_review_state();
