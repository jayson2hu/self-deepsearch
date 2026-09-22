-- +goose Up
CREATE TABLE platform.visitor_metrics_daily (
    visit_day date NOT NULL,
    visitor_hash text NOT NULL CHECK (visitor_hash ~ '^[0-9a-f]{64}$'),
    page_views bigint NOT NULL CHECK (page_views > 0),
    PRIMARY KEY (visit_day, visitor_hash)
);
GRANT SELECT, INSERT, UPDATE, DELETE ON platform.visitor_metrics_daily TO platform_api;
UPDATE platform.system_metadata SET schema_version = 28 WHERE singleton;

-- +goose Down
DROP TABLE platform.visitor_metrics_daily;
UPDATE platform.system_metadata SET schema_version = 27 WHERE singleton;
