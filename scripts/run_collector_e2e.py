#!/usr/bin/env python3
"""Exercise both repositories through real HTTP and PostgreSQL, with an optional review environment."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import secrets
import shutil
import signal
import socket
import sqlite3
import subprocess
import time
import uuid
from datetime import datetime, timezone
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener

from collector_integration import ARCHIVE, load_archive

ROOT = Path(__file__).resolve().parents[1]
SOURCE = '12345678-1234-1234-1234-123456789abc'
HTTP = build_opener(ProxyHandler({}))


def free_port():
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        return listener.getsockname()[1]


def response(url, *, data=None, headers=None):
    request = Request(url, data=data, headers=headers or {})
    try:
        with HTTP.open(request, timeout=20) as result:
            return result.status, result.read(), result.headers
    except HTTPError as error:
        return error.code, error.read(), error.headers


def ready(url, process):
    for _ in range(80):
        if process.poll() is not None:
            raise RuntimeError('acceptance service exited before readiness; see local service log')
        try:
            if response(url)[0] == 200:
                return
        except (URLError, TimeoutError, ConnectionError):
            pass
        time.sleep(.25)
    raise RuntimeError('acceptance service did not become ready')


def run(args):
    root = args.output.resolve()
    root.mkdir(parents=True, exist_ok=False)
    root.chmod(0o700)
    report = {'status': 'running', 'checked_at': datetime.now(timezone.utc).isoformat(), 'checks': {}}
    checks = report['checks']
    processes, log_files = [], []
    container = 'selfdeepsearch-collector-e2e-' + uuid.uuid4().hex[:10]
    started = False
    env = {key: value for key, value in os.environ.items() if not key.endswith('TEST_DATABASE_URL')}
    collector = args.collector.resolve()
    cli = ['node', str(collector / 'scripts/run_collector.mjs')]
    go = shutil.which('go')
    if not go:
        raise RuntimeError('Go must be available on PATH')

    def command(argv, *, input_text=None, timeout=180, command_env=None, log=None):
        result = subprocess.run(argv, input=input_text, text=True, cwd=ROOT,
                                env=command_env or env, capture_output=True, timeout=timeout, check=False)
        if log:
            (root / log).write_text(result.stdout + result.stderr, encoding='utf-8')
        if result.returncode:
            raise RuntimeError(f'{Path(argv[0]).name} failed (exit {result.returncode}); see {log or "local command output"}')
        return result.stdout

    def start(argv, service_env, log):
        handle = (root / log).open('w', encoding='utf-8')
        log_files.append(handle)
        process = subprocess.Popen(argv, cwd=ROOT, env=service_env, stdout=handle, stderr=handle,
                                   start_new_session=True)
        processes.append(process)
        return process

    try:
        people, _, archive_report = load_archive(ARCHIVE)
        checks['archive_integrity'] = archive_report
        checks['collector_head'] = command(['git', '-C', str(collector), 'rev-parse', 'HEAD']).strip()
        checks['platform_head'] = command(['git', 'rev-parse', 'HEAD']).strip()
        code_paths = list((ROOT / 'services/platform-api').rglob('*.go'))
        for folder in ['apps/ops-web/app', 'apps/ops-web/components', 'apps/ops-web/lib']:
            code_paths.extend(path for path in (ROOT / folder).rglob('*') if path.suffix in {'.ts', '.tsx', '.css'})
        code_paths.extend([ROOT / 'scripts/run_collector_e2e.py', ROOT / 'scripts/test_collector_e2e_browser.mjs'])
        checks['platform_code_sha256'] = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(code_paths)}
        checks['collector_code_sha256'] = {name: hashlib.sha256((collector / name).read_bytes()).hexdigest()
                                          for name in ['scripts/run_collector.mjs', 'workers/collector-python/collector/delivery.py']}
        checks['working_tree_note'] = 'Includes uncommitted implementation; file hashes identify the tested code.'
        command([go, 'build', '-o', str(root / 'platform-api'), './services/platform-api/cmd/api'], log='api-build.log')
        command([go, 'build', '-o', str(root / 'create-owner'), './services/platform-api/cmd/create-owner'], log='owner-build.log')
        if not args.skip_web_build:
            command(['npm', 'run', 'build', '--workspace', '@self-deepsearch/ops-web'], timeout=600, log='ops-build.log')
        command(['docker', 'run', '--rm', '-d', '--name', container, '--pull=never',
                 '--label', f'selfdeepsearch.collector-acceptance={container}',
                 '-e', 'POSTGRES_PASSWORD=disposable-collector-e2e-only',
                 '-e', 'POSTGRES_DB=self_deepsearch_migrate_test', '-p', '127.0.0.1::5432', 'postgres:16-alpine'])
        started = True
        command(['docker', 'exec', container, 'sh', '-c',
                 'for i in $(seq 1 30); do pg_isready -U postgres >/dev/null && exit 0; sleep 1; done; exit 1'])
        port = json.loads(command(['docker', 'inspect', '--format', '{{json .NetworkSettings.Ports}}', container]))['5432/tcp'][0]['HostPort']
        psql = ['docker', 'exec', '-i', container, 'psql', '-X', '-qAt', '-U', 'postgres',
                '-d', 'self_deepsearch_migrate_test', '-v', 'ON_ERROR_STOP=1']
        migrations = sorted((ROOT / 'db/migrations').glob('*.sql'))
        sql = '\n'.join(p.read_text().split('-- +goose Down')[0].replace('-- +goose Up', '') for p in migrations)
        sql += f"""
