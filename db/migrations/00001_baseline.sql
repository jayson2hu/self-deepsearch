-- +goose Up
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;

CREATE SCHEMA IF NOT EXISTS collector;
CREATE SCHEMA IF NOT EXISTS platform;
CREATE SCHEMA IF NOT EXISTS audit;

CREATE TABLE IF NOT EXISTS platform.system_metadata (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    schema_version integer NOT NULL CHECK (schema_version > 0),
    installed_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO platform.system_metadata (singleton, schema_version)
VALUES (true, 1)
ON CONFLICT (singleton) DO UPDATE SET schema_version = EXCLUDED.schema_version;

-- +goose Down
DROP TABLE IF EXISTS platform.system_metadata;
DROP SCHEMA IF EXISTS audit;
DROP SCHEMA IF EXISTS platform;
DROP SCHEMA IF EXISTS collector;
