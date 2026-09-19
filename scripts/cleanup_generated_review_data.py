#!/usr/bin/env python3
"""Precisely remove known review fixtures; retain real imports and immutable audits."""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
CONTAINER = 'self-deepsearch-review-postgres-1'
OWNER = '62123194-4b40-44cc-bbe1-17187afc1ef5'
REAL_SOURCE = '6e3efba4-6939-5e47-8bfd-16c07ddc68fc'
FIXTURE_SOURCE = '80000000-0000-4000-8000-000000000001'
WORKS = [f'10000000-0000-4000-8000-{i:012d}' for i in range(1,101)]
PERFORMERS = [f'60000000-0000-4000-8000-{i:012d}' for i in range(1,31)]
STUDIOS = ['70000000-0000-4000-8000-000000000001']
USERS = [
    '90000000-0000-4000-8000-000000000001','90000000-0000-4000-8000-000000000002',
    'eb7b11a2-b1e5-4d37-8246-97ab17406ddb','777a7898-20a7-41ae-9a8f-d1352176bd37',
    'bea2f9c3-3c37-41a9-a44f-3af3da4160ee','31eb5497-d959-42e2-99ac-642888c275b8',
    '41772e77-8184-44c8-992b-907ffad11c28',
]
ADS = ['746e1ec7-d005-4d3c-9d9e-d65208b08555','2b9a5d87-5d1c-4d64-9e80-69f286b2319c',
 '75c84db0-78f3-47c1-9faa-8746e4f09470','d06d91b0-d35e-4f4f-8a7f-e9a40682d011',
 'ceff7cf1-3ebe-4a5e-b8c3-022db2876e96','849122e2-e8d7-4fcb-aa6d-2a80c5f49c0f',
 '2e6bd9d8-7dc5-4654-8273-9ff8b22c1d94','dc5b137b-cd7d-43c2-b555-f60ad0a08654',
 '5e1a0ea8-ada3-40a8-bb3e-68c45f94a2d1','86d1c7d4-298a-41cc-8e9a-bb08adb5385e',
 '194d0be2-123d-473c-b09e-3a905d395b55','1bf5f09b-ff4c-40be-810f-7c75a79909d7',
 'd5cab002-c1df-4ba0-b733-55915a1b7edf','61f8cf16-62b2-48db-8ff9-e1c243a41492',
 '9f9293dd-9af9-4d55-98a8-8fc68cf6e534','d74c2269-2802-41b6-ac33-326526a61775',
 '2a74a66a-330a-4e86-b087-ad2175521d8c','bc73e630-199e-4518-9bab-6fd7987dab07',
 '7d092899-7518-4735-b665-019ff4fdf823','4a072385-171c-4130-87f2-8aaa52744995',
 '9ac14336-2e55-4263-908e-b3973e9c71a0']


def ids(values):
    return ','.join("'"+value+"'::uuid" for value in values)


def sql(query):
    result = subprocess.run(['docker','exec','-i',CONTAINER,'psql','-X','-q','-A','-t','-v','ON_ERROR_STOP=1',
                             '-U','platform','-d','self_deepsearch'], input=query,text=True,capture_output=True)
    if result.returncode:
        raise RuntimeError('SQL transaction failed/rolled back: '+result.stderr[-2000:])
    return json.loads(result.stdout) if result.stdout.strip() else None


def snapshot():
    return sql(f"""SELECT json_build_object(
      'fixture_works',(SELECT count(*) FROM platform.works WHERE work_id IN ({ids(WORKS)})),
      'fixture_performers',(SELECT count(*) FROM platform.performers WHERE performer_id IN ({ids(PERFORMERS)})),
      'fixture_studios',(SELECT count(*) FROM platform.studios WHERE studio_id IN ({ids(STUDIOS)})),
      'mock_ads',(SELECT count(*) FROM platform.ad_campaigns WHERE campaign_id IN ({ids(ADS)})),
      'test_users',(SELECT count(*) FROM platform.users WHERE user_id IN ({ids(USERS)})),
      'real_records',(SELECT count(*) FROM collector.source_records WHERE source_id='{REAL_SOURCE}'),
      'real_normalized',(SELECT count(*) FROM collector.normalized_records n JOIN collector.source_records s USING(source_record_id) WHERE s.source_id='{REAL_SOURCE}'),
      'real_media',(SELECT count(*) FROM collector.media_staging WHERE source_id='{REAL_SOURCE}'),
      'owner_active',(SELECT count(*) FROM platform.users WHERE user_id='{OWNER}' AND role='owner' AND account_status='active'),
      'audit_rows',(SELECT count(*) FROM audit.audit_logs),'login_audit_rows',(SELECT count(*) FROM audit.login_events));""")