CREATE ROLE platform_api_login LOGIN PASSWORD 'disposable-collector-runtime-only';
GRANT platform_api TO platform_api_login;
INSERT INTO collector.sources(source_id,name,source_type,base_url,status,rights_status)
VALUES('{SOURCE}','Jable collector acceptance','public_web','https://jable.tv','active','needs_review');
"""
        command(psql, input_text=sql, log='migrations.log')
        api_port, ops_port = free_port(), free_port()
        while api_port == ops_port:
            ops_port = free_port()
        api_url, ops_url = f'http://127.0.0.1:{api_port}', f'http://127.0.0.1:{ops_port}'
        password, token = secrets.token_urlsafe(24), secrets.token_urlsafe(48)
        service_env = dict(env, COLLECTOR_E2E_RUN_ID=container, APP_ENV='development', API_ADDR=f'127.0.0.1:{api_port}',
                           DATABASE_URL=f'postgres://platform_api_login:disposable-collector-runtime-only@127.0.0.1:{port}/self_deepsearch_migrate_test?sslmode=disable',
                           AUTH_HMAC_SECRET=secrets.token_urlsafe(48), SMTP_URL='smtp://127.0.0.1:9?from=acceptance@example.test&insecure=true',
                           TURNSTILE_BYPASS='true', ALLOWED_ORIGINS=f'{ops_url},{api_url}',
                           COLLECTOR_INGEST_SOURCE_ID=SOURCE, COLLECTOR_INGEST_TOKEN=token,
                           S3_PUBLIC_BASE_URL='https://media.example.invalid', SEO_DAILY_SCAN_ENABLED='false',
                           METRICS_TOKEN='', MEDIA_EDGE_POLICY_MODE='off', MEDIA_DELIVERY_MODE='normal',
                           MEDIA_DELIVERY_DYNAMIC_MODE='off', VISITOR_METRICS_ENABLED='false')
        command([str(root / 'create-owner')], command_env=dict(service_env,
                BOOTSTRAP_OWNER_EMAIL='collector-owner@example.test', BOOTSTRAP_OWNER_PASSWORD=password), log='owner-bootstrap.log')
        command(psql, input_text="""
