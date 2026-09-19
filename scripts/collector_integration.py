#!/usr/bin/env python3
"""Test-only, fail-closed archive -> PostgreSQL staging -> authenticated preview."""
from __future__ import annotations

import argparse
import hashlib
import http.cookies
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import uuid
import unicodedata
from functools import lru_cache
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit
from urllib.request import ProxyHandler, Request, build_opener

ROOT = Path(__file__).resolve().parents[1]
ARCHIVE = ROOT.parent / 'self-deepsearch-collector/runtime/jable-archive/20260919-final'
DATA_COMMIT = '95cac10a4579a4fd20f15575c2e77a351bb1284b'
MANIFEST_HASH = 'ddc94eaf953efe1bfbb42cbd550d09b7e97de0c3a1d01253e9246cc100fa109d'
SOURCE = str(uuid.uuid5(uuid.NAMESPACE_URL, 'self-deepsearch:test-integration:jable_reference'))
HASH = re.compile(r'^sha256:[0-9a-f]{64}$')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def safe_path(root, relative):
    require(isinstance(relative, str) and not Path(relative).is_absolute(), 'absolute archive path')
    target = (root / relative).resolve()
    require(target.is_relative_to(root.resolve()) and target.is_file(), 'missing or escaping archive path')
    return target


def jsonl(path):
    return [json.loads(line) for line in path.read_text(encoding='utf-8').splitlines() if line.strip()]


def load_archive(root):
    root = root.resolve()
    raw_manifest = (root / 'archive-manifest.json').read_bytes()
    require(digest(raw_manifest) == MANIFEST_HASH, 'unrecognized pinned archive manifest')
    manifest = json.loads(raw_manifest)
    require(manifest['source_id'] == 'jable_reference', 'wrong source')
    for entry in manifest['files']:
        data = safe_path(root, entry['file']).read_bytes()
        require(len(data) == entry['bytes'] and digest(data) == entry['sha256'], 'archive checksum mismatch')
    with sqlite3.connect((root / 'collector.db').as_uri() + '?mode=ro&immutable=1', uri=True) as db:
        require(db.execute('PRAGMA integrity_check').fetchone()[0] == 'ok', 'SQLite integrity failure')
        sqlite_people = {row[0]: row[1] for row in db.execute('SELECT performer_id,current_content_hash FROM performers')}
    people = jsonl(root / 'selfdeepsearch-export/performers.jsonl')
    media = jsonl(root / 'performer_media_candidates.jsonl')
    require(len(people) == 4196 and len(media) == 130, 'unexpected pinned dataset counts')
    by_id, external_ids = {}, set()
    for person in people:
        pid = person['performer_id']
        require(str(uuid.UUID(pid)) == pid and pid not in by_id, 'duplicate or invalid performer UUID')
        require(person['external_id'] not in external_ids and 0 < len(person['external_id']) <= 255, 'duplicate source key')
        require(person['source_id'] == 'jable_reference', 'unexpected source')
        require(person['rights_status'] == 'needs_review' and person['publication_status'] == 'staging', 'unsafe publication state')
        require(0 < len(person['name'].strip()) <= 200 and bool(HASH.fullmatch(person['current_content_hash'])), 'invalid performer')
        require(sqlite_people.get(pid) == person['current_content_hash'], 'SQLite/export mismatch')
        require(urlsplit(person['profile_url']).hostname == 'jable.tv', 'unexpected profile host')
        datetime.fromisoformat(person['checked_at'].replace('Z', '+00:00'))
        by_id[pid] = person
        external_ids.add(person['external_id'])
    media_ids, owners = set(), set()
    for item in media:
        mid = item['media_candidate_id']
        require(bool(re.fullmatch('[a-f0-9]{64}', mid)) and mid not in media_ids, 'duplicate or invalid media ID')
        owner = by_id.get(item['performer_id'])
        require(owner is not None and item['performer_id'] not in owners, 'missing or duplicate media owner')
        require(item['source_id'] == 'jable_reference' and item['external_id'] == owner['external_id'], 'media source mismatch')
        require(item['purpose'] == 'avatar' and item['download_status'] == 'downloaded', 'invalid media state')
        require(item['rights_status'] == 'needs_review' and item['review_status'] == 'pending', 'unsafe media state')
        data = safe_path(root, item['local_path']).read_bytes()
        require(digest(data) == item['sha256'] and len(data) == item['byte_size'], 'avatar checksum mismatch')
        require(item['mime_type'] == 'image/jpeg' and data.startswith(b'\xff\xd8\xff'), 'invalid JPEG')
        require(item['width'] == 125 and item['height'] == 125, 'unexpected avatar dimensions')
        item['staging_id'] = str(uuid.uuid5(uuid.NAMESPACE_URL, 'jable_reference:media:' + mid))
        owner['avatar'] = {key: item[key] for key in ['media_candidate_id', 'sha256', 'width', 'height', 'byte_size']}
        media_ids.add(mid)
        owners.add(item['performer_id'])
    require(len(sqlite_people) == len(people), 'SQLite count mismatch')
    exported_media = jsonl(root / 'selfdeepsearch-export/performer_media_staging.jsonl')
    require({m['media_candidate_id'] for m in exported_media} == media_ids, 'media export mismatch')
    for item in exported_media:
        candidate = next(m for m in media if m['media_candidate_id'] == item['media_candidate_id'])
        require(item['entity_id'] == candidate['performer_id'] and item['content_hash'] == 'sha256:' + candidate['sha256'], 'media export relation mismatch')
    return people, media, {'manifest_sha256': digest(raw_manifest), 'files_verified': len(manifest['files']),
                           'performers': len(people), 'avatars': len(media), 'without_avatar': len(people) - len(media),
                           'sqlite_integrity': 'ok', 'source_data_commit': DATA_COMMIT}


