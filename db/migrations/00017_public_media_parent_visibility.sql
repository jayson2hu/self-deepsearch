-- +goose Up
-- Keep the column contract and existing grants; narrow visibility to the same
-- published parent views used by the public catalog. A prepared media link is
-- not itself permission to expose an unpublished parent.
CREATE OR REPLACE VIEW platform.public_entity_media WITH (security_barrier = true) AS
SELECT
    link.entity_type,
    link.entity_id,
    link.asset_id,
    link.purpose,
    link.position,
    link.is_primary,
    object.rendition,
    object.public_url,
    object.width,
    object.height,
    object.byte_size,
    object.mime_type
FROM platform.entity_media AS link
JOIN platform.media_assets AS asset
  ON asset.asset_id = link.asset_id
 AND asset.asset_status = 'published'
 AND asset.rights_status = 'allowed'
JOIN platform.media_objects AS object
  ON object.asset_id = asset.asset_id
 AND object.version = asset.current_version
 AND object.object_status = 'published'
 AND object.public_url IS NOT NULL
WHERE link.publication_status = 'published'
  AND (
    (link.entity_type = 'work' AND EXISTS (
      SELECT 1 FROM platform.public_published_works AS parent
      WHERE parent.work_id = link.entity_id
    ))
    OR (link.entity_type = 'performer' AND EXISTS (
      SELECT 1 FROM platform.public_published_performers AS parent
      WHERE parent.performer_id = link.entity_id
    ))
  );

UPDATE platform.system_metadata SET schema_version = 17 WHERE singleton;

-- +goose Down
-- Rollback restores the historical v16 visibility; never use it with a public
-- entry point still enabled. This section exists for disposable roundtrips.
CREATE OR REPLACE VIEW platform.public_entity_media AS
SELECT
    link.entity_type,
    link.entity_id,
    link.asset_id,
    link.purpose,
    link.position,
    link.is_primary,
    object.rendition,
    object.public_url,
    object.width,
    object.height,
    object.byte_size,
    object.mime_type
FROM platform.entity_media AS link
JOIN platform.media_assets AS asset
  ON asset.asset_id = link.asset_id
 AND asset.asset_status = 'published'
 AND asset.rights_status = 'allowed'
JOIN platform.media_objects AS object
  ON object.asset_id = asset.asset_id
 AND object.version = asset.current_version
 AND object.object_status = 'published'
 AND object.public_url IS NOT NULL
WHERE link.publication_status = 'published';

ALTER VIEW platform.public_entity_media RESET (security_barrier);
UPDATE platform.system_metadata SET schema_version = 16 WHERE singleton;
