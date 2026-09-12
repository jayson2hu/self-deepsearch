-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION platform.valid_media_upload_receipt(value jsonb) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE generation bigint;
BEGIN
    IF value IS NULL OR jsonb_typeof(value)<>'object' OR octet_length(value::text)>4096 OR
       NOT (value ?& ARRAY['status','epoch','generation','mode','command_id','applied_at']) OR
       (value-ARRAY['status','epoch','generation','mode','command_id','applied_at'])<>'{}'::jsonb OR
       jsonb_typeof(value->'generation')<>'number' OR (value->>'generation') !~ '^[0-9]{1,16}$' OR
       EXISTS (SELECT 1 FROM jsonb_each(value) field WHERE field.key<>'generation' AND jsonb_typeof(field.value)<>'string') OR
       value->>'status' NOT IN ('observed','applied','replayed') OR value->>'mode' NOT IN ('paused','enabled') THEN
        RETURN false;
    END IF;
    generation := (value->>'generation')::bigint;
    IF generation=0 THEN
        RETURN value->>'status'='observed' AND value->>'mode'='paused' AND value->>'epoch'='' AND value->>'command_id'='' AND value->>'applied_at'='';
    END IF;
    IF generation>9007199254740991 OR
       value->>'epoch' !~ '^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$' OR
       value->>'command_id' !~ '^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$' OR
       value->>'applied_at' !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' THEN
        RETURN false;
    END IF;
    PERFORM (value->>'applied_at')::timestamptz;
    RETURN true;
EXCEPTION WHEN invalid_datetime_format OR datetime_field_overflow OR numeric_value_out_of_range THEN
    RETURN false;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.valid_media_upload_receipt(jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.valid_media_upload_receipt(jsonb) TO platform_api,platform_worker;

CREATE TABLE platform.media_upload_control_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    target_ref text NOT NULL CHECK (target_ref ~ '^[a-f0-9]{64}$'),
    last_receipt jsonb,
    last_success_at timestamptz,
    last_error_code text CHECK (last_error_code ~ '^[a-z_]{1,64}$'),
    updated_at timestamptz NOT NULL,
    next_poll_at timestamptz NOT NULL,
    lease_token uuid,
    lease_until timestamptz,
    CHECK ((last_receipt IS NULL) = (last_success_at IS NULL)),
    CHECK (last_receipt IS NULL OR (platform.valid_media_upload_receipt(last_receipt) AND last_receipt->>'status'='observed')),
    CHECK ((lease_token IS NULL) = (lease_until IS NULL)),
    CHECK (last_success_at IS NULL OR last_success_at <= updated_at)
);

