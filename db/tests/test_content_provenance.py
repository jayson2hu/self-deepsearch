import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


class ContentProvenanceMigrationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.migration = (ROOT / "db" / "migrations" / "00014_content_provenance.sql").read_text(encoding="utf-8")

    def test_content_provenance_is_revision_scoped_and_append_only(self) -> None:
        self.assertIn("FOREIGN KEY (revision_id) REFERENCES platform.content_revisions", self.migration)
        self.assertIn("provenance revision does not match entity", self.migration)
        self.assertIn("field_provenance_append_only", self.migration)
        self.assertIn("field provenance is append-only", self.migration)

    def test_migration_backfills_without_inventing_web_urls(self) -> None:
        self.assertIn("legacy_record", self.migration)
        self.assertIn("Release A provenance migration v14", self.migration)
        self.assertNotIn("https://", self.migration)
        self.assertIn("UPDATE platform.system_metadata SET schema_version = 14", self.migration)
        self.assertIn("UPDATE platform.system_metadata SET schema_version = 13", self.migration)


if __name__ == "__main__":
    unittest.main()
