-- +goose Up
ALTER TABLE platform.media_objects
    ADD COLUMN storage_scope text;

UPDATE platform.media_objects
SET storage_scope = CASE WHEN rendition IN ('quarantine', 'master') THEN 'private' ELSE 'public' END;

ALTER TABLE platform.media_objects
    ALTER COLUMN storage_scope SET NOT NULL,
    ADD CONSTRAINT media_objects_storage_scope_check CHECK (storage_scope IN ('private', 'public')),
    ADD CONSTRAINT media_objects_rendition_scope_check CHECK (
        (rendition IN ('quarantine', 'master') AND storage_scope = 'private') OR
        (rendition IN ('w320', 'w640', 'w960') AND storage_scope = 'public')
    ),
    ADD CONSTRAINT media_objects_scope_key_check CHECK (
        (storage_scope = 'private' AND storage_key LIKE 'media-master/%') OR
        (storage_scope = 'public' AND storage_key LIKE 'media-public/%')
    );

ALTER TABLE platform.media_objects
    DROP CONSTRAINT media_objects_storage_provider_storage_key_key,
    ADD CONSTRAINT media_objects_storage_location_uq UNIQUE (storage_provider, storage_scope, storage_key);

UPDATE platform.system_metadata SET schema_version = 9 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 8 WHERE singleton;

ALTER TABLE platform.media_objects
    DROP CONSTRAINT media_objects_storage_location_uq,
    ADD CONSTRAINT media_objects_storage_provider_storage_key_key UNIQUE (storage_provider, storage_key),
    DROP CONSTRAINT media_objects_rendition_scope_check,
    DROP CONSTRAINT media_objects_scope_key_check,
    DROP CONSTRAINT media_objects_storage_scope_check,
    DROP COLUMN storage_scope;