CREATE TABLE audit.media_upload_commands (
    command_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id uuid NOT NULL REFERENCES platform.users(user_id),
    idempotency_key uuid NOT NULL,
    target_ref text NOT NULL CHECK (target_ref ~ '^[a-f0-9]{64}$'),
    expected_epoch text NOT NULL,
    expected_generation bigint NOT NULL CHECK (expected_generation BETWEEN 0 AND 9007199254740990),
    expected_observed_at timestamptz NOT NULL,
    mode text NOT NULL CHECK (mode IN ('paused','enabled')),
    reason text NOT NULL CHECK (reason=btrim(reason) AND char_length(reason) BETWEEN 2 AND 1000 AND reason !~ '[[:cntrl:]]'),
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','uncertain','applied','rejected','superseded')),
    first_dispatched_at timestamptz,
    dispatch_count integer NOT NULL DEFAULT 0 CHECK (dispatch_count BETWEEN 0 AND 1000000),
    completed_at timestamptz,
    error_code text CHECK (error_code ~ '^[a-z_]{1,64}$'),
    receipt jsonb,
    UNIQUE (actor_id,idempotency_key),
    CHECK ((expected_generation=0 AND expected_epoch='' AND mode='paused') OR
           (expected_generation>0 AND expected_epoch ~ '^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$')),
    CHECK (expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
    CHECK (date_trunc('second',expires_at)=expires_at),
    CHECK (expected_observed_at<=created_at AND expected_observed_at>=created_at-interval '2 minutes'),
    CHECK (first_dispatched_at IS NULL OR (first_dispatched_at>=created_at AND first_dispatched_at<expires_at)),
    CHECK ((first_dispatched_at IS NULL) = (dispatch_count=0)),
    CHECK (completed_at IS NULL OR completed_at>=created_at),
    CHECK ((status IN ('pending','running','uncertain')) = (completed_at IS NULL)),
    CHECK (receipt IS NULL OR platform.valid_media_upload_receipt(receipt)),
    CHECK (status<>'applied' OR (first_dispatched_at IS NOT NULL AND receipt IS NOT NULL AND error_code IS NULL)),
    CHECK (status NOT IN ('rejected','superseded') OR error_code IS NOT NULL)
);
CREATE UNIQUE INDEX media_upload_one_open_command_idx ON audit.media_upload_commands ((true))
    WHERE status IN ('pending','running','uncertain');
CREATE INDEX media_upload_commands_recent_idx ON audit.media_upload_commands (created_at DESC,command_id DESC);

-- Never accept an old/replaced Beijing ledger as a new healthy baseline.
-- Recovery of an epoch requires a stopped, separately reviewed migration.
-- +goose StatementBegin
CREATE FUNCTION platform.guard_media_upload_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.target_ref IS DISTINCT FROM OLD.target_ref OR NEW.updated_at<OLD.updated_at OR
       (OLD.last_success_at IS NOT NULL AND (NEW.last_success_at IS NULL OR NEW.last_success_at<OLD.last_success_at)) OR
       (OLD.last_receipt IS NOT NULL AND (
           NEW.last_receipt IS NULL OR
           (OLD.last_receipt->>'generation')::bigint>(NEW.last_receipt->>'generation')::bigint OR
           ((OLD.last_receipt->>'generation')::bigint>0 AND NEW.last_receipt->>'epoch' IS DISTINCT FROM OLD.last_receipt->>'epoch') OR
           (NEW.last_receipt->>'generation'=OLD.last_receipt->>'generation' AND NEW.last_receipt IS DISTINCT FROM OLD.last_receipt))) THEN
        RAISE EXCEPTION 'upload target and confirmed state cannot regress' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER media_upload_state_guard BEFORE UPDATE ON platform.media_upload_control_state
FOR EACH ROW EXECUTE FUNCTION platform.guard_media_upload_state();
REVOKE ALL ON FUNCTION platform.guard_media_upload_state() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION audit.guard_media_upload_command() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN
        IF NEW.status<>'pending' OR NEW.first_dispatched_at IS NOT NULL OR NEW.dispatch_count<>0 OR
           NEW.receipt IS NOT NULL OR NEW.error_code IS NOT NULL OR NOT platform.lock_media_usage_reviewer(NEW.actor_id) THEN
            RAISE EXCEPTION 'upload command must start as an authorized pending request' USING ERRCODE='23514';
        END IF;
    ELSE
        IF NEW.dispatch_count>OLD.dispatch_count AND
           (NEW.dispatch_count<>OLD.dispatch_count+1 OR NEW.status<>'running' OR
            NEW.expires_at<=clock_timestamp() OR NOT platform.lock_media_usage_reviewer(NEW.actor_id)) THEN
            RAISE EXCEPTION 'dispatch requires an active operator and an unexpired request' USING ERRCODE='23514';
        END IF;
        IF NEW.status='running' AND NEW.dispatch_count=OLD.dispatch_count THEN
            RAISE EXCEPTION 'running state requires a new durable attempt' USING ERRCODE='23514';
        END IF;
        IF NEW.status='superseded' AND
           (NEW.first_dispatched_at IS NULL OR NEW.receipt IS NULL OR
            (NEW.receipt->>'epoch'=NEW.expected_epoch AND NEW.receipt->>'generation'=NEW.expected_generation::text)) THEN
            RAISE EXCEPTION 'superseded command requires changed remote evidence' USING ERRCODE='23514';
        END IF;
        IF OLD.status IN ('applied','rejected','superseded') OR
           (to_jsonb(NEW)-ARRAY['status','first_dispatched_at','dispatch_count','completed_at','error_code','receipt']) IS DISTINCT FROM
           (to_jsonb(OLD)-ARRAY['status','first_dispatched_at','dispatch_count','completed_at','error_code','receipt']) OR
           (OLD.first_dispatched_at IS NOT NULL AND NEW.first_dispatched_at IS DISTINCT FROM OLD.first_dispatched_at) OR
           NEW.dispatch_count<OLD.dispatch_count THEN
            RAISE EXCEPTION 'upload command request and final evidence are immutable' USING ERRCODE='23514';
        END IF;
        IF NEW.status='applied' AND (
            NEW.receipt->>'command_id' IS DISTINCT FROM NEW.command_id::text OR
            NEW.receipt->>'epoch' IS DISTINCT FROM CASE WHEN NEW.expected_epoch='' THEN NEW.command_id::text ELSE NEW.expected_epoch END OR
            NEW.receipt->>'generation' IS DISTINCT FROM (NEW.expected_generation+1)::text OR
            NEW.receipt->>'mode' IS DISTINCT FROM NEW.mode OR
            coalesce(NEW.receipt->>'status','') NOT IN ('observed','applied','replayed')) THEN
            RAISE EXCEPTION 'applied upload command requires its exact remote receipt' USING ERRCODE='23514';
        END IF;
        IF NEW.status='rejected' AND NEW.first_dispatched_at IS NOT NULL AND
           (NEW.error_code IS DISTINCT FROM 'control_request_expired' OR NEW.completed_at<NEW.expires_at+interval '310 seconds' OR
            NEW.receipt IS NULL OR NEW.receipt->>'generation' IS DISTINCT FROM NEW.expected_generation::text OR
            NEW.receipt->>'epoch' IS DISTINCT FROM NEW.expected_epoch) THEN
            RAISE EXCEPTION 'attempted upload command requires expiry and unchanged remote evidence' USING ERRCODE='23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER media_upload_command_guard BEFORE INSERT OR UPDATE ON audit.media_upload_commands
FOR EACH ROW EXECUTE FUNCTION audit.guard_media_upload_command();
REVOKE ALL ON FUNCTION audit.guard_media_upload_command() FROM PUBLIC;

-- The API cannot UPDATE execution state; this fixed projection only locks it.
-- +goose StatementBegin
CREATE FUNCTION platform.lock_media_upload_control_state()
RETURNS TABLE(target_ref text,last_receipt jsonb,last_success_at timestamptz,last_error_code text,lease_live boolean)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog AS $$
    SELECT state.target_ref,state.last_receipt,state.last_success_at,state.last_error_code,
           coalesce(state.lease_until>clock_timestamp(),false)
    FROM platform.media_upload_control_state state WHERE state.singleton FOR UPDATE;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION platform.lock_media_upload_control_state() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION platform.lock_media_upload_control_state() TO platform_api;
GRANT SELECT ON platform.media_upload_control_state TO platform_api;
GRANT SELECT,INSERT,UPDATE ON platform.media_upload_control_state TO platform_worker;
GRANT SELECT,INSERT ON audit.media_upload_commands TO platform_api;
GRANT SELECT ON audit.media_upload_commands TO platform_worker;
GRANT UPDATE(status,first_dispatched_at,dispatch_count,completed_at,error_code,receipt) ON audit.media_upload_commands TO platform_worker;
UPDATE platform.system_metadata SET schema_version = 21 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 20 WHERE singleton;
DROP FUNCTION platform.lock_media_upload_control_state();
DROP TABLE audit.media_upload_commands;
DROP FUNCTION audit.guard_media_upload_command();
DROP TABLE platform.media_upload_control_state;
DROP FUNCTION platform.guard_media_upload_state();
DROP FUNCTION platform.valid_media_upload_receipt(jsonb);