INSERT INTO platform.users(normalized_email,email_verified_at,password_hash,role,account_status,registration_origin)
SELECT 'collector-'||roles.role||'@example.test',now(),u.password_hash,roles.role,'active','bootstrap'
FROM platform.users u CROSS JOIN (VALUES('admin'),('editor'),('user')) roles(role)
WHERE u.normalized_email='collector-owner@example.test';
""")
        api_process = start([str(root / 'platform-api')], service_env, 'api.log')
        ready(api_url + '/readyz', api_process)
        shutil.copy2(ARCHIVE / 'collector.db', root / 'collector.db')
        # The real collector CLI owns both snapshot export and HTTP delivery.
        prepared = json.loads(command(cli + ['export-performer-batches', '--database', str(root / 'collector.db'),
                              '--source-key', 'jable_reference', '--source-id', SOURCE, '--source-url', 'https://jable.tv',
                              '--region', 'japan', '--output-dir', str(root / 'batches')], log='collector-export.log'))
        checks['collector_export'] = prepared
        deliver = cli + ['deliver-performer-batches', '--manifest', str(root / 'batches/manifest.json'),
                         '--endpoint', api_url + '/internal/v1/collector/performer-batches',
                         '--allow-loopback-http', '--receipts-dir', str(root / 'receipts')]
        sender_env = dict(env, COLLECTOR_INGEST_TOKEN=token)
        checks['first_delivery'] = json.loads(command(deliver, command_env=sender_env, log='collector-delivery-first.log'))
        checks['resumed_delivery'] = json.loads(command(deliver, command_env=sender_env, log='collector-delivery-resume.log'))
        if checks['first_delivery']['submitted'] != 9 or checks['resumed_delivery']['skipped'] != 9 or checks['resumed_delivery']['requests'] != 0:
            raise RuntimeError('first delivery or resume counters mismatch')
        checks['replayed_delivery'] = json.loads(command(deliver + ['--replay'], command_env=sender_env, log='collector-delivery-replay.log'))
        if checks['replayed_delivery']['replayed'] != 9:
            raise RuntimeError('expected nine server-confirmed replays')
        receipt_file = sorted((root / 'receipts').glob('batch-*.json'))[-1]
        receipt_file.rename(receipt_file.with_suffix('.saved'))
        checks['lost_receipt_recovery'] = json.loads(command(deliver, command_env=sender_env, log='collector-delivery-recover.log'))
        if checks['lost_receipt_recovery']['requests'] != 1 or checks['lost_receipt_recovery']['replayed'] != 1:
            raise RuntimeError('lost receipt recovery failed')
        batches = sorted((root / 'batches').glob('batch-*.json'))
        if len(batches) != 9:
            raise RuntimeError('expected nine collector request files')
        raw = batches[0].read_bytes()
        headers = {'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json',
                   'X-Content-SHA256': 'sha256:' + hashlib.sha256(raw).hexdigest()}
        checks['wrong_credential_status'] = response(api_url + '/internal/v1/collector/performer-batches', data=raw,
                                                   headers=dict(headers, Authorization='Bearer incorrect'))[0]
        changed = json.loads(raw)
        changed['records'][0]['payload']['name'] += ' altered'
        modified = json.dumps(changed, ensure_ascii=False, separators=(',', ':')).encode()
        checks['same_key_changed_content_status'] = response(api_url + '/internal/v1/collector/performer-batches', data=modified,
                   headers=dict(headers, **{'X-Content-SHA256': 'sha256:' + hashlib.sha256(modified).hexdigest()}))[0]
        if checks['wrong_credential_status'] != 401 or checks['same_key_changed_content_status'] != 409:
            raise RuntimeError('authentication or idempotency rejection failed')
        summary = json.loads(command(psql, input_text=f"""
