\if :{?release_a_fixture}
SELECT :'release_a_fixture' = '1' AS release_a_fixture_confirmed \gset
\else
\set release_a_fixture_confirmed false
\endif
\if :release_a_fixture_confirmed
\else
-- psql 16 ignores arguments to \quit and exits successfully. Raise a real
-- error so automated callers cannot report an unconfirmed seed as accepted.
\set ON_ERROR_STOP on
DO $fixture_guard$
BEGIN
    RAISE EXCEPTION 'Refusing to load Release A synthetic fixtures without --set release_a_fixture=1';
END
$fixture_guard$;
\endif

-- Every supported caller uses psql --single-transaction. These locks make the
-- empty/fixture-only check and the fixture writes one atomic catalog operation;
-- a live writer causes a quick failure instead of racing the safety check.
SET LOCAL lock_timeout = '5s';
LOCK TABLE
    collector.sources,
    collector.publication_batches,
    collector.source_records,
    collector.review_tasks,
    collector.conflict_reviews,
    platform.users,
    platform.studios,
    platform.performers,
    platform.performer_aliases,
    platform.works,
    platform.work_performers,
    platform.content_revisions,
    platform.publications,
    platform.search_documents,
    platform.field_provenance
IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    current_schema_version integer;
BEGIN
    SELECT schema_version INTO current_schema_version
    FROM platform.system_metadata
    WHERE singleton;
    IF current_schema_version IS DISTINCT FROM 21 THEN
        RAISE EXCEPTION 'Release A fixtures require schema version 21, got %', current_schema_version;
    END IF;

    IF EXISTS (
        SELECT 1 FROM platform.users
        WHERE user_id NOT IN (
            '90000000-0000-4000-8000-000000000001'::uuid,
            '90000000-0000-4000-8000-000000000002'::uuid
        )
    ) OR EXISTS (
        SELECT 1 FROM platform.users
        WHERE user_id IN (
            '90000000-0000-4000-8000-000000000001'::uuid,
            '90000000-0000-4000-8000-000000000002'::uuid
        ) AND (
            account_status <> 'closed'
            OR normalized_email NOT IN ('owner@example.invalid', 'reviewer@example.invalid')
        )
    ) OR EXISTS (
        SELECT 1 FROM platform.works
        WHERE work_id::text !~ '^10000000-0000-4000-8000-000000000(0[0-9][1-9]|0[1-9][0-9]|100)$'
    ) OR EXISTS (
        SELECT 1 FROM platform.performers
        WHERE performer_id::text !~ '^60000000-0000-4000-8000-0000000000(0[1-9]|[12][0-9]|30)$'
    ) OR EXISTS (
        SELECT 1 FROM platform.studios
        WHERE studio_id <> '70000000-0000-4000-8000-000000000001'::uuid
    ) OR EXISTS (
        SELECT 1 FROM collector.sources
        WHERE source_id NOT IN (
            '80000000-0000-4000-8000-000000000001'::uuid,
            '81000000-0000-4000-8000-000000000001'::uuid
        ) OR (
            source_id = '81000000-0000-4000-8000-000000000001'::uuid
            AND (name <> 'Manual CSV import' OR source_type <> 'manual' OR base_url IS NOT NULL)
        )
    ) OR EXISTS (
        -- Migration 6 creates the built-in manual source even in an empty
        -- database. Its presence is safe only before real ingestion begins.
        SELECT 1 FROM collector.publication_batches
        WHERE source_id <> '80000000-0000-4000-8000-000000000001'::uuid
    ) OR EXISTS (
        SELECT 1 FROM collector.source_records
        WHERE source_id <> '80000000-0000-4000-8000-000000000001'::uuid
    ) THEN
        RAISE EXCEPTION 'Release A synthetic fixtures require an empty or fixture-only catalog database';
    END IF;
END
$$;

INSERT INTO collector.sources (
    source_id, name, source_type, priority, status, rights_status, checked_at
) VALUES (
    '80000000-0000-4000-8000-000000000001', 'Release A fixture', 'fixture', 1, 'active', 'allowed', now()
) ON CONFLICT (source_id) DO NOTHING;

