-- +goose Up
ALTER TABLE audit.notification_deliveries
    DROP CONSTRAINT notification_deliveries_notification_type_check;

ALTER TABLE audit.notification_deliveries
    ADD CONSTRAINT notification_deliveries_notification_type_check
        CHECK (notification_type IN (
            'signup_code',
            'password_code',
            'account_close_code',
            'invitation_code',
            'account_closed',
            'admin_alert'
        ));

CREATE TABLE platform.email_delivery_state (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    consecutive_failures integer NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    circuit_open_until timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO platform.email_delivery_state (singleton) VALUES (true);

CREATE TABLE platform.email_delivery_daily (
    delivery_date date PRIMARY KEY,
    reserved_count bigint NOT NULL DEFAULT 0 CHECK (reserved_count >= 0),
    sent_count bigint NOT NULL DEFAULT 0 CHECK (sent_count >= 0),
    failed_count bigint NOT NULL DEFAULT 0 CHECK (failed_count >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (sent_count + failed_count <= reserved_count)
);

CREATE INDEX notification_deliveries_outcome_idx
    ON audit.notification_deliveries (notification_type, delivery_status);

GRANT SELECT, UPDATE ON platform.email_delivery_state TO platform_api;
GRANT SELECT, INSERT, UPDATE ON platform.email_delivery_daily TO platform_api;

UPDATE platform.system_metadata SET schema_version = 15 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 14 WHERE singleton;

REVOKE ALL ON platform.email_delivery_daily FROM platform_api;
REVOKE ALL ON platform.email_delivery_state FROM platform_api;

DROP INDEX IF EXISTS audit.notification_deliveries_outcome_idx;
DROP TABLE IF EXISTS platform.email_delivery_daily;
DROP TABLE IF EXISTS platform.email_delivery_state;

DELETE FROM audit.notification_deliveries
WHERE notification_type = 'invitation_code';

ALTER TABLE audit.notification_deliveries
    DROP CONSTRAINT notification_deliveries_notification_type_check;

ALTER TABLE audit.notification_deliveries
    ADD CONSTRAINT notification_deliveries_notification_type_check
        CHECK (notification_type IN (
            'signup_code',
            'password_code',
            'account_close_code',
            'account_closed',
            'admin_alert'
        ));
