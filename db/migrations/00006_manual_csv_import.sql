-- +goose Up
INSERT INTO collector.sources (
    source_id, name, source_type, priority, status, rights_status, checked_at
) VALUES (
    '81000000-0000-4000-8000-000000000001', 'Manual CSV import', 'manual', 1, 'active', 'needs_review', now()
) ON CONFLICT (source_id) DO NOTHING;

GRANT SELECT ON collector.sources TO platform_api;
GRANT SELECT, INSERT, UPDATE ON collector.publication_batches, collector.source_records, collector.normalized_records TO platform_api;

UPDATE platform.system_metadata SET schema_version = 6 WHERE singleton;

-- +goose Down
UPDATE platform.system_metadata SET schema_version = 5 WHERE singleton;

REVOKE SELECT, INSERT, UPDATE ON collector.publication_batches, collector.source_records, collector.normalized_records FROM platform_api;
REVOKE SELECT ON collector.sources FROM platform_api;

DELETE FROM collector.normalized_records normalized
USING collector.source_records record
WHERE normalized.source_record_id = record.source_record_id
  AND record.source_id = '81000000-0000-4000-8000-000000000001';

DELETE FROM collector.source_records
WHERE source_id = '81000000-0000-4000-8000-000000000001';

DELETE FROM collector.publication_batches
WHERE source_id = '81000000-0000-4000-8000-000000000001';

DELETE FROM collector.sources
WHERE source_id = '81000000-0000-4000-8000-000000000001';