def literal(value):
    """Hex encodes untrusted strings, independent of SQL string escaping settings."""
    return "convert_from(decode('" + value.encode('utf-8').hex() + "','hex'),'UTF8')"


def postgres(sql, *, write=False):
    password = os.environ.get('INTEGRATION_DB_PASSWORD')
    require(bool(password), 'INTEGRATION_DB_PASSWORD is required (never logged)')
    environment = {k: v for k, v in os.environ.items() if not k.startswith('PG')}
    environment.update(PGPASSWORD=password, PGCONNECT_TIMEOUT='3', PGOPTIONS='-c statement_timeout=15000')
    role = 'platform' if write else 'platform_api_login'
    prefix = '' if write else 'BEGIN READ ONLY;\n'
    suffix = '' if write else '\nCOMMIT;'
    result = subprocess.run(['psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1',
                             '-h', '127.0.0.1', '-p', '15432', '-U', role, '-d', 'self_deepsearch'],
                            input=prefix + sql + suffix, text=True, capture_output=True, env=environment, timeout=25)
    # Do not echo SQL/data/credentials on failures.
    require(result.returncode == 0, 'PostgreSQL operation failed; transaction rolled back or query rejected')
    return json.loads(result.stdout.strip()) if result.stdout.strip() else None


def import_archive(root, confirmation):
    require(confirmation == 'self-deepsearch-review-staging-only', 'explicit test environment confirmation required')
    container = subprocess.run(['docker', 'inspect', '--format', '{{index .Config.Labels "com.docker.compose.project"}}',
                                'self-deepsearch-review-postgres-1'], capture_output=True, text=True, check=True)
    require(container.stdout.strip() == 'self-deepsearch-review', 'wrong test database container')
    people, media, report = load_archive(root)
    repo = root.resolve().parents[2]
    subprocess.run(['git', '-C', str(repo), 'merge-base', '--is-ancestor', DATA_COMMIT, 'HEAD'], check=True, capture_output=True)
    report['collector_head'] = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip()
    batch = str(uuid.uuid5(uuid.NAMESPACE_URL, 'jable-integration:' + report['manifest_sha256']))
    people_json, media_json = map(lambda x: literal(json.dumps(x, ensure_ascii=False)), (people, media))
    sql = f"""
BEGIN;
SET LOCAL lock_timeout='5s';
DO $$ BEGIN PERFORM pg_advisory_xact_lock(20260919,13004); END $$;
CREATE TEMP TABLE before_counts AS SELECT
 (SELECT count(*) FROM collector.source_records WHERE source_id='{SOURCE}') AS people,
 (SELECT count(*) FROM collector.media_staging WHERE source_id='{SOURCE}') AS media,
 (SELECT count(*) FROM platform.publications) AS publications,
 (SELECT count(*) FROM platform.performers) AS platform_performers;
CREATE TEMP TABLE incoming AS SELECT value AS p FROM jsonb_array_elements({people_json}::jsonb);
CREATE TEMP TABLE incoming_media AS SELECT value AS m FROM jsonb_array_elements({media_json}::jsonb);
INSERT INTO collector.sources(source_id,name,source_type,base_url,status,rights_status)
 VALUES('{SOURCE}','jable_reference (integration staging)','public_web','https://jable.tv','paused','needs_review')
 ON CONFLICT(source_id) DO NOTHING;
INSERT INTO collector.publication_batches(batch_id,source_id,region,idempotency_key,request_hash,input_count,accepted_count,status,summary,completed_at)
 VALUES('{batch}','{SOURCE}','japan','jable-archive:{report['manifest_sha256']}','sha256:{report['manifest_sha256']}',4196,4196,'accepted',
 jsonb_build_object('mode','integration-staging-only','source_data_commit','{DATA_COMMIT}','media_count',130),now())
 ON CONFLICT(batch_id) DO NOTHING;
INSERT INTO collector.source_records(batch_id,source_id,external_id,entity_type,connector_version,idempotency_key,content_hash,payload)
 SELECT '{batch}','{SOURCE}',p->>'external_id','performer','jable-html@2026-09-19.1',
 'performer:'||(p->>'external_id')||':'||(p->>'current_content_hash'),p->>'current_content_hash',p FROM incoming
 ON CONFLICT(source_id,entity_type,external_id,content_hash) DO NOTHING;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM incoming i JOIN collector.source_records s
 ON s.source_id='{SOURCE}' AND s.external_id=i.p->>'external_id' AND s.content_hash=i.p->>'current_content_hash'
 WHERE s.payload IS DISTINCT FROM i.p) THEN RAISE EXCEPTION 'same key/hash has different payload'; END IF; END $$;
INSERT INTO collector.normalized_records(source_record_id,parser_version,entity_type,normalized_payload,candidate_status)
 SELECT s.source_record_id,'integration-archive@1','performer',i.p,'pending' FROM incoming i JOIN collector.source_records s
 ON s.source_id='{SOURCE}' AND s.external_id=i.p->>'external_id' AND s.content_hash=i.p->>'current_content_hash'
 ON CONFLICT(source_record_id) DO NOTHING;
INSERT INTO collector.media_staging(media_staging_id,source_id,source_record_id,entity_type,entity_id,candidate_url,source_type,
 rights_status,download_status,review_status,content_hash,mime_type,byte_size,checked_at)
 SELECT (m->>'staging_id')::uuid,'{SOURCE}',s.source_record_id,'performer',NULL,m->>'candidate_url','web_page',
 'needs_review','downloaded','pending','sha256:'||(m->>'sha256'),'image/jpeg',(m->>'byte_size')::bigint,(m->>'checked_at')::timestamptz
 FROM incoming_media JOIN incoming ON p->>'performer_id'=m->>'performer_id' JOIN collector.source_records s
 ON s.source_id='{SOURCE}' AND s.external_id=p->>'external_id' AND s.content_hash=p->>'current_content_hash'
 ON CONFLICT(media_staging_id) DO NOTHING;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM incoming_media i JOIN collector.media_staging s
 ON s.media_staging_id=(i.m->>'staging_id')::uuid WHERE s.source_id IS DISTINCT FROM '{SOURCE}'::uuid
 OR s.content_hash IS DISTINCT FROM 'sha256:'||(i.m->>'sha256')) THEN RAISE EXCEPTION 'media identity conflict'; END IF; END $$;
DO $$ BEGIN IF (SELECT count(*) FROM collector.source_records WHERE source_id='{SOURCE}') <> 4196
 OR (SELECT count(*) FROM collector.media_staging WHERE source_id='{SOURCE}') <> 130
 OR (SELECT count(*) FROM platform.publications) <> (SELECT publications FROM before_counts)
 OR (SELECT count(*) FROM platform.performers) <> (SELECT platform_performers FROM before_counts)
 THEN RAISE EXCEPTION 'integration count/isolation check failed'; END IF; END $$;
SELECT json_build_object('source_records',(SELECT count(*) FROM collector.source_records WHERE source_id='{SOURCE}'),
 'normalized_records',(SELECT count(*) FROM collector.normalized_records n JOIN collector.source_records s USING(source_record_id) WHERE s.source_id='{SOURCE}'),
 'media_staging',(SELECT count(*) FROM collector.media_staging WHERE source_id='{SOURCE}'),
 'inserted_people',(SELECT count(*) FROM collector.source_records WHERE source_id='{SOURCE}')-people,
 'inserted_media',(SELECT count(*) FROM collector.media_staging WHERE source_id='{SOURCE}')-media,
 'publications_delta',(SELECT count(*) FROM platform.publications)-publications,
 'platform_performers_delta',(SELECT count(*) FROM platform.performers)-platform_performers)
 FROM before_counts;
COMMIT;
"""
    report.update(postgres(sql, write=True))
    report.update(checked_at=datetime.now(timezone.utc).isoformat(), mode='test-staging-only', batch_id=batch, source_id=SOURCE)
    require(report['source_records'] == 4196 and report['media_staging'] == 130, 'target count mismatch')
    require(report['publications_delta'] == 0 and report['platform_performers_delta'] == 0, 'publication isolation failed')
    return report


