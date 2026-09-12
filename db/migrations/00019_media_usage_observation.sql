-- +goose Up
CREATE TABLE platform.media_usage_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    config_digest text NOT NULL CHECK (config_digest ~ '^[a-f0-9]{64}$'),
    account_ref text NOT NULL CHECK (account_ref ~ '^[a-f0-9]{64}$'),
    policy_snapshot jsonb NOT NULL CHECK (jsonb_typeof(policy_snapshot)='object' AND octet_length(policy_snapshot::text)<=1024),
    period_start timestamptz NOT NULL,
    period_end timestamptz NOT NULL,
    high_class_a numeric(20,0) NOT NULL DEFAULT 0 CHECK (high_class_a BETWEEN 0 AND 18446744073709551615),
    high_class_b numeric(20,0) NOT NULL DEFAULT 0 CHECK (high_class_b BETWEEN 0 AND 18446744073709551615),
    high_free numeric(20,0) NOT NULL DEFAULT 0 CHECK (high_free BETWEEN 0 AND 18446744073709551615),
    last_success_at timestamptz,
    last_until timestamptz,
    review_required boolean NOT NULL DEFAULT false,
    review_since timestamptz,
    review_reason text,
    stop_recommended boolean NOT NULL DEFAULT false,
    last_status text NOT NULL DEFAULT 'unknown' CHECK (last_status IN ('unknown','low_estimate','warning','high','stop_recommended')),
    last_reason text NOT NULL DEFAULT 'usage_no_data' CHECK (last_reason ~ '^[a-z_]{1,64}$'),
    last_recommendation text NOT NULL DEFAULT 'hold_for_review' CHECK (last_recommendation IN ('observe_only','review_usage','pause_nonessential','stop_media_review_required','hold_for_review')),
    updated_at timestamptz NOT NULL,
    next_poll_at timestamptz NOT NULL,
    active_run_id uuid,
    lease_until timestamptz,
    CHECK (period_end > period_start AND period_end <= period_start + interval '744 hours'),
    CHECK ((last_success_at IS NULL) = (last_until IS NULL)),
    CHECK (last_until IS NULL OR (last_until >= period_start AND last_until < period_end AND last_success_at >= last_until)),
    CHECK ((review_required AND review_since IS NOT NULL AND review_reason IS NOT NULL AND review_reason ~ '^[a-z_]{1,64}$') OR
           (NOT review_required AND review_since IS NULL AND review_reason IS NULL)),
    CHECK (NOT stop_recommended OR review_required),
    CHECK ((active_run_id IS NULL) = (lease_until IS NULL))
);

CREATE TABLE audit.media_usage_runs (
    run_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    config_digest text NOT NULL CHECK (config_digest ~ '^[a-f0-9]{64}$'),
    run_status text NOT NULL CHECK (run_status IN ('running','completed','failed')),
    window_start timestamptz NOT NULL,
    window_end timestamptz NOT NULL,
    started_at timestamptz NOT NULL,
    completed_at timestamptz,
    observation jsonb,
    assessment jsonb,
    error_code text,
    CHECK (window_start <= window_end),
    CHECK (completed_at IS NULL OR completed_at >= started_at),
    CHECK (observation IS NULL OR (jsonb_typeof(observation) = 'object' AND octet_length(observation::text) <= 4096)),
    CHECK (assessment IS NULL OR (jsonb_typeof(assessment) = 'object' AND octet_length(assessment::text) <= 1024)),
    CHECK (
        (run_status = 'running' AND completed_at IS NULL AND observation IS NULL AND assessment IS NULL AND error_code IS NULL) OR
        (run_status = 'completed' AND completed_at IS NOT NULL AND observation IS NOT NULL AND assessment IS NOT NULL AND error_code IS NULL) OR
        (run_status = 'failed' AND completed_at IS NOT NULL AND observation IS NULL AND error_code IS NOT NULL AND error_code ~ '^[a-z_]{1,64}$')
    )
);
CREATE INDEX media_usage_runs_started_idx ON audit.media_usage_runs (started_at DESC);
CREATE UNIQUE INDEX media_usage_one_running_idx ON audit.media_usage_runs ((true)) WHERE run_status = 'running';

-- Runtime writes cannot lower counters, clear a hold, or silently change the
-- account/period/budget identity. A later authorized review path needs its own
-- audited database contract; direct runtime UPDATE is deliberately insufficient.
-- +goose StatementBegin
CREATE FUNCTION platform.guard_media_usage_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.config_digest IS DISTINCT FROM OLD.config_digest OR NEW.account_ref IS DISTINCT FROM OLD.account_ref OR
       NEW.period_start IS DISTINCT FROM OLD.period_start OR NEW.period_end IS DISTINCT FROM OLD.period_end OR NEW.policy_snapshot IS DISTINCT FROM OLD.policy_snapshot OR
       NEW.high_class_a < OLD.high_class_a OR NEW.high_class_b < OLD.high_class_b OR NEW.high_free < OLD.high_free OR
       (OLD.review_required AND (NOT NEW.review_required OR NEW.review_since IS DISTINCT FROM OLD.review_since OR NEW.review_reason IS DISTINCT FROM OLD.review_reason)) OR
       (OLD.stop_recommended AND NOT NEW.stop_recommended) OR
       (OLD.last_success_at IS NOT NULL AND (NEW.last_success_at IS NULL OR NEW.last_success_at < OLD.last_success_at OR NEW.last_until < OLD.last_until)) THEN
        RAISE EXCEPTION 'media usage state cannot be implicitly reset' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER media_usage_state_guard BEFORE UPDATE ON platform.media_usage_state FOR EACH ROW EXECUTE FUNCTION platform.guard_media_usage_state();

-- +goose StatementBegin
CREATE FUNCTION audit.guard_media_usage_run() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.run_status <> 'running' OR NEW.run_status = 'running' OR NEW.run_id IS DISTINCT FROM OLD.run_id OR
       NEW.config_digest IS DISTINCT FROM OLD.config_digest OR NEW.window_start IS DISTINCT FROM OLD.window_start OR
       NEW.window_end IS DISTINCT FROM OLD.window_end OR NEW.started_at IS DISTINCT FROM OLD.started_at THEN
        RAISE EXCEPTION 'media usage run is immutable after completion' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER media_usage_run_guard BEFORE UPDATE ON audit.media_usage_runs FOR EACH ROW EXECUTE FUNCTION audit.guard_media_usage_run();

GRANT SELECT, INSERT, UPDATE ON platform.media_usage_state, audit.media_usage_runs TO platform_worker;
GRANT SELECT ON platform.media_usage_state, audit.media_usage_runs TO platform_api;
REVOKE ALL ON FUNCTION platform.guard_media_usage_state(), audit.guard_media_usage_run() FROM PUBLIC;
UPDATE platform.system_metadata SET schema_version = 19 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 18 WHERE singleton;
REVOKE ALL ON platform.media_usage_state, audit.media_usage_runs FROM platform_worker, platform_api;
DROP TABLE audit.media_usage_runs;
DROP TABLE platform.media_usage_state;
DROP FUNCTION audit.guard_media_usage_run();
DROP FUNCTION platform.guard_media_usage_state();
