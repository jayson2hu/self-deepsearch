-- +goose Up
CREATE TABLE platform.search_metrics_hourly (
    hour_bucket timestamptz PRIMARY KEY,
    search_requests bigint NOT NULL DEFAULT 0 CHECK (search_requests >= 0),
    zero_result_requests bigint NOT NULL DEFAULT 0 CHECK (zero_result_requests >= 0),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (hour_bucket = (date_trunc('hour', hour_bucket AT TIME ZONE 'UTC') AT TIME ZONE 'UTC')),
    CHECK (zero_result_requests <= search_requests)
);

CREATE INDEX search_metrics_hourly_recent_idx
    ON platform.search_metrics_hourly (hour_bucket DESC);

GRANT SELECT, INSERT, UPDATE ON platform.search_metrics_hourly TO platform_api;

UPDATE platform.system_metadata SET schema_version = 16 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 15 WHERE singleton;

REVOKE SELECT, INSERT, UPDATE ON platform.search_metrics_hourly FROM platform_api;
DROP TABLE IF EXISTS platform.search_metrics_hourly;