def cleanup_query():
    entities = ids(WORKS+PERFORMERS+STUDIOS)
    users,ads = ids(USERS),ids(ADS)
    return f"""BEGIN;
SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='30s';
LOCK TABLE platform.works,platform.performers,platform.studios,platform.users,platform.ad_campaigns IN SHARE ROW EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM platform.works WHERE work_id IN ({ids(WORKS)}) AND canonical_code !~ '^(TEST|DEMO)-[0-9]{{3}}$')
 OR EXISTS(SELECT 1 FROM platform.performers WHERE performer_id IN ({ids(PERFORMERS)}) AND display_name NOT LIKE '测试人物%')
 OR EXISTS(SELECT 1 FROM platform.ad_campaigns WHERE campaign_id IN ({ads}) AND (advertiser <> '幕鉴测试环境' OR name NOT LIKE '%模拟广告%'))
 THEN RAISE EXCEPTION 'target no longer matches audited fixture'; END IF;
 IF (SELECT count(*) FROM collector.source_records WHERE source_id='{REAL_SOURCE}') <> 4196
 OR (SELECT count(*) FROM collector.media_staging WHERE source_id='{REAL_SOURCE}') <> 130
 THEN RAISE EXCEPTION 'real data baseline changed'; END IF;
END $$;
CREATE TEMP TABLE deleted_counts(name text,n bigint);
CREATE TEMP TABLE target_users AS SELECT user_id,normalized_email FROM platform.users WHERE user_id IN ({users});
CREATE TEMP TABLE retained_audit_users AS SELECT user_id FROM target_users WHERE user_id IN (SELECT user_id FROM audit.login_events)
 OR user_id IN (SELECT author_id FROM platform.content_revisions UNION SELECT reviewer_id FROM platform.content_revisions);
UPDATE platform.ad_campaigns SET published_version_id=NULL WHERE campaign_id IN ({ads});
WITH gone AS(DELETE FROM platform.ad_campaign_versions WHERE campaign_id IN ({ads}) RETURNING 1) INSERT INTO deleted_counts SELECT 'ad_versions',count(*) FROM gone;
WITH gone AS(DELETE FROM platform.ad_campaigns WHERE campaign_id IN ({ads}) RETURNING 1) INSERT INTO deleted_counts SELECT 'ad_campaigns',count(*) FROM gone;
DELETE FROM platform.seo_issue_events WHERE issue_id IN(SELECT issue_id FROM platform.seo_issues WHERE entity_id IN ({entities}));
DELETE FROM platform.seo_issues WHERE entity_id IN ({entities});
DELETE FROM platform.editorial_recommendations WHERE work_id IN ({ids(WORKS)});
DELETE FROM collector.conflict_reviews WHERE entity_id IN ({entities});
WITH gone AS(DELETE FROM collector.review_tasks WHERE entity_id IN ({entities}) RETURNING 1) INSERT INTO deleted_counts SELECT 'review_tasks',count(*) FROM gone;
INSERT INTO deleted_counts SELECT 'immutable_field_provenance_retained',count(*) FROM platform.field_provenance WHERE entity_id IN ({entities});
DELETE FROM platform.search_documents WHERE entity_id IN ({entities});
WITH gone AS(DELETE FROM platform.content_metrics_hourly WHERE content_id IN ({entities}) RETURNING page_views) INSERT INTO deleted_counts SELECT 'synthetic_content_pv',coalesce(sum(page_views),0) FROM gone;
WITH gone AS(DELETE FROM platform.view_history WHERE content_id IN ({entities}) OR user_id IN ({users}) RETURNING 1) INSERT INTO deleted_counts SELECT 'view_history',count(*) FROM gone;
DELETE FROM platform.entity_media WHERE entity_id IN ({entities});
DELETE FROM platform.entity_merges WHERE source_entity_id IN ({entities}) OR target_entity_id IN ({entities});
DELETE FROM platform.outbox_events WHERE aggregate_id IN ({entities}) AND status='completed';
DELETE FROM platform.publications WHERE entity_id IN ({entities});
INSERT INTO deleted_counts SELECT 'immutable_revision_snapshots_retained',count(*) FROM platform.content_revisions WHERE entity_id IN ({entities});
DELETE FROM platform.work_performers WHERE work_id IN ({ids(WORKS)}) OR performer_id IN ({ids(PERFORMERS)});
WITH gone AS(DELETE FROM platform.works WHERE work_id IN ({ids(WORKS)}) RETURNING 1) INSERT INTO deleted_counts SELECT 'works',count(*) FROM gone;
WITH gone AS(DELETE FROM platform.performers WHERE performer_id IN ({ids(PERFORMERS)}) RETURNING 1) INSERT INTO deleted_counts SELECT 'performers',count(*) FROM gone;
WITH gone AS(DELETE FROM platform.studios WHERE studio_id IN ({ids(STUDIOS)}) RETURNING 1) INSERT INTO deleted_counts SELECT 'studios',count(*) FROM gone;
UPDATE collector.sources SET status='disabled' WHERE source_id='{FIXTURE_SOURCE}' AND source_type='fixture';
DELETE FROM platform.sessions WHERE user_id IN ({users});
DELETE FROM platform.user_invitations WHERE normalized_email IN (SELECT normalized_email FROM target_users);
DELETE FROM platform.email_challenges WHERE user_id IN ({users}) OR normalized_email IN (SELECT normalized_email FROM target_users);
UPDATE platform.users SET account_status='closed',closed_at=coalesce(closed_at,now()),is_test=true,registration_origin='fixture',
 normalized_email='deleted-test-'||user_id::text||'@example.invalid',password_hash='$removed-test-account$not-a-login-hash'
 WHERE user_id IN (SELECT user_id FROM retained_audit_users);
WITH gone AS(DELETE FROM platform.users WHERE user_id IN ({users}) AND user_id NOT IN (SELECT user_id FROM retained_audit_users) RETURNING 1) INSERT INTO deleted_counts SELECT 'users_hard_deleted',count(*) FROM gone;
INSERT INTO deleted_counts SELECT 'audit_user_tombstones_retained',count(*) FROM retained_audit_users;
DO $$ BEGIN
 IF (SELECT count(*) FROM collector.source_records WHERE source_id='{REAL_SOURCE}') <> 4196
 OR (SELECT count(*) FROM collector.media_staging WHERE source_id='{REAL_SOURCE}') <> 130
 OR NOT EXISTS(SELECT 1 FROM platform.users WHERE user_id='{OWNER}' AND account_status='active' AND role='owner')
 THEN RAISE EXCEPTION 'protected data changed'; END IF;
END $$;
SELECT json_object_agg(name,n) FROM deleted_counts;
COMMIT;"""


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--apply',action='store_true')
    parser.add_argument('--confirm',default='')
    args=parser.parse_args()
    project=subprocess.check_output(['docker','inspect','--format','{{index .Config.Labels "com.docker.compose.project"}}',CONTAINER],text=True).strip()
    assert project=='self-deepsearch-review'
    before=snapshot()
    if not args.apply:
        print(json.dumps(before,indent=2));return
    assert args.confirm=='delete-known-generated-review-fixtures'
    assert before['owner_active']==1 and before['real_records']==4196 and before['real_media']==130
    backup_dir=ROOT/'.cache/cleanup-generated-2026-09-19'
    backup_dir.mkdir(mode=0o700,exist_ok=True);backup_dir.chmod(0o700)
    stamp=datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    backup=backup_dir/f'before-{stamp}.dump'
    with backup.open('xb') as stream:
        os.chmod(backup,0o600)
        subprocess.run(['docker','exec',CONTAINER,'pg_dump','-U','platform','-d','self_deepsearch','-Fc'],stdout=stream,check=True)
    assert backup.stat().st_size>10000
    # pg_restore list proves this is a readable custom-format backup before any deletion.
    with backup.open('rb') as stream:
        subprocess.run(['docker','exec','-i',CONTAINER,'pg_restore','--list'],stdin=stream,stdout=subprocess.DEVNULL,check=True)
    counts=sql(cleanup_query())
    after=snapshot()
    for key in ['real_records','real_normalized','real_media','owner_active','audit_rows','login_audit_rows']:
        assert before[key]==after[key],key
    report={'checked_at':datetime.now(timezone.utc).isoformat(),'status':'passed','before':before,'deleted':counts,'after':after,
            'backup_relative_path':str(backup.relative_to(ROOT)),'backup_bytes':backup.stat().st_size,
            'backup_sha256':hashlib.sha256(backup.read_bytes()).hexdigest(),
            'retained_reason':'Append-only field provenance and its revision snapshots/source remain as historical evidence; referenced test users are closed anonymized tombstones. No audit trigger was disabled.'}
    evidence=ROOT/'docs/evidence/cleanup-search-traffic-2026-09-19'
    evidence.mkdir(parents=True,exist_ok=True)
    (evidence/'cleanup-report.json').write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report,indent=2))


if __name__=='__main__':
    main()
