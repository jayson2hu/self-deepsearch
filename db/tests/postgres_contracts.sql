\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
    business_objects integer;
    schema_version integer;
    published_works integer;
    total_works integer;
    published_performers integer;
    exact_code_matches integer;
BEGIN
    SELECT metadata.schema_version INTO schema_version
    FROM platform.system_metadata AS metadata
    WHERE metadata.singleton;
    IF schema_version <> 21 THEN
        RAISE EXCEPTION 'expected schema version 21, got %', schema_version;
    END IF;

    SELECT count(*) INTO business_objects
    FROM pg_class AS object
    JOIN pg_namespace AS namespace ON namespace.oid = object.relnamespace
    WHERE namespace.nspname = 'public'
      AND object.relkind IN ('r', 'p', 'v', 'm');
    IF business_objects <> 0 THEN
        RAISE EXCEPTION 'business objects must not exist in public schema, got %', business_objects;
    END IF;

    SELECT count(*) INTO published_works FROM platform.public_published_works;
    IF published_works <> 99 THEN
        RAISE EXCEPTION 'expected 99 published fixture works, got %', published_works;
    END IF;

    SELECT count(*) INTO total_works FROM platform.works;
    IF total_works <> 100 THEN
        RAISE EXCEPTION 'expected 100 total fixture works, got %', total_works;
    END IF;

    SELECT count(*) INTO published_performers FROM platform.public_published_performers;
    IF published_performers <> 30 THEN
        RAISE EXCEPTION 'expected 30 published fixture performers, got %', published_performers;
    END IF;

    SELECT count(*) INTO exact_code_matches
    FROM platform.public_search_documents
    WHERE compact_code = 'TEST001';
    IF exact_code_matches <> 1 THEN
        RAISE EXCEPTION 'exact compact-code search must return one published work, got %', exact_code_matches;
    END IF;

    SELECT count(*) INTO exact_code_matches
    FROM platform.public_search_documents
    WHERE compact_code = 'DEMO100';
    IF exact_code_matches <> 1 THEN
        RAISE EXCEPTION 'generated fixture search must return one published work, got %', exact_code_matches;
    END IF;

    SELECT count(*) INTO exact_code_matches
    FROM platform.public_search_documents
    WHERE entity_type = 'work' AND search_text ILIKE '%测试别名%';
    IF exact_code_matches <> 4 THEN
        RAISE EXCEPTION 'performer alias search must reach four linked published works, got %', exact_code_matches;
    END IF;

    IF NOT has_schema_privilege('public_reader', 'platform', 'USAGE')
       OR NOT has_table_privilege('public_reader', 'platform.public_published_works', 'SELECT')
       OR NOT has_table_privilege('public_reader', 'platform.public_search_documents', 'SELECT') THEN
        RAISE EXCEPTION 'public_reader is missing required public catalog privileges';
    END IF;
    IF has_table_privilege('public_reader', 'platform.users', 'SELECT')
       OR has_table_privilege('public_reader', 'platform.works', 'SELECT')
       OR has_table_privilege('public_reader', 'platform.editorial_recommendations', 'SELECT')
       OR has_table_privilege('public_reader', 'platform.public_published_works', 'INSERT') THEN
        RAISE EXCEPTION 'public_reader can access non-public or mutable catalog data';
    END IF;

    IF NOT has_table_privilege('collector_ingest', 'collector.source_records', 'INSERT')
       OR NOT has_table_privilege('collector_ingest', 'collector.jobs', 'UPDATE') THEN
        RAISE EXCEPTION 'collector_ingest is missing ingestion privileges';
    END IF;

    IF NOT has_table_privilege('platform_api', 'platform.editorial_recommendations', 'SELECT')
       OR NOT has_table_privilege('platform_api', 'platform.editorial_recommendations', 'INSERT')
       OR NOT has_table_privilege('platform_api', 'platform.editorial_recommendations', 'UPDATE')
       OR has_table_privilege('platform_api', 'platform.editorial_recommendations', 'DELETE') THEN
        RAISE EXCEPTION 'platform_api editorial recommendation privileges are invalid';
    END IF;
    IF has_table_privilege('collector_ingest', 'platform.works', 'SELECT')
       OR has_table_privilege('collector_ingest', 'platform.users', 'SELECT') THEN
        RAISE EXCEPTION 'collector_ingest can access platform data';
    END IF;

    IF NOT has_table_privilege('platform_worker', 'platform.outbox_events', 'UPDATE')
       OR NOT has_table_privilege('platform_worker', 'audit.media_reconciliation_runs', 'INSERT') THEN
        RAISE EXCEPTION 'platform_worker is missing worker privileges';
    END IF;
    IF has_table_privilege('platform_worker', 'platform.users', 'SELECT')
       OR has_table_privilege('platform_worker', 'collector.source_records', 'SELECT') THEN
        RAISE EXCEPTION 'platform_worker can access identity or collector data';
    END IF;

    IF NOT has_table_privilege('audit_writer', 'audit.audit_logs', 'INSERT')
       OR has_table_privilege('audit_writer', 'audit.audit_logs', 'UPDATE')
       OR has_table_privilege('audit_writer', 'audit.audit_logs', 'DELETE') THEN
        RAISE EXCEPTION 'audit_writer append-only grants are invalid';
    END IF;
