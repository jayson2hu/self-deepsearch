#!/usr/bin/env python3
"""Prepare deterministic v1 metadata requests from the verified archive; never send them."""
import argparse
import hashlib
import json
import uuid
from pathlib import Path

from collector_integration import ARCHIVE, load_archive


def prepare(archive, output, source_id):
    if str(uuid.UUID(source_id)) != source_id:
        raise ValueError('source_id must be a canonical UUID registered by the platform')
    people, media, report = load_archive(archive)
    # Exclusive directory creation prevents overwriting a previous prepared batch.
    output.mkdir(parents=True, exist_ok=False)
    receipts = []
    for start in range(0, len(people), 500):
        records = [{
            'external_id': p['external_id'], 'checked_at': p['checked_at'],
            'payload': {'name': p['name'], 'aliases': p.get('aliases', []),
                        'profile_url': p['profile_url'],
                        'source_content_hash': p['current_content_hash']},
        } for p in people[start:start + 500]]
        batch = {'schema_version': 'collector.performers.v1', 'source_id': source_id,
                 'region': 'japan', 'connector_version': 'jable-archive@2026-09-19.1',
                 'idempotency_key': f"archive-{report['manifest_sha256']}-{start // 500 + 1}",
                 'records': records}
        raw = json.dumps(batch, ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode()
        name = f'batch-{start // 500 + 1:03d}.json'
        (output / name).write_bytes(raw)
        receipts.append({'file': name, 'records': len(records), 'bytes': len(raw),
                         'request_hash': 'sha256:' + hashlib.sha256(raw).hexdigest()})
    result = {'mode': 'prepared-only', 'source_id': source_id, 'archive': report,
              'records': len(people), 'media_not_submitted': len(media), 'batches': receipts}
    (output / 'manifest.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return result


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--archive', type=Path, default=ARCHIVE)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--source-id', required=True)
    args = parser.parse_args()
    print(json.dumps(prepare(args.archive, args.output, args.source_id), ensure_ascii=False, indent=2))