CURRENT = f"""WITH current_people AS (
 SELECT DISTINCT ON (external_id) source_record_id,payload,external_id FROM collector.source_records
 WHERE source_id='{SOURCE}' AND entity_type='performer' ORDER BY external_id,received_at DESC,source_record_id
), visible AS (SELECT p.*,n.candidate_status,m.review_status AS media_review_status,m.rights_status AS media_rights_status
 FROM current_people p JOIN collector.normalized_records n USING(source_record_id)
 LEFT JOIN collector.media_staging m ON m.source_record_id=p.source_record_id AND m.content_hash='sha256:'||(p.payload->'avatar'->>'sha256')) """


@lru_cache(maxsize=1)
def search_variant_table():
    table = json.loads((ROOT / 'services/platform-api/internal/searchtext/data/variants.json').read_text(encoding='utf-8'))
    require(len(table['source']) == len(table['target']) > 0, 'invalid search variant table')
    return table


def fold_search_text(value):
    table = search_variant_table()
    return unicodedata.normalize('NFKC', value).lower().translate(str.maketrans(table['source'], table['target']))


def folded_search_sql(expression, query):
    table = search_variant_table()
    wanted = set(fold_search_text(query))
    pairs = [(a,b) for a,b in zip(table['source'],table['target']) if b in wanted]
    source,target = ''.join(a for a,b in pairs),''.join(b for a,b in pairs)
    return f"lower(translate(normalize({expression},NFKC),{literal(source)},{literal(target)}))"