SELECT json_build_object('source_records',(SELECT count(*) FROM collector.source_records WHERE source_id='{SOURCE}'),
'candidates',(SELECT count(*) FROM collector.normalized_records),'batches',(SELECT count(*) FROM collector.publication_batches),
'formal_performers',(SELECT count(*) FROM platform.performers),'publications',(SELECT count(*) FROM platform.publications),
'media',(SELECT count(*) FROM platform.media_assets),'staged_media',(SELECT count(*) FROM collector.media_staging),'unsafe_candidates',(SELECT count(*) FROM collector.normalized_records WHERE candidate_status<>'pending'));
"""))
        checks['database'] = summary
        if summary != {'source_records': 4196, 'candidates': 4196, 'batches': 9,
                       'formal_performers': 0, 'publications': 0, 'media': 0, 'staged_media': 0, 'unsafe_candidates': 0}:
            raise RuntimeError('database reconciliation or publication isolation failed')
        expected = [{'external_id': person['external_id'], 'name': person['name'], 'profile_url': person['profile_url'],
                     'source_content_hash': person['current_content_hash'], 'checked_at': person['checked_at']} for person in people]
        with sqlite3.connect((root / 'collector.db').as_uri() + '?mode=ro&immutable=1', uri=True) as db:
            observations = {external: json.loads(raw) for external, raw in db.execute(
                'SELECT p.external_id,o.candidate_json FROM performers p JOIN performer_observations o '
                'ON o.source_id=p.source_id AND o.external_id=p.external_id AND o.content_hash=p.current_content_hash '
                "WHERE p.source_id='jable_reference'")}
        for item in expected:
            observation = observations[item['external_id']]
            item['aliases'] = observation['payload'].get('aliases', [])
            item['connector_version'] = observation['provenance']['connector_version']
        (root / 'expected.json').write_text(json.dumps(expected, ensure_ascii=False), encoding='utf-8')
        ops_process = start(['node', 'scripts/start-next-standalone.mjs',
                            'apps/ops-web/.next/standalone/apps/ops-web/server.js', str(ops_port)],
                            dict(env, COLLECTOR_E2E_RUN_ID=container, HOSTNAME='127.0.0.1', PORT=str(ops_port), PLATFORM_API_URL=api_url,
                                 APP_ENV='test', TURNSTILE_BYPASS='true'), 'ops.log')
        ready(ops_url + '/login', ops_process)
        browser_env = dict(env, COLLECTOR_E2E_API_URL=api_url, COLLECTOR_E2E_OPS_URL=ops_url,
                           COLLECTOR_E2E_PASSWORD=password, COLLECTOR_E2E_OUTPUT=str(root))
        command(['node', 'scripts/test_collector_e2e_browser.mjs'], command_env=browser_env,
                timeout=240, log='browser.log')
        checks['browser'] = json.loads((root / 'browser-report.json').read_text())
        report.update(status='passed', api_url=api_url, ops_url=ops_url + '/collector', kept=args.keep,
                      scope='real collector CLI -> real HTTP API -> PostgreSQL -> authenticated ops browser')
        if args.keep:
            credentials = {'email': 'collector-owner@example.test', 'password': password,
                           'api_url': api_url, 'ops_url': ops_url, 'container': container,
                           'pids': [process.pid for process in processes], 'run_id': container}
            with (root / 'access.json').open('x', encoding='utf-8') as access:
                json.dump(credentials, access, indent=2)
            (root / 'access.json').chmod(0o600)
        return report
    except BaseException as error:
        report.update(status='failed', failure=str(error))
        raise
    finally:
        preserve = False
        try:
            (root / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
            preserve = args.keep and report['status'] == 'passed'
        finally:
            if not preserve:
                for process in reversed(processes):
                    process.terminate()
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
                if started:
                    command(['docker', 'rm', '-f', container])
            for handle in log_files:
                handle.close()



if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--collector', type=Path, default=ROOT.parent / 'self-deepsearch-collector')
    parser.add_argument('--output', type=Path, required=True, help='New private directory for logs, evidence and optional credentials')
    parser.add_argument('--keep', action='store_true', help='Keep successful isolated loopback services for manual acceptance')
    parser.add_argument('--skip-web-build', action='store_true', help='Use a previously built current ops-web')
    arguments = parser.parse_args()
    def interrupted(_signal, _frame):
        raise InterruptedError('acceptance run interrupted')
    signal.signal(signal.SIGTERM, interrupted)
    result = run(arguments)
    print(json.dumps({key: result[key] for key in ('status', 'api_url', 'ops_url', 'kept')}, ensure_ascii=False))
