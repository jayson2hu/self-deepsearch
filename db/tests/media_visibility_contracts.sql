\set ON_ERROR_STOP on

-- Disposable, migrated fixture database only; no real images or network I/O.
-- All synthetic records and state changes are rolled back at the end.
BEGIN;

CREATE TEMP TABLE media_visibility_cases ON COMMIT DROP AS
SELECT gen_random_uuid() AS entity_id, gen_random_uuid() AS asset_id,
       gen_random_uuid() AS revision_id, entity_type,
       scenario, parent_status, publication_status, site
FROM (VALUES ('work'), ('performer')) AS entity(entity_type)
CROSS JOIN (VALUES
    ('published-main', 'published', 'published', 'main'),
    ('draft', 'draft', 'published', 'main'),
    ('reviewing', 'reviewing', 'published', 'main'),
    ('approved', 'approved', 'published', 'main'),
    ('hidden', 'hidden', 'published', 'main'),
    ('takedown', 'takedown', 'published', 'main'),
    ('merged', 'merged', 'published', 'main'),
    ('missing-publication', 'published', NULL, 'main'),
    ('hidden-publication', 'published', 'hidden', 'main'),
    ('takedown-publication', 'published', 'takedown', 'main'),
    ('secondary-site', 'published', 'published', 'secondary')
) AS boundary(scenario, parent_status, publication_status, site);

INSERT INTO platform.works (work_id, canonical_code, compact_code, title, publication_status)
SELECT entity_id, 'VIS-' || entity_id::text, 'VIS' || upper(replace(entity_id::text, '-', '')),
       'Synthetic visibility contract', parent_status
FROM media_visibility_cases WHERE entity_type = 'work';

INSERT INTO platform.performers (performer_id, display_name, canonical_slug, adult_status, publication_status)
SELECT entity_id, 'Synthetic visibility contract', 'vis-' || entity_id::text, 'verified', parent_status
FROM media_visibility_cases WHERE entity_type = 'performer';

INSERT INTO platform.content_revisions (
    revision_id, entity_type, entity_id, version, payload, revision_status,
    author_id, reviewer_id, reason, submitted_at, reviewed_at
)
SELECT revision_id, entity_type, entity_id, 1, '{}'::jsonb, 'approved',
       '90000000-0000-4000-8000-000000000001'::uuid,
       '90000000-0000-4000-8000-000000000002'::uuid,
       'Synthetic visibility contract', now(), now()
FROM media_visibility_cases;

INSERT INTO platform.publications (
    entity_type, entity_id, site, current_revision_id, publication_status,
    canonical_slug, published_at, hidden_at, takedown_at
)
SELECT entity_type, entity_id, site, revision_id, publication_status, 'vis-' || entity_id::text,
       CASE WHEN publication_status = 'published' THEN now() END,
       CASE WHEN publication_status = 'hidden' THEN now() END,
       CASE WHEN publication_status = 'takedown' THEN now() END
FROM media_visibility_cases WHERE publication_status IS NOT NULL;

INSERT INTO platform.media_assets (
    asset_id, asset_type, source_type, checked_at, confidence, rights_status, asset_status
)
SELECT asset_id, CASE WHEN entity_type = 'work' THEN 'work_image' ELSE 'performer_avatar' END,
       'fixture', now(), 1, 'allowed', 'published'
FROM media_visibility_cases;

INSERT INTO platform.media_objects (
    asset_id, version, rendition, storage_provider, storage_scope, storage_key, backup_path,
    public_url, sha256, mime_type, width, height, byte_size, object_status
)
SELECT asset_id, 1, 'w320', 's3', 'public',
       'media-public/visibility-contract/' || asset_id::text || '.webp',
       'media-public/visibility-contract/' || asset_id::text || '.webp',
       'https://media.example.test/visibility-contract/' || scenario || '/' || asset_id::text || '.webp',
       repeat('a', 64), 'image/webp', 320, 200, 512, 'published'
FROM media_visibility_cases;

INSERT INTO platform.entity_media (entity_type, entity_id, asset_id, purpose, is_primary, publication_status)
SELECT entity_type, entity_id, asset_id,
       CASE WHEN entity_type = 'work' THEN 'cover' ELSE 'avatar' END, true, 'published'
FROM media_visibility_cases;

SET LOCAL ROLE public_reader;
DO $$
DECLARE
    total integer;
    leaked integer;
BEGIN
    IF current_user <> 'public_reader' THEN
        RAISE EXCEPTION 'media visibility contract must read as public_reader';
    END IF;
    SELECT count(*), count(*) FILTER (WHERE public_url NOT LIKE '%/published-main/%')
    INTO total, leaked FROM platform.public_entity_media
    WHERE public_url LIKE 'https://media.example.test/visibility-contract/%';
    IF total <> 2 OR leaked <> 0 THEN
        RAISE EXCEPTION 'parent visibility failed: expected 2 published rows, got %, leaked %', total, leaked;
    END IF;
    IF has_table_privilege(current_user, 'platform.media_objects', 'SELECT')
       OR has_table_privilege(current_user, 'platform.entity_media', 'SELECT') THEN
        RAISE EXCEPTION 'public reader can bypass the media view';
    END IF;
END
$$;
RESET ROLE;

-- Hide the publication only: stale parent/link/asset/object flags must not
-- preserve public visibility. Keep these flags untouched to test the gate.
UPDATE platform.publications SET publication_status = 'hidden', hidden_at = now(), published_at = NULL
WHERE entity_id IN (SELECT entity_id FROM media_visibility_cases WHERE scenario = 'published-main');

SET LOCAL ROLE public_reader;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM platform.public_entity_media
               WHERE public_url LIKE 'https://media.example.test/visibility-contract/%') THEN
        RAISE EXCEPTION 'hidden publication retained public media';
    END IF;
END
$$;
RESET ROLE;

UPDATE platform.publications SET publication_status = 'published', hidden_at = NULL, published_at = now()
WHERE entity_id IN (SELECT entity_id FROM media_visibility_cases WHERE scenario = 'published-main');

SET LOCAL ROLE public_reader;
DO $$
BEGIN
    IF (SELECT count(*) FROM platform.public_entity_media
        WHERE public_url LIKE 'https://media.example.test/visibility-contract/%') <> 2 THEN
        RAISE EXCEPTION 'republishing did not restore prepared public media';
    END IF;
END
$$;
RESET ROLE;

ROLLBACK;
\echo 'PostgreSQL media parent visibility contracts passed'