def performer_search_predicate(query):
    """Stored public names and string aliases only; literal, case-insensitive match.

    Missing/null/malformed alias fields are not searched as serialized JSON.
    This never manufactures aliases or changes the source payload.
    """
    term = literal(fold_search_text(query.strip()))
    names = folded_search_sql("concat_ws(' ',payload->>'name',payload->>'name_original',payload->>'romanized_name',external_id)",query)
    alias = folded_search_sql("alias.value #>> '{}'",query)
    return f"""(position({term} in {names})>0
        OR EXISTS(SELECT 1 FROM jsonb_array_elements(
          CASE WHEN jsonb_typeof(payload->'aliases')='array' THEN payload->'aliases' ELSE '[]'::jsonb END
        ) AS alias(value) WHERE jsonb_typeof(alias.value)='string'
          AND position({term} in {alias})>0))"""


def read_people(query='', page=1, avatar='all', performer_id=None):
    require(1 <= page <= 10000 and len(query) <= 200 and avatar in ('all', 'yes', 'no'), 'invalid pagination/filter')
    conditions = [performer_search_predicate(query)]
    if avatar != 'all':
        conditions.append("(payload ? 'avatar')=" + ('true' if avatar == 'yes' else 'false'))
    if performer_id is not None:
        require(str(uuid.UUID(performer_id)) == performer_id, 'invalid performer ID')
        conditions.append("payload->>'performer_id'=" + literal(performer_id))
    return postgres(CURRENT + f""", filtered AS(SELECT * FROM visible WHERE {' AND '.join(conditions)}),
 paged AS(SELECT payload||jsonb_build_object('candidate_status',candidate_status,
 'media_review_status',media_review_status,'media_rights_status',media_rights_status) AS item FROM filtered
 ORDER BY payload->>'name',external_id LIMIT 24 OFFSET {(page-1)*24})
 SELECT json_build_object('total',(SELECT count(*) FROM filtered),'page',{page},'page_size',24,
 'counts',(SELECT json_build_object('performers',count(*),'avatars',count(*) FILTER(WHERE payload ? 'avatar'),
 'without_avatar',count(*) FILTER(WHERE NOT payload ? 'avatar')) FROM visible),
 'items',coalesce((SELECT json_agg(item) FROM paged),'[]'::json));""")


