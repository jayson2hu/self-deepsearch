import hashlib
import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import prepare_collector_batches as subject


class PrepareTests(unittest.TestCase):
    def test_archive_conversion_is_complete_deterministic_and_exclusive(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = '12345678-1234-1234-1234-123456789abc'
            a = subject.prepare(subject.ARCHIVE, root / 'a', source)
            b = subject.prepare(subject.ARCHIVE, root / 'b', source)
            self.assertEqual(a, b)
            self.assertEqual((a['records'], a['media_not_submitted'], len(a['batches'])), (4196, 130, 9))
            ids = set()
            for entry in a['batches']:
                raw = (root / 'a' / entry['file']).read_bytes()
                self.assertEqual(raw, (root / 'b' / entry['file']).read_bytes())
                self.assertEqual('sha256:' + hashlib.sha256(raw).hexdigest(), entry['request_hash'])
                body = json.loads(raw)
                for row in body['records']:
                    self.assertNotIn(row['external_id'], ids)
                    ids.add(row['external_id'])
                    self.assertNotIn('avatar', row['payload'])
            self.assertEqual(len(ids), 4196)
            with self.assertRaises(FileExistsError):
                subject.prepare(subject.ARCHIVE, root / 'a', source)
