-- +goose Up
ALTER TABLE platform.field_provenance
    ADD COLUMN revision_id uuid;

UPDATE platform.field_provenance AS provenance
SET revision_id = (
    SELECT candidate.revision_id
    FROM platform.content_revisions AS candidate
    WHERE candidate.entity_type = provenance.entity_type
      AND candidate.entity_id = provenance.entity_id
    ORDER BY candidate.version DESC
    LIMIT 1
)
WHERE provenance.entity_type IN ('work', 'performer', 'studio');

UPDATE platform.field_provenance AS provenance
SET operator_id = revision.author_id
FROM platform.content_revisions AS revision
WHERE provenance.revision_id = revision.revision_id
  AND provenance.operator_id IS NULL;

ALTER TABLE platform.field_provenance
    ADD CONSTRAINT field_provenance_revision_fk
        FOREIGN KEY (revision_id) REFERENCES platform.content_revisions(revision_id) ON DELETE RESTRICT,
    ADD CONSTRAINT field_provenance_revision_scope_check
        CHECK (
            (entity_type = 'media' AND revision_id IS NULL)
            OR
            (entity_type IN ('work', 'performer', 'studio') AND revision_id IS NOT NULL AND operator_id IS NOT NULL)
        );

INSERT INTO platform.field_provenance (
    entity_type, entity_id, revision_id, field_name, source_type, source_title,
    checked_at, operator_id, confidence, rights_status
)
SELECT
    revision.entity_type,
    revision.entity_id,
    revision.revision_id,
    field.field_name,
    'legacy_record',
    'Release A provenance migration v14',
    revision.created_at,
    revision.author_id,
    0.500,
    'needs_review'
FROM platform.content_revisions AS revision
CROSS JOIN LATERAL jsonb_object_keys(revision.payload) AS field(field_name)
WHERE NOT EXISTS (
    SELECT 1
    FROM platform.field_provenance AS existing
    WHERE existing.revision_id = revision.revision_id
      AND existing.field_name = field.field_name
);

CREATE INDEX field_provenance_revision_idx
    ON platform.field_provenance (revision_id, field_name, checked_at DESC)
    WHERE revision_id IS NOT NULL;

CREATE OR REPLACE FUNCTION platform.validate_field_provenance_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.entity_type = 'media' THEN
        IF NEW.revision_id IS NOT NULL THEN
            RAISE EXCEPTION 'media provenance cannot reference a content revision';
        END IF;
        RETURN NEW;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM platform.content_revisions AS revision
        WHERE revision.revision_id = NEW.revision_id
          AND revision.entity_type = NEW.entity_type
          AND revision.entity_id = NEW.entity_id
    ) THEN
        RAISE EXCEPTION 'provenance revision does not match entity';
    END IF;
    RETURN NEW;
END
$$;

CREATE TRIGGER field_provenance_validate_revision
BEFORE INSERT OR UPDATE ON platform.field_provenance
FOR EACH ROW EXECUTE FUNCTION platform.validate_field_provenance_revision();

CREATE OR REPLACE FUNCTION platform.protect_field_provenance()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'field provenance is append-only';
END
$$;

CREATE TRIGGER field_provenance_append_only
BEFORE UPDATE OR DELETE ON platform.field_provenance
FOR EACH ROW EXECUTE FUNCTION platform.protect_field_provenance();

UPDATE platform.system_metadata SET schema_version = 14 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 13 WHERE singleton;

DROP TRIGGER IF EXISTS field_provenance_append_only ON platform.field_provenance;
DROP FUNCTION IF EXISTS platform.protect_field_provenance();
DROP TRIGGER IF EXISTS field_provenance_validate_revision ON platform.field_provenance;
DROP FUNCTION IF EXISTS platform.validate_field_provenance_revision();
DROP INDEX IF EXISTS platform.field_provenance_revision_idx;

DELETE FROM platform.field_provenance
WHERE source_type = 'legacy_record'
  AND source_title = 'Release A provenance migration v14';

ALTER TABLE platform.field_provenance
    DROP CONSTRAINT IF EXISTS field_provenance_revision_scope_check,
    DROP CONSTRAINT IF EXISTS field_provenance_revision_fk,
    DROP COLUMN IF EXISTS revision_id;
