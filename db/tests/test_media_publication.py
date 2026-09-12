from __future__ import annotations

import re
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
MIGRATIONS = ROOT / "db/migrations"


class MediaPublicationContracts(unittest.TestCase):
    def test_public_media_requires_both_media_and_parent_visibility(self) -> None:
        up = (MIGRATIONS / "00017_public_media_parent_visibility.sql").read_text(encoding="utf-8").split("-- +goose Down")[0]
        for condition in (
            "security_barrier = true",
            "asset.asset_status = 'published'",
            "asset.rights_status = 'allowed'",
            "object.version = asset.current_version",
            "object.object_status = 'published'",
            "object.public_url IS NOT NULL",
            "link.publication_status = 'published'",
            "link.entity_type = 'work' AND EXISTS",
            "FROM platform.public_published_works AS parent",
            "parent.work_id = link.entity_id",
            "link.entity_type = 'performer' AND EXISTS",
            "FROM platform.public_published_performers AS parent",
            "parent.performer_id = link.entity_id",
        ):
            self.assertIn(condition, up)

    def test_view_upgrade_preserves_columns_and_explicitly_restores_old_down_contract(self) -> None:
        original = (MIGRATIONS / "00004_media_audit_permissions.sql").read_text(encoding="utf-8")
        migration = (MIGRATIONS / "00017_public_media_parent_visibility.sql").read_text(encoding="utf-8")
        up, down = migration.split("-- +goose Down")
        old_query = original.split("CREATE VIEW platform.public_entity_media AS", 1)[1].split(";", 1)[0]
        up_query = up.split(" AS\nSELECT", 1)[1].split("FROM platform.entity_media", 1)[0]
        old_columns = old_query.split("SELECT", 1)[1].split("FROM platform.entity_media", 1)[0]
        self.assertEqual(up_query.split(), old_columns.split())
        down_query = down.split("CREATE OR REPLACE VIEW platform.public_entity_media AS", 1)[1].split(";", 1)[0]
        self.assertEqual(old_query.split(), down_query.split())
        self.assertIn("RESET (security_barrier)", down)
        self.assertNotRegex(migration, r"(?i)DROP\s+(TABLE|VIEW)|GRANT\s+.+ON\s+platform\.media_objects")

    def test_latest_schema_gate_is_synchronized_across_runtime_seed_ci_and_monitoring(self) -> None:
        latest = len(list(MIGRATIONS.glob("*.sql")))
        expected = {
            "services/platform-api/internal/httpapi/handlers.go": f"requiredDatabaseSchemaVersion = {latest}",
            "services/platform-api/internal/database/invitation_store_test.go": f"schemaVersion != {latest}",
            "services/platform-api/internal/database/identity_store_contract_test.go": f"version != {latest}",
            "services/platform-worker/internal/outbox/postgres_contract_test.go": f"version != {latest}",
            "db/seeds/development.sql": f"current_schema_version IS DISTINCT FROM {latest}",
            "db/tests/postgres_contracts.sql": f"schema_version <> {latest}",
            ".github/workflows/ci.yml": f"'schema_version={latest}' migration-rerun.txt",
            "infra/monitoring/alerts.yml": f"self_deepsearch_schema_version != {latest}",
        }
        for path, marker in expected.items():
            with self.subTest(path=path):
                self.assertIn(marker, (ROOT / path).read_text(encoding="utf-8"))

    def test_real_public_reader_state_matrix_runs_before_roundtrip_rollback(self) -> None:
        contract = (ROOT / "db/tests/media_visibility_contracts.sql").read_text(encoding="utf-8")
        roundtrip = (ROOT / "db/tests/postgres_roundtrip.sh").read_text(encoding="utf-8")
        self.assertIn("('work'), ('performer')", contract)
        for scenario in (
            "published-main", "draft", "reviewing", "approved", "hidden", "takedown", "merged",
            "missing-publication", "hidden-publication", "takedown-publication", "secondary-site",
        ):
            self.assertIn(f"'{scenario}'", contract)
        self.assertEqual(len(re.findall(r"^SET LOCAL ROLE public_reader;", contract, re.MULTILINE)), 3)
        self.assertIn("total <> 2 OR leaked <> 0", contract)
        self.assertIn("republishing did not restore prepared public media", contract)
        self.assertIn("ROLLBACK;", contract)
        self.assertIn("--no-psqlrc", roundtrip)
        self.assertLess(roundtrip.index("media_visibility_contracts.sql"), roundtrip.index('apply_section "${migration}" down'))