END
$$;

INSERT INTO platform.media_assets (
    asset_id, asset_type, source_id, source_type, checked_at, operator_id,
    confidence, rights_status, asset_status
) VALUES (
    'a0000000-0000-4000-8000-000000000001', 'work_image',
    '80000000-0000-4000-8000-000000000001', 'fixture', now(),
    '90000000-0000-4000-8000-000000000001', 1, 'allowed', 'ready'
);

INSERT INTO platform.media_objects (
    media_object_id, asset_id, version, rendition, storage_provider, storage_scope,
    storage_key, backup_path, public_url, sha256, mime_type, width, height,
    byte_size, object_status
) VALUES
    (
        'a1000000-0000-4000-8000-000000000001',
        'a0000000-0000-4000-8000-000000000001', 1, 'master', 's3', 'private',
        'media-master/a0/asset-v1.webp', 'media-master/a0/asset-v1.webp', NULL,
        repeat('a', 64), 'image/webp', 1200, 1800, 1024, 'ready'
    ),
    (
        'a1000000-0000-4000-8000-000000000002',
        'a0000000-0000-4000-8000-000000000001', 1, 'w320', 's3', 'public',
        'media-public/a0/asset-v1-w320.webp', 'media-public/a0/asset-v1-w320.webp',
        'https://media.example.invalid/media-public/a0/asset-v1-w320.webp',
        repeat('b', 64), 'image/webp', 320, 480, 512, 'published'
    );

DO $$
BEGIN
    BEGIN
        INSERT INTO platform.media_objects (
            asset_id, version, rendition, storage_provider, storage_scope, storage_key,
            sha256, mime_type, width, height, byte_size, object_status
        ) VALUES (
            'a0000000-0000-4000-8000-000000000001', 1, 'w640', 's3', 'private',
            'media-master/a0/invalid-public-derivative.webp', repeat('c', 64),
            'image/webp', 640, 960, 700, 'ready'
        );
        RAISE EXCEPTION 'public derivative accepted in private storage scope';
    EXCEPTION
        WHEN check_violation THEN NULL;
    END;

    BEGIN
        INSERT INTO platform.media_objects (
            asset_id, version, rendition, storage_provider, storage_scope, storage_key,
            sha256, mime_type, width, height, byte_size, object_status
        ) VALUES (
            'a0000000-0000-4000-8000-000000000001', 1, 'master', 's3', 'public',
            'media-public/a0/invalid-master.webp', repeat('d', 64),
            'image/webp', 1200, 1800, 900, 'ready'
        );
        RAISE EXCEPTION 'master accepted in public storage scope';
    EXCEPTION
        WHEN check_violation THEN NULL;
    END;
END
$$;

INSERT INTO audit.audit_logs (
    actor_type, actor_id, action, object_type, object_id, request_id
) VALUES (
    'system', NULL, 'contract.verify', 'database', NULL, 'postgres-contracts'
);

DO $$
BEGIN
    BEGIN
        UPDATE audit.audit_logs
        SET action = 'contract.mutated'
        WHERE request_id = 'postgres-contracts';
        RAISE EXCEPTION 'append-only audit row was mutable';
    EXCEPTION
        WHEN raise_exception THEN
            IF SQLERRM <> 'audit records are append-only' THEN
                RAISE;
            END IF;
    END;
END
$$;

INSERT INTO audit.media_reconciliation_runs (
    run_id, run_status, expected_count, started_at
) VALUES (
    'a2000000-0000-4000-8000-000000000001', 'running', 2, now()
);

UPDATE audit.media_reconciliation_runs
SET run_status = 'completed', completed_at = now(), missing_s3_count = 1,
    issue_samples = '{"missing_s3":["media-public/a0/missing.webp"]}'::jsonb
WHERE run_id = 'a2000000-0000-4000-8000-000000000001';

DO $$
BEGIN
    BEGIN
        INSERT INTO audit.media_reconciliation_runs (
            run_status, expected_count, started_at
        ) VALUES ('completed', 0, now());
        RAISE EXCEPTION 'completed reconciliation accepted without completion evidence';
    EXCEPTION
        WHEN check_violation THEN NULL;
    END;
END
$$;

ROLLBACK;

\echo 'PostgreSQL Release A contracts passed'
