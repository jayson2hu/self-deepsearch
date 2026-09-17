-- +goose Up
ALTER TABLE platform.users
  ADD COLUMN registration_origin text NOT NULL DEFAULT 'unknown'
    CHECK (registration_origin IN ('public_signup', 'invitation', 'bootstrap', 'fixture', 'unknown')),
  ADD COLUMN is_test boolean NOT NULL DEFAULT false,
  ADD COLUMN registered_at timestamptz,
  ADD CONSTRAINT users_registration_fact_check CHECK (
    (registration_origin IN ('public_signup', 'invitation') AND registered_at IS NOT NULL AND email_verified_at IS NOT NULL)
    OR (registration_origin IN ('bootstrap', 'fixture', 'unknown'))
  );

CREATE INDEX users_operations_page_idx ON platform.users (created_at DESC, user_id ASC);
CREATE INDEX users_registration_summary_idx
  ON platform.users (registered_at)
  WHERE registration_origin = 'public_signup' AND is_test = false AND registered_at IS NOT NULL;

GRANT SELECT, INSERT, UPDATE ON platform.users TO platform_api;

UPDATE platform.system_metadata SET schema_version = 23 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 22 WHERE singleton;

DROP INDEX IF EXISTS platform.users_registration_summary_idx;
DROP INDEX IF EXISTS platform.users_operations_page_idx;
ALTER TABLE platform.users
  DROP CONSTRAINT users_registration_fact_check,
  DROP COLUMN registered_at,
  DROP COLUMN is_test,
  DROP COLUMN registration_origin;
