-- +goose Up
ALTER TABLE platform.publications
  ADD COLUMN content_classification text NOT NULL DEFAULT 'unknown'
    CHECK (content_classification IN ('unknown', 'non_explicit', 'explicit')),
  ADD COLUMN seo_quality_status text NOT NULL DEFAULT 'unknown'
    CHECK (seo_quality_status IN ('unknown', 'eligible', 'ineligible')),
  ADD COLUMN seo_reviewed_at timestamptz,
  ADD COLUMN seo_reviewed_by uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
  ADD CONSTRAINT publications_seo_review_check CHECK (
    (content_classification = 'unknown' AND seo_quality_status = 'unknown' AND seo_reviewed_at IS NULL AND seo_reviewed_by IS NULL)
    OR (content_classification <> 'unknown' AND seo_quality_status <> 'unknown' AND seo_reviewed_at IS NOT NULL AND seo_reviewed_by IS NOT NULL)
  );

CREATE INDEX publications_seo_sitemap_idx
  ON platform.publications (entity_type, canonical_slug)
  WHERE publication_status = 'published'
    AND seo_quality_status = 'eligible'
    AND content_classification <> 'unknown';

ALTER TABLE platform.media_assets
  ADD COLUMN content_classification text NOT NULL DEFAULT 'unknown'
    CHECK (content_classification IN ('unknown', 'non_explicit', 'explicit')),
  ADD COLUMN external_preview_permission text NOT NULL DEFAULT 'unknown'
    CHECK (external_preview_permission IN ('unknown', 'allow', 'deny')),
  ADD COLUMN preview_reviewed_at timestamptz,
  ADD COLUMN preview_reviewed_by uuid REFERENCES platform.users(user_id) ON DELETE RESTRICT,
  ADD CONSTRAINT media_assets_preview_review_check CHECK (
    (content_classification = 'unknown' AND external_preview_permission = 'unknown' AND preview_reviewed_at IS NULL AND preview_reviewed_by IS NULL)
    OR (content_classification <> 'unknown' AND external_preview_permission <> 'unknown' AND preview_reviewed_at IS NOT NULL AND preview_reviewed_by IS NOT NULL)
  ),
  ADD CONSTRAINT media_assets_preview_allow_check CHECK (
    external_preview_permission <> 'allow' OR content_classification = 'non_explicit'
  );

CREATE OR REPLACE VIEW platform.public_published_works AS
SELECT
    work.work_id,
    work.canonical_code,
    work.compact_code,
    work.title,
    work.title_original,
    work.release_date,
    work.studio_id,
    work.edition_of,
    work.summary,
    publication.publication_id,
    publication.current_revision_id,
    publication.canonical_slug,
    publication.published_at,
    publication.updated_at,
    publication.content_classification,
    publication.seo_quality_status
FROM platform.works AS work
JOIN platform.publications AS publication
  ON publication.entity_type = 'work'
 AND publication.entity_id = work.work_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE work.publication_status = 'published';

CREATE OR REPLACE VIEW platform.public_published_performers AS
SELECT
    performer.performer_id,
    performer.display_name,
    performer.name_original,
    performer.romanized_name,
    performer.canonical_slug,
    performer.activity_status,
    performer.agency,
    performer.debut_year,
    publication.publication_id,
    publication.current_revision_id,
    publication.published_at,
    publication.updated_at,
    performer.birth_year,
    performer.height_cm,
    performer.measurements,
    publication.content_classification,
    publication.seo_quality_status
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer'
 AND publication.entity_id = performer.performer_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE performer.publication_status = 'published'
  AND performer.adult_status = 'verified';

CREATE OR REPLACE VIEW platform.public_published_studios AS
SELECT
    studio.studio_id,
    studio.name,
    studio.normalized_name,
    studio.canonical_slug,
    publication.publication_id,
    publication.current_revision_id,
    publication.published_at,
    publication.updated_at,
    publication.content_classification,
    publication.seo_quality_status
FROM platform.studios AS studio
JOIN platform.publications AS publication
  ON publication.entity_type = 'studio'
 AND publication.entity_id = studio.studio_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE studio.publication_status = 'published';

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
    object.mime_type,
    asset.content_classification = 'non_explicit'
      AND asset.external_preview_permission = 'allow' AS external_preview_allowed
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

GRANT SELECT ON platform.public_published_works, platform.public_published_performers,
  platform.public_published_studios, platform.public_entity_media TO public_reader;

UPDATE platform.system_metadata SET schema_version = 22 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 21 WHERE singleton;

DROP VIEW platform.public_entity_media;
DROP VIEW platform.public_published_performers;
DROP VIEW platform.public_published_studios;
DROP VIEW platform.public_published_works;

CREATE VIEW platform.public_published_works AS
SELECT
    work.work_id, work.canonical_code, work.compact_code, work.title, work.title_original,
    work.release_date, work.studio_id, work.edition_of, work.summary,
    publication.publication_id, publication.current_revision_id, publication.canonical_slug,
    publication.published_at, publication.updated_at
FROM platform.works AS work
JOIN platform.publications AS publication
  ON publication.entity_type = 'work' AND publication.entity_id = work.work_id
 AND publication.site = 'main' AND publication.publication_status = 'published'
WHERE work.publication_status = 'published';

CREATE VIEW platform.public_published_performers AS
SELECT
    performer.performer_id, performer.display_name, performer.name_original,
    performer.romanized_name, performer.canonical_slug, performer.activity_status,
    performer.agency, performer.debut_year, publication.publication_id,
    publication.current_revision_id, publication.published_at, publication.updated_at,
    performer.birth_year, performer.height_cm, performer.measurements
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer' AND publication.entity_id = performer.performer_id
 AND publication.site = 'main' AND publication.publication_status = 'published'
WHERE performer.publication_status = 'published' AND performer.adult_status = 'verified';

CREATE VIEW platform.public_published_studios AS
SELECT
    studio.studio_id, studio.name, studio.normalized_name, studio.canonical_slug,
    publication.publication_id, publication.current_revision_id,
    publication.published_at, publication.updated_at
FROM platform.studios AS studio
JOIN platform.publications AS publication
  ON publication.entity_type = 'studio' AND publication.entity_id = studio.studio_id
 AND publication.site = 'main' AND publication.publication_status = 'published'
WHERE studio.publication_status = 'published';

CREATE VIEW platform.public_entity_media WITH (security_barrier = true) AS
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

GRANT SELECT ON platform.public_published_works, platform.public_published_performers,
  platform.public_published_studios, platform.public_entity_media TO public_reader;

ALTER TABLE platform.media_assets
  DROP CONSTRAINT media_assets_preview_allow_check,
  DROP CONSTRAINT media_assets_preview_review_check,
  DROP COLUMN preview_reviewed_by,
  DROP COLUMN preview_reviewed_at,
  DROP COLUMN external_preview_permission,
  DROP COLUMN content_classification;

DROP INDEX platform.publications_seo_sitemap_idx;
ALTER TABLE platform.publications
  DROP CONSTRAINT publications_seo_review_check,
  DROP COLUMN seo_reviewed_by,
  DROP COLUMN seo_reviewed_at,
  DROP COLUMN seo_quality_status,
  DROP COLUMN content_classification;
