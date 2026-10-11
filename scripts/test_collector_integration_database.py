#!/usr/bin/env python3
"""Real review DB checks, including transaction rollback and review-state preservation."""
import json
import os
from datetime import datetime, timezone

import collector_integration as tool


def main():
    tool.require(os.environ.get('INTEGRATION_TEST_CONFIRM') == 'self-deepsearch-review-staging-only', 'test confirmation required')
    people, media, _ = tool.load_archive(tool.ARCHIVE)
    source = tool.SOURCE
    report = {'checked_at': datetime.now(timezone.utc).isoformat(), 'status': 'running', 'checks': {}}
    path = tool.ROOT / 'docs/evidence/collector-integration-2026-09-19/database-report.json'
    try:
        data = tool.postgres(f"""SELECT json_build_object(
          'records',(SELECT json_agg(payload) FROM collector.source_records WHERE source_id='{source}'),
          'normalized_count',(SELECT count(*) FROM collector.normalized_records n JOIN collector.source_records s USING(source_record_id) WHERE s.source_id='{source}'),
          'orphan_media',(SELECT count(*) FROM collector.media_staging m LEFT JOIN collector.source_records s USING(source_record_id)
                          WHERE m.source_id='{source}' AND (s.source_record_id IS NULL OR s.source_id <> m.source_id)),
          'unmapped_media',(SELECT count(*) FROM collector.media_staging WHERE source_id='{source}' AND entity_id IS NOT NULL),
          'unsafe_media',(SELECT count(*) FROM collector.media_staging WHERE source_id='{source}' AND (rights_status <> 'needs_review' OR review_status <> 'pending')));""", write=True)
        actual = {p['performer_id']: p for p in data.pop('records')}
        assert len(actual) == len(people) == 4196
        for person in people:
            assert actual[person['performer_id']] == person
        assert data == {'normalized_count':4196, 'orphan_media':0, 'unmapped_media':0, 'unsafe_media':0}, data
        report['checks']['complete_source_payload_and_relations'] = data

        # The intentional error is caught only after insertion, exercising rollback of a subtransaction.
        rollback = tool.postgres(f"""BEGIN;
          DO $$ BEGIN
            BEGIN
              INSERT INTO collector.source_records(source_id,external_id,entity_type,connector_version,idempotency_key,content_hash,payload)
              VALUES('{source}','__integration_transaction_probe__','performer','test','__rollback_probe__','sha256:{'0'*64}','{{}}');
              RAISE division_by_zero;
            EXCEPTION WHEN division_by_zero THEN NULL;
            END;
          END $$;
          SELECT json_build_object('probe_records',count(*)) FROM collector.source_records
            WHERE source_id='{source}' AND external_id='__integration_transaction_probe__';
          COMMIT;""", write=True)
        assert rollback == {'probe_records':0}
        report['checks']['failed_transaction_leaves_no_record'] = rollback

        # Touch only a record created by this integration import; never override pre-existing reviewer work.
        mid = media[0]['staging_id']
        current = tool.postgres(f"SELECT to_json(review_status) FROM collector.media_staging WHERE media_staging_id='{mid}' AND source_id='{source}';", write=True)
        assert current == 'pending', 'do not replace an existing reviewer decision'
        tool.postgres(f"BEGIN; UPDATE collector.media_staging SET review_status='reviewing' WHERE media_staging_id='{mid}' AND source_id='{source}' AND review_status='pending'; COMMIT;", write=True)
        try:
            replay = tool.import_archive(tool.ARCHIVE, 'self-deepsearch-review-staging-only')
            current = tool.postgres(f"SELECT to_json(review_status) FROM collector.media_staging WHERE media_staging_id='{mid}';", write=True)
            assert current == 'reviewing'
            assert replay['inserted_people'] == replay['inserted_media'] == replay['publications_delta'] == 0
            report['checks']['replay_preserves_manual_review_state'] = {'preserved':'reviewing','new_people':0,'new_media':0,'publications':0}
        finally:
            tool.postgres(f"BEGIN; UPDATE collector.media_staging SET review_status='pending' WHERE media_staging_id='{mid}' AND source_id='{source}' AND review_status='reviewing'; COMMIT;", write=True)
        report['status'] = 'passed'
    except Exception as error:
        report['status'] = 'failed'
        report['error_type'] = type(error).__name__
        raise
    finally:
        path.write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps(report))


if __name__ == '__main__':
    main()