def operator_allowed(cookie):
    try:
        parsed = http.cookies.SimpleCookie(cookie)
        token = parsed.get('sd_session')
        if token is None or len(token.value) > 4096:
            return False
        request = Request('http://127.0.0.1:18080/api/v1/me', headers={'Cookie': 'sd_session=' + token.value})
        with build_opener(ProxyHandler({})).open(request, timeout=3) as response:
            user = json.load(response)['user']
            return user.get('role') in ('owner', 'admin', 'editor') and user.get('account_status', 'active') == 'active'
    except Exception:
        return False


def serve(root, expose, port):
    _, media, _ = load_archive(root)
    images = {m['media_candidate_id']: m for m in media}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def respond(self, status, body, content_type='application/json; charset=utf-8'):
            if not isinstance(body, bytes):
                body = json.dumps(body, ensure_ascii=False).encode()
            self.send_response(status)
            for key, value in {'Content-Type': content_type, 'Content-Length': str(len(body)), 'Cache-Control': 'no-store',
                               'X-Robots-Tag': 'noindex, nofollow, noarchive', 'X-Content-Type-Options': 'nosniff',
                               'Referrer-Policy': 'no-referrer', 'Content-Security-Policy': "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"}.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            path = urlsplit(self.path).path
            if path == '/healthz':
                try:
                    read_people(page=1)
                    return self.respond(200, {'status': 'ok', 'mode': 'test-staging-only'})
                except Exception:
                    return self.respond(503, {'status': 'unavailable'})
            if path == '/robots.txt':
                return self.respond(200, b'User-agent: *\nDisallow: /\n', 'text/plain')
            if not operator_allowed(self.headers.get('Cookie', '')):
                body = '<!doctype html><meta charset="utf-8"><title>请先登录</title><h1>联调预览需要后台账号</h1><p>请先在同一主机的 13001 端口登录后台，再刷新本页。仅 owner/admin/editor 可访问。</p>'
                return self.respond(401, body.encode(), 'text/html; charset=utf-8')
            try:
                if path in ('/', '/preview.js', '/preview.css'):
                    filename, mime = {'/': ('collector-integration.html', 'text/html'), '/preview.js': ('collector-integration.js', 'text/javascript'),
                                      '/preview.css': ('collector-integration.css', 'text/css')}[path]
                    return self.respond(200, (ROOT / 'scripts/templates' / filename).read_bytes(), mime + '; charset=utf-8')
                if path == '/api/performers' or path.startswith('/api/performers/'):
                    query = parse_qs(urlsplit(self.path).query)
                    data = read_people(query.get('q', [''])[0], int(query.get('page', ['1'])[0]), query.get('avatar', ['all'])[0],
                                       path.rsplit('/', 1)[-1] if path != '/api/performers' else None)
                    return self.respond(200 if data['items'] or path == '/api/performers' else 404, data)
                if path.startswith('/media/') and path.endswith('.jpg'):
                    mid = path[7:-4]
                    if mid in images:
                        item = images[mid]
                        # Recheck database review state and bytes at delivery time, not just startup.
                        person = read_people(performer_id=item['performer_id'])['items']
                        if person and person[0]['media_review_status'] != 'rejected' and person[0]['media_rights_status'] not in ('restricted', 'takedown'):
                            data = safe_path(root, item['local_path']).read_bytes()
                            require(digest(data) == item['sha256'], 'avatar changed since validation')
                            return self.respond(200, data, 'image/jpeg')
                return self.respond(404, {'error': 'not_found'})
            except (ValueError, KeyError):
                return self.respond(400, {'error': 'invalid_request'})
            except Exception:
                return self.respond(503, {'error': 'temporarily_unavailable'})

    server = ThreadingHTTPServer(('0.0.0.0' if expose else '127.0.0.1', port), Handler)
    print(f'Test-only integration preview on port {port}; existing operator session required', flush=True)
    server.serve_forever()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command', choices=['verify', 'import', 'serve'])
    parser.add_argument('--archive', type=Path, default=ARCHIVE)
    parser.add_argument('--confirm', default='')
    parser.add_argument('--report', type=Path)
    parser.add_argument('--port', type=int, default=13004)
    parser.add_argument('--expose-test-preview', action='store_true')
    args = parser.parse_args()
    if args.command == 'serve':
        serve(args.archive, args.expose_test_preview, args.port)
        return
    result = load_archive(args.archive)[2] if args.command == 'verify' else import_archive(args.archive, args.confirm)
    output = json.dumps(result, ensure_ascii=False, indent=2) + '\n'
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(output, encoding='utf-8')
    print(output)


if __name__ == '__main__':
    main()
