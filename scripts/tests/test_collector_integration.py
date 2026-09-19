"""Regression coverage for the bounded integration tool (no real DB writes)."""
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch, Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import collector_integration as subject


class IntegrationTests(unittest.TestCase):
    def test_pinned_archive_and_relations(self):
        people, media, report = subject.load_archive(subject.ARCHIVE)
        self.assertEqual((len(people), len(media), report['files_verified']), (4196, 130, 804))
        self.assertEqual(sum('avatar' not in p for p in people), 4066)

    def test_unrecognized_manifest_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'archive-manifest.json').write_text('{}')
            with self.assertRaisesRegex(ValueError, 'unrecognized'):
                subject.load_archive(root)

    def test_missing_and_escaping_paths_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for path in ('../outside', '/etc/passwd', 'missing.jpg'):
                with self.subTest(path=path), self.assertRaises(ValueError):
                    subject.safe_path(root, path)
            (root / 'escape').symlink_to('/etc/passwd')
            with self.assertRaises(ValueError):
                subject.safe_path(root, 'escape')

    def test_sql_literal_contains_only_hex_data(self):
        value = "'\\; DROP TABLE x;--日本語%_"
        self.assertNotIn(value, subject.literal(value))
        self.assertIn(value.encode().hex(), subject.literal(value))

    def test_missing_write_confirmation(self):
        with patch.object(subject, 'load_archive') as archive:
            with self.assertRaises(ValueError):
                subject.import_archive(subject.ARCHIVE, '')
            archive.assert_not_called()

    def test_invalid_pagination_and_filter(self):
        for options in ({'page': 0}, {'page': 10001}, {'query': 'a'*201}, {'avatar': 'bad'}, {'performer_id': "' OR 1=1"}):
            with self.subTest(options=options), self.assertRaises(ValueError):
                subject.read_people(**options)

    def test_stage_name_query_uses_stored_aliases_and_literal_input(self):
        with patch.object(subject, 'postgres', return_value={}) as query:
            subject.read_people(query="  星_%'  ")
        sql = query.call_args.args[0]
        self.assertIn(subject.literal("星_%'"), sql)
        self.assertIn("jsonb_typeof(payload->'aliases')='array'", sql)
        self.assertIn("jsonb_typeof(alias.value)='string'", sql)
        self.assertIn("payload->>'name_original'", sql)
        self.assertIn("payload->>'romanized_name'", sql)
        self.assertNotIn("星_%'", sql)

    def test_missing_cookie_does_not_call_auth(self):
        with patch.object(subject, 'build_opener') as opener:
            self.assertFalse(subject.operator_allowed(''))
            self.assertFalse(subject.operator_allowed('sd_session=' + 'a'*4097))
            opener.assert_not_called()

    def test_auth_role_matrix_and_disabled_account(self):
        for role, status, expected in [('owner','active',True),('admin','active',True),('editor','active',True),('user','active',False),('owner','disabled',False),('unexpected','active',False)]:
            with self.subTest(role=role,status=status):
                response = io.BytesIO(json.dumps({'user':{'role':role,'account_status':status}}).encode())
                opener = Mock()
                opener.open.return_value.__enter__ = Mock(return_value=response)
                opener.open.return_value.__exit__ = Mock(return_value=False)
                with patch.object(subject, 'build_opener', return_value=opener):
                    self.assertEqual(subject.operator_allowed('sd_session=test'), expected)

    def test_auth_outage_fails_closed(self):
        with patch.object(subject, 'build_opener', side_effect=TimeoutError):
            self.assertFalse(subject.operator_allowed('sd_session=test'))

    def test_read_only_db_and_errors_redacted(self):
        with patch.dict(subject.os.environ, {'INTEGRATION_DB_PASSWORD':'secret'}), patch.object(subject.subprocess, 'run') as run:
            run.return_value = Mock(returncode=0, stdout='{}')
            subject.postgres('SELECT 1;')
            self.assertTrue(run.call_args.kwargs['input'].startswith('BEGIN READ ONLY;'))
            self.assertNotIn('secret', str(run.call_args.args))
            run.return_value = Mock(returncode=1, stdout='', stderr='sensitive SQL')
            with self.assertRaisesRegex(ValueError, '^PostgreSQL operation failed;'):
                subject.postgres('SELECT 1;')


if __name__ == '__main__':
    unittest.main()
