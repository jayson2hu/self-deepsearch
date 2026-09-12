-- +goose Up
GRANT SELECT, UPDATE ON platform.view_history TO platform_worker;

UPDATE platform.system_metadata SET schema_version = 7 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 6 WHERE singleton;

REVOKE SELECT, UPDATE ON platform.view_history FROM platform_worker;