INSERT INTO platform.users (
    user_id, normalized_email, email_verified_at, password_hash, role, account_status, closed_at
) VALUES
    ('90000000-0000-4000-8000-000000000001', 'owner@example.invalid', now(), '$fixture$not-a-login-password-hash', 'owner', 'closed', now()),
    ('90000000-0000-4000-8000-000000000002', 'reviewer@example.invalid', now(), '$fixture$not-a-login-password-hash', 'admin', 'closed', now())
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO platform.studios (
    studio_id, name, normalized_name, canonical_slug, publication_status
) VALUES (
    '70000000-0000-4000-8000-000000000001', '测试厂牌', '测试厂牌', 'fixture-studio', 'published'
) ON CONFLICT (studio_id) DO NOTHING;

INSERT INTO platform.performers (
    performer_id, display_name, canonical_slug, adult_status, activity_status, publication_status
) VALUES (
    '60000000-0000-4000-8000-000000000001', '测试人物', 'fixture-performer', 'verified', 'active', 'published'
) ON CONFLICT (performer_id) DO NOTHING;

INSERT INTO platform.performer_aliases (
    performer_alias_id, performer_id, alias_type, raw_value, normalized_value
) VALUES (
    '61000000-0000-4000-8000-000000000001',
    '60000000-0000-4000-8000-000000000001',
    'stage_name', '测试别名', '测试别名'
) ON CONFLICT (performer_alias_id) DO NOTHING;

INSERT INTO platform.works (
    work_id, canonical_code, compact_code, title, release_date, studio_id, publication_status
) VALUES
    ('10000000-0000-4000-8000-000000000001', 'TEST-001', 'TEST001', '城市记录', '2026-08-01', '70000000-0000-4000-8000-000000000001', 'published'),
    ('10000000-0000-4000-8000-000000000002', 'TEST-002', 'TEST002', '海岸来信', '2026-07-28', '70000000-0000-4000-8000-000000000001', 'published'),
    ('10000000-0000-4000-8000-000000000003', 'TEST-003', 'TEST003', '舞台之后', '2026-07-24', '70000000-0000-4000-8000-000000000001', 'published'),
    ('10000000-0000-4000-8000-000000000004', 'TEST-004', 'TEST004', '花园午后', '2026-07-18', '70000000-0000-4000-8000-000000000001', 'published'),
    ('10000000-0000-4000-8000-000000000005', 'TEST-005', 'TEST005', '待审核城市来信', '2026-08-08', '70000000-0000-4000-8000-000000000001', 'reviewing')
ON CONFLICT (work_id) DO NOTHING;

INSERT INTO platform.work_performers (work_id, performer_id, position)
SELECT work_id, '60000000-0000-4000-8000-000000000001', 0
FROM platform.works
WHERE work_id IN (
    '10000000-0000-4000-8000-000000000001',
    '10000000-0000-4000-8000-000000000002',
    '10000000-0000-4000-8000-000000000003',
    '10000000-0000-4000-8000-000000000004'
)
ON CONFLICT DO NOTHING;

