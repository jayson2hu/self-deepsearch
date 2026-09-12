-- +goose Up
ALTER TABLE platform.security_rate_limits
    DROP CONSTRAINT security_rate_limits_action_check;
ALTER TABLE platform.security_rate_limits
    ADD CONSTRAINT security_rate_limits_action_check CHECK (action IN ('signup_code', 'signup', 'login', 'password_code', 'password_reset', 'account_close', 'feedback', 'invitation_code', 'invitation_accept'));

CREATE TABLE platform.user_invitations (
    invitation_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    normalized_email text NOT NULL CHECK (normalized_email = lower(btrim(normalized_email)) AND length(normalized_email) BETWEEN 3 AND 320),
    role text NOT NULL CHECK (role IN ('admin', 'editor', 'user')),
    invitation_code_hash text NOT NULL CHECK (invitation_code_hash ~ '^[a-f0-9]{64}$'),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 5),
    invited_by uuid NOT NULL REFERENCES platform.users(user_id) ON DELETE RESTRICT,
    request_ip_hash text NOT NULL CHECK (length(request_ip_hash) BETWEEN 16 AND 255),
    sent_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > sent_at),
    CHECK (consumed_at IS NULL OR consumed_at >= sent_at),
    CHECK ((status = 'pending' AND consumed_at IS NULL) OR (status <> 'pending' AND consumed_at IS NOT NULL))
);

CREATE UNIQUE INDEX user_invitations_pending_email_uq
    ON platform.user_invitations (normalized_email)
    WHERE status = 'pending';
CREATE INDEX user_invitations_status_idx
    ON platform.user_invitations (status, created_at DESC);
CREATE INDEX user_invitations_expiry_idx
    ON platform.user_invitations (expires_at)
    WHERE status = 'pending';

CREATE TRIGGER user_invitations_set_updated_at
BEFORE UPDATE ON platform.user_invitations
FOR EACH ROW EXECUTE FUNCTION platform.set_updated_at();

GRANT SELECT, INSERT, UPDATE ON platform.user_invitations TO platform_api;
UPDATE platform.system_metadata SET schema_version = 13 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 12 WHERE singleton;
REVOKE SELECT, INSERT, UPDATE ON platform.user_invitations FROM platform_api;
DROP TRIGGER IF EXISTS user_invitations_set_updated_at ON platform.user_invitations;
DROP TABLE IF EXISTS platform.user_invitations;
DELETE FROM platform.security_rate_limits WHERE action IN ('invitation_code', 'invitation_accept');
ALTER TABLE platform.security_rate_limits
    DROP CONSTRAINT security_rate_limits_action_check;
ALTER TABLE platform.security_rate_limits
    ADD CONSTRAINT security_rate_limits_action_check CHECK (action IN ('signup_code', 'signup', 'login', 'password_code', 'password_reset', 'account_close', 'feedback'));
