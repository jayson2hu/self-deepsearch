-- +goose Up
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
    performer.measurements
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer'
 AND publication.entity_id = performer.performer_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE performer.publication_status = 'published'
  AND performer.adult_status = 'verified';

UPDATE platform.system_metadata SET schema_version = 5 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 4 WHERE singleton;

DROP VIEW platform.public_published_performers;

CREATE VIEW platform.public_published_performers AS
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
    publication.updated_at
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer'
 AND publication.entity_id = performer.performer_id
 AND publication.site = 'main'
 AND publication.publication_status = 'published'
WHERE performer.publication_status = 'published'
  AND performer.adult_status = 'verified';

GRANT SELECT ON platform.public_published_performers TO public_reader;
