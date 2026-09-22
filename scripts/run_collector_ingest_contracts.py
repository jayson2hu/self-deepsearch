#!/usr/bin/env python3
"""Run collector contracts in a disposable PostgreSQL 16 container, then remove it."""
import argparse
import json
import os
import shutil
import subprocess
import tempfile
import uuid
from pathlib import Path

from prepare_collector_batches import ARCHIVE, prepare

ROOT = Path(__file__).resolve().parents[1]
SOURCE = '12345678-1234-1234-1234-123456789abc'


def run_contracts():
    go = shutil.which('go')
    if not go:
        raise RuntimeError('Go must be available on PATH')
    name = 'selfdeepsearch-ingest-contract-' + uuid.uuid4().hex[:12]
    env = os.environ.copy()
    # Do not inherit external database targets from another test workflow.
    for key in tuple(env):
        if key.endswith('TEST_DATABASE_URL') or key == 'DATABASE_URL':
            env.pop(key)

    def command(args, *, stdin=None):
        result = subprocess.run(args, input=stdin, text=True, cwd=ROOT, env=env,
                                capture_output=True, timeout=180, check=False)
        if result.returncode:
            raise RuntimeError(f'{args[0]} failed: {result.stdout}\n{result.stderr}')
        return result.stdout

    started = False
    try:
        command(['docker', 'run', '--rm', '-d', '--name', name, '--pull=never',
                 '-e', 'POSTGRES_PASSWORD=ingest-disposable-admin',
                 '-e', 'POSTGRES_DB=self_deepsearch_migrate_test',
                 '-p', '127.0.0.1::5432', 'postgres:16-alpine'])
        started = True
        command(['docker', 'exec', name, 'sh', '-c',
                 'for i in $(seq 1 30); do pg_isready -U postgres >/dev/null && exit 0; sleep 1; done; exit 1'])
        port = json.loads(command(['docker', 'inspect', '--format',
                                  '{{json .NetworkSettings.Ports}}', name]))['5432/tcp'][0]['HostPort']
        migrations = sorted((ROOT / 'db/migrations').glob('*.sql'))
        sql = '\n'.join(p.read_text().split('-- +goose Down')[0].replace('-- +goose Up', '') for p in migrations)
        sql += f"""
CREATE ROLE platform_api_login LOGIN PASSWORD 'ingest-disposable-test';
GRANT platform_api TO platform_api_login;
INSERT INTO collector.sources(source_id,name,source_type,base_url)
VALUES('{SOURCE}','ingest contract fixture','public_web','https://example.test');
INSERT INTO collector.sources(source_id,name,source_type,base_url,status,rights_status) VALUES
('12345678-1234-1234-1234-123456789abd','ingest paused','public_web','https://example.test','paused','needs_review'),
('12345678-1234-1234-1234-123456789abe','ingest blocked','public_web','https://example.test','blocked','needs_review'),
('12345678-1234-1234-1234-123456789abf','ingest disabled','public_web','https://example.test','disabled','needs_review'),
('12345678-1234-1234-1234-123456789ac0','ingest restricted','public_web','https://example.test','active','restricted'),
('12345678-1234-1234-1234-123456789ac1','ingest takedown','public_web','https://example.test','active','takedown'),
('12345678-1234-1234-1234-123456789ac2','ingest fixture source','fixture','https://example.test','active','needs_review'),
('12345678-1234-1234-1234-123456789ac3','ingest manual source','manual','https://example.test','active','needs_review'),
('12345678-1234-1234-1234-123456789ac4','ingest missing base','public_web',NULL,'active','needs_review'),
('12345678-1234-1234-1234-123456789ac5','ingest allowed source','public_web','https://example.test','active','allowed');
"""
        psql = ['docker', 'exec', '-i', name, 'psql', '-U', 'postgres', '-d',
                'self_deepsearch_migrate_test', '-v', 'ON_ERROR_STOP=1']
        command(psql, stdin=sql)
        env['COLLECTOR_TEST_DATABASE_URL'] = (
            f'postgres://platform_api_login:ingest-disposable-test@127.0.0.1:{port}/'
            'self_deepsearch_migrate_test?sslmode=disable')
        transaction = command([go, 'test', '-v', './services/platform-api/internal/database',
                               '-run', '^TestPostgresCollector(Ingest|SourcePolicy|Review)$', '-count=1'])
        command(psql, stdin=f"UPDATE collector.sources SET base_url='https://jable.tv' WHERE source_id='{SOURCE}';")
        with tempfile.TemporaryDirectory(prefix='collector-ingest-contract-') as directory:
            prepared = Path(directory) / 'batches'
            manifest = prepare(ARCHIVE, prepared, SOURCE)
            env['COLLECTOR_TEST_BATCH_DIR'] = str(prepared)
            archive = command([go, 'test', '-v', './services/platform-api/internal/database',
                               '-run', '^TestPostgresCollectorArchiveHTTP$', '-count=1'])
        return {'status': 'passed', 'postgres': '16-alpine', 'migrations': len(migrations),
                'archive': manifest, 'transaction_test': transaction, 'archive_http_test': archive,
                'boundary': 'disposable database only; review and production unchanged'}
    finally:
        if started:
            command(['docker', 'rm', '-f', name])


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--report', type=Path, required=True)
    args = parser.parse_args()
    # Never silently replace historical evidence.
    with args.report.open('x', encoding='utf-8') as report:
        json.dump(run_contracts(), report, ensure_ascii=False, indent=2)
        report.write('\n')
    print(f'Collector contracts passed; evidence: {args.report}')