INSERT INTO platform.content_revisions (
    revision_id, entity_type, entity_id, version, payload, revision_status,
    author_id, reviewer_id, reason, submitted_at, reviewed_at
) VALUES
    ('21000000-0000-4000-8000-000000000001', 'studio', '70000000-0000-4000-8000-000000000001', 1, '{"name":"测试厂牌"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('22000000-0000-4000-8000-000000000001', 'performer', '60000000-0000-4000-8000-000000000001', 1, '{"display_name":"测试人物","adult_status":"verified"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('20000000-0000-4000-8000-000000000001', 'work', '10000000-0000-4000-8000-000000000001', 1, '{"canonical_code":"TEST-001","title":"城市记录"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('20000000-0000-4000-8000-000000000002', 'work', '10000000-0000-4000-8000-000000000002', 1, '{"canonical_code":"TEST-002","title":"海岸来信"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('20000000-0000-4000-8000-000000000003', 'work', '10000000-0000-4000-8000-000000000003', 1, '{"canonical_code":"TEST-003","title":"舞台之后"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('20000000-0000-4000-8000-000000000004', 'work', '10000000-0000-4000-8000-000000000004', 1, '{"canonical_code":"TEST-004","title":"花园午后"}', 'approved', '90000000-0000-4000-8000-000000000001', '90000000-0000-4000-8000-000000000002', 'Release A fixture', now(), now()),
    ('20000000-0000-4000-8000-000000000005', 'work', '10000000-0000-4000-8000-000000000005', 1, '{"canonical_code":"TEST-005","compact_code":"TEST005","title":"待审核城市来信","release_date":"2026-08-08","studio_id":"70000000-0000-4000-8000-000000000001"}', 'reviewing', '90000000-0000-4000-8000-000000000002', NULL, 'Release A review fixture', now(), NULL)
ON CONFLICT (revision_id) DO NOTHING;

INSERT INTO collector.review_tasks (
    review_task_id, task_type, entity_type, entity_id, priority, status
) VALUES
  (
    '40000000-0000-4000-8000-000000000001', 'publication', 'work',
    '10000000-0000-4000-8000-000000000005', 50, 'pending'
  ),
  (
    '40000000-0000-4000-8000-000000000002', 'conflict', 'work',
    '10000000-0000-4000-8000-000000000001', 80, 'pending'
  )
ON CONFLICT (review_task_id) DO NOTHING;

INSERT INTO collector.conflict_reviews (
    conflict_review_id, review_task_id, entity_type, entity_id, field_name,
    current_value, candidate_value, source_id, source_priority
) VALUES (
    '41000000-0000-4000-8000-000000000001',
    '40000000-0000-4000-8000-000000000002',
    'work', '10000000-0000-4000-8000-000000000001', 'release_date',
    '"2026-08-01"'::jsonb, '"2026-08-02"'::jsonb,
    '80000000-0000-4000-8000-000000000001', 1
) ON CONFLICT (conflict_review_id) DO NOTHING;

INSERT INTO platform.publications (
    publication_id, entity_type, entity_id, current_revision_id,
    publication_status, canonical_slug, published_at
) VALUES
    ('31000000-0000-4000-8000-000000000001', 'studio', '70000000-0000-4000-8000-000000000001', '21000000-0000-4000-8000-000000000001', 'published', 'fixture-studio', now()),
    ('32000000-0000-4000-8000-000000000001', 'performer', '60000000-0000-4000-8000-000000000001', '22000000-0000-4000-8000-000000000001', 'published', 'fixture-performer', now()),
    ('30000000-0000-4000-8000-000000000001', 'work', '10000000-0000-4000-8000-000000000001', '20000000-0000-4000-8000-000000000001', 'published', 'test-001', now()),
    ('30000000-0000-4000-8000-000000000002', 'work', '10000000-0000-4000-8000-000000000002', '20000000-0000-4000-8000-000000000002', 'published', 'test-002', now()),
    ('30000000-0000-4000-8000-000000000003', 'work', '10000000-0000-4000-8000-000000000003', '20000000-0000-4000-8000-000000000003', 'published', 'test-003', now()),
    ('30000000-0000-4000-8000-000000000004', 'work', '10000000-0000-4000-8000-000000000004', '20000000-0000-4000-8000-000000000004', 'published', 'test-004', now())
ON CONFLICT (publication_id) DO NOTHING;

INSERT INTO platform.search_documents (
    search_document_id, entity_type, entity_id, canonical_code, compact_code,
    title, names, aliases, studio_name, publication_id
) SELECT
    gen_random_uuid(), 'work', work.work_id, work.canonical_code, work.compact_code,
    work.title, '测试人物', '测试别名', '测试厂牌', publication.publication_id
FROM platform.works AS work
JOIN platform.publications AS publication
  ON publication.entity_type = 'work' AND publication.entity_id = work.work_id
WHERE work.work_id IN (
    '10000000-0000-4000-8000-000000000001',
    '10000000-0000-4000-8000-000000000002',
    '10000000-0000-4000-8000-000000000003',
    '10000000-0000-4000-8000-000000000004'
)
ON CONFLICT (entity_type, entity_id) DO UPDATE SET aliases = EXCLUDED.aliases;

-- Deterministic synthetic catalog for pagination, discovery mixing and search tests.
-- These rows are development-only fixtures and do not represent real people or works.
INSERT INTO platform.performers (
    performer_id, display_name, canonical_slug, adult_status, activity_status,
    publication_status
)
SELECT
    format('60000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('测试人物 %s', lpad(number::text, 3, '0')),
    format('fixture-performer-%s', lpad(number::text, 3, '0')),
    'verified', 'active', 'published'
FROM generate_series(2, 30) AS number
ON CONFLICT (performer_id) DO NOTHING;

INSERT INTO platform.works (
    work_id, canonical_code, compact_code, title, release_date, studio_id,
    publication_status
)
SELECT
    format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('DEMO-%s', lpad(number::text, 3, '0')),
    format('DEMO%s', lpad(number::text, 3, '0')),
    format('测试作品 %s', lpad(number::text, 3, '0')),
    DATE '2026-08-18' - (number - 6),
    '70000000-0000-4000-8000-000000000001'::uuid,
    'published'
FROM generate_series(6, 100) AS number
ON CONFLICT (work_id) DO NOTHING;

INSERT INTO platform.work_performers (work_id, performer_id, position)
SELECT
    format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('60000000-0000-4000-8000-%s', lpad((((number - 1) % 30) + 1)::text, 12, '0'))::uuid,
    0
FROM generate_series(6, 100) AS number
ON CONFLICT DO NOTHING;

INSERT INTO platform.content_revisions (
    revision_id, entity_type, entity_id, version, payload, revision_status,
    author_id, reviewer_id, reason, submitted_at, reviewed_at
)
SELECT
    format('22000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'performer',
    format('60000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    1,
    jsonb_build_object(
        'display_name', format('测试人物 %s', lpad(number::text, 3, '0')),
        'adult_status', 'verified'
    ),
    'approved',
    '90000000-0000-4000-8000-000000000001'::uuid,
    '90000000-0000-4000-8000-000000000002'::uuid,
    'Release A deterministic fixture', now(), now()
FROM generate_series(2, 30) AS number
ON CONFLICT (revision_id) DO NOTHING;

INSERT INTO platform.content_revisions (
    revision_id, entity_type, entity_id, version, payload, revision_status,
    author_id, reviewer_id, reason, submitted_at, reviewed_at
)
SELECT
    format('20000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'work',
    format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    1,
    jsonb_build_object(
        'canonical_code', format('DEMO-%s', lpad(number::text, 3, '0')),
        'compact_code', format('DEMO%s', lpad(number::text, 3, '0')),
        'title', format('测试作品 %s', lpad(number::text, 3, '0')),
        'release_date', to_char(DATE '2026-08-18' - (number - 6), 'YYYY-MM-DD'),
        'studio_id', '70000000-0000-4000-8000-000000000001'
    ),
    'approved',
    '90000000-0000-4000-8000-000000000001'::uuid,
    '90000000-0000-4000-8000-000000000002'::uuid,
    'Release A deterministic fixture', now(), now()
FROM generate_series(6, 100) AS number
ON CONFLICT (revision_id) DO NOTHING;

INSERT INTO platform.publications (
    publication_id, entity_type, entity_id, current_revision_id,
    publication_status, canonical_slug, published_at
)
SELECT
    format('32000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'performer',
    format('60000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('22000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'published',
    format('fixture-performer-%s', lpad(number::text, 3, '0')),
    timestamptz '2026-08-18 00:00:00+00' - make_interval(hours => number)
FROM generate_series(2, 30) AS number
ON CONFLICT (publication_id) DO NOTHING;

INSERT INTO platform.publications (
    publication_id, entity_type, entity_id, current_revision_id,
    publication_status, canonical_slug, published_at
)
SELECT
    format('30000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'work',
    format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('20000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'published',
    format('demo-%s', lpad(number::text, 3, '0')),
    timestamptz '2026-08-18 00:00:00+00' - make_interval(hours => number)
FROM generate_series(6, 100) AS number
ON CONFLICT (publication_id) DO NOTHING;

INSERT INTO platform.search_documents (
    search_document_id, entity_type, entity_id, canonical_code, compact_code,
    title, names, studio_name, publication_id
)
SELECT
    gen_random_uuid(), 'work', work.work_id, work.canonical_code, work.compact_code,
    work.title, performer.display_name, studio.name, publication.publication_id
FROM platform.works AS work
JOIN platform.publications AS publication
  ON publication.entity_type = 'work' AND publication.entity_id = work.work_id
JOIN platform.studios AS studio ON studio.studio_id = work.studio_id
JOIN platform.work_performers AS relation ON relation.work_id = work.work_id
JOIN platform.performers AS performer ON performer.performer_id = relation.performer_id
WHERE work.work_id IN (
    SELECT format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid
    FROM generate_series(6, 100) AS number
)
ON CONFLICT (entity_type, entity_id) DO NOTHING;

INSERT INTO platform.search_documents (
    search_document_id, entity_type, entity_id, title, names, publication_id
)
SELECT
    gen_random_uuid(), 'performer', performer.performer_id,
    performer.display_name, performer.display_name, publication.publication_id
FROM platform.performers AS performer
JOIN platform.publications AS publication
  ON publication.entity_type = 'performer' AND publication.entity_id = performer.performer_id
WHERE performer.performer_id IN (
    SELECT format('60000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid
    FROM generate_series(2, 30) AS number
)
ON CONFLICT (entity_type, entity_id) DO NOTHING;

INSERT INTO platform.field_provenance (
    provenance_id, entity_type, entity_id, revision_id, field_name, source_id, source_type,
    source_title, checked_at, operator_id, confidence, rights_status
)
SELECT
    format('51000001-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'work',
    format('10000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('20000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'canonical_code',
    '80000000-0000-4000-8000-000000000001'::uuid,
    'fixture', 'Release A deterministic fixture', now(),
    '90000000-0000-4000-8000-000000000001'::uuid, 1, 'allowed'
FROM generate_series(6, 100) AS number
ON CONFLICT (provenance_id) DO NOTHING;

INSERT INTO platform.field_provenance (
    provenance_id, entity_type, entity_id, revision_id, field_name, source_id, source_type,
    source_title, checked_at, operator_id, confidence, rights_status
)
SELECT
    format('52000001-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'performer',
    format('60000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    format('22000000-0000-4000-8000-%s', lpad(number::text, 12, '0'))::uuid,
    'display_name',
    '80000000-0000-4000-8000-000000000001'::uuid,
    'fixture', 'Release A deterministic fixture', now(),
    '90000000-0000-4000-8000-000000000001'::uuid, 1, 'allowed'
FROM generate_series(2, 30) AS number
ON CONFLICT (provenance_id) DO NOTHING;

INSERT INTO platform.field_provenance (
    provenance_id, entity_type, entity_id, revision_id, field_name, source_id, source_type,
    source_title, checked_at, operator_id, confidence, rights_status
)
SELECT
    md5(revision.revision_id::text || ':' || field.field_name)::uuid,
    revision.entity_type,
    revision.entity_id,
    revision.revision_id,
    field.field_name,
    '80000000-0000-4000-8000-000000000001'::uuid,
    'fixture',
    'Release A deterministic fixture',
    revision.created_at,
    revision.author_id,
    1,
    'allowed'
FROM platform.content_revisions AS revision
CROSS JOIN LATERAL jsonb_object_keys(revision.payload) AS field(field_name)
WHERE revision.reason = 'Release A deterministic fixture'
ON CONFLICT (provenance_id) DO NOTHING;
