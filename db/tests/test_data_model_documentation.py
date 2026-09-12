from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MIGRATION_DIR = ROOT / "db" / "migrations"
DATA_MODEL = ROOT / "docs" / "DATA_MODEL.md"


class DataModelDocumentationTests(unittest.TestCase):
    def test_every_created_table_and_view_is_listed(self) -> None:
        migrations = "\n".join(
            path.read_text(encoding="utf-8")
            for path in sorted(MIGRATION_DIR.glob("*.sql"))
        )
        tables = set(
            re.findall(r"^CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_]+\.[a-z_]+)", migrations, re.MULTILINE)
        )
        views = set(
            re.findall(r"^CREATE(?: OR REPLACE)? VIEW\s+([a-z_]+\.[a-z_]+)", migrations, re.MULTILINE)
        )
        document = DATA_MODEL.read_text(encoding="utf-8")

        self.assertEqual(62, len(tables))
        self.assertEqual(5, len(views))
        missing = sorted(name for name in tables | views if f"`{name}`" not in document)
        self.assertEqual([], missing)
        self.assertIn("当前 Schema 版本为 21", document)


if __name__ == "__main__":
    unittest.main()
