from __future__ import annotations

import re
import unittest
from pathlib import Path

MIGRATION_DIR = Path(__file__).resolve().parents[1] / "migrations"
BOOTSTRAP_DIR = Path(__file__).resolve().parents[1] / "bootstrap"
PRODUCTION_MIGRATOR = Path(__file__).resolve().parents[2] / "infra" / "database" / "migrate.sh"
MIGRATION_NAME = re.compile(r"^\d{5}_[a-z0-9_]+\.sql$")
UP_MARKER = "-- +goose Up"
DOWN_MARKER = "-- +goose Down"


def migrations() -> list[Path]:
    return sorted(MIGRATION_DIR.glob("*.sql"))


def sections(path: Path) -> tuple[str, str]:
    text = path.read_text(encoding="utf-8")
    before, marker, down = text.partition(DOWN_MARKER)
    if not marker:
        raise AssertionError(f"{path.name}: missing Down section")
    prefix, marker, up = before.partition(UP_MARKER)
    if not marker or prefix.strip():
        raise AssertionError(f"{path.name}: Up marker must be the first content")
    return up.strip(), down.strip()


class MigrationContractTests(unittest.TestCase):
    def test_production_migrator_is_locked_sequential_and_up_only(self) -> None:
        script = PRODUCTION_MIGRATOR.read_text(encoding="utf-8")
        self.assertIn("pg_advisory_lock", script)
        self.assertIn("migration sequence gap", script)
        self.assertIn("current_version", script)
        self.assertIn("printf '%s\\n' 'BEGIN;'", script)
        self.assertIn("printf '%s\\n'", script)
        self.assertIn("'COMMIT;'", script)
        self.assertIn(r"/^-- \+goose Up/ { in_up = 1; next }", script)
        self.assertIn(r"/^-- \+goose Down/ { in_up = 0; next }", script)
        self.assertIn("chmod 600", script)
        self.assertNotIn("current_section == requested_section", script)

    def test_names_and_sections_are_deterministic(self) -> None:
        files = migrations()
        self.assertTrue(files, "at least one migration is required")
        self.assertEqual(len(files), len({path.name[:5] for path in files}))

        for path in files:
            with self.subTest(path=path.name):
                self.assertRegex(path.name, MIGRATION_NAME)
                text = path.read_text(encoding="utf-8")
                self.assertEqual(text.count(UP_MARKER), 1)
                self.assertEqual(text.count(DOWN_MARKER), 1)
                up, down = sections(path)
                self.assertTrue(up)
                self.assertTrue(down)

    def test_schema_version_tracks_every_migration(self) -> None:
        for expected_version, path in enumerate(migrations(), start=1):
            with self.subTest(path=path.name):
                up, down = sections(path)
                if expected_version == 1:
                    self.assertIn("schema_version integer", up)
                else:
                    self.assertIn(
                        f"UPDATE platform.system_metadata SET schema_version = {expected_version}",
                        up,
                    )
                    self.assertIn(
                        f"UPDATE platform.system_metadata SET schema_version = {expected_version - 1}",
                        down,
                    )

    def test_required_publication_boundaries_exist(self) -> None:
        combined_up = "\n".join(sections(path)[0] for path in migrations())
        required = (
            "CREATE SCHEMA IF NOT EXISTS collector",
            "CREATE SCHEMA IF NOT EXISTS platform",
            "CREATE SCHEMA IF NOT EXISTS audit",
            "CREATE VIEW platform.public_published_works",
            "CREATE VIEW platform.public_published_performers",
            "CREATE VIEW platform.public_search_documents",
            "CREATE VIEW platform.public_entity_media",
            "CREATE TRIGGER publications_validate_revision",
            "CREATE TRIGGER content_revisions_protect_payload",
            "CREATE TRIGGER audit_logs_append_only",
            "GRANT SELECT ON platform.public_published_works",
        )
        for statement in required:
            with self.subTest(statement=statement):
                self.assertIn(statement, combined_up)

    def test_no_destructive_cascade_or_public_table_grants(self) -> None:
        combined = "\n".join(path.read_text(encoding="utf-8") for path in migrations())
        self.assertNotRegex(combined, r"(?i)DROP\s+(?:TABLE|SCHEMA).+CASCADE")
        self.assertNotRegex(combined, r"(?i)GRANT\s+.+ON\s+ALL\s+TABLES.+TO\s+PUBLIC")

    def test_verified_backups_require_copy_and_restore_evidence(self) -> None:
        up, _ = sections(MIGRATION_DIR / "00008_backup_evidence_constraints.sql")
        self.assertIn("backup_runs_completed_evidence_check", up)
        self.assertIn("backup_runs_verified_evidence_check", up)
        self.assertIn("copied_at IS NOT NULL AND restore_verified_at IS NOT NULL", up)
        self.assertIn("restore_verified_at IS NULL OR copied_at IS NOT NULL", up)

    def test_backup_retention_evidence_is_wired_into_postgres_roundtrip(self) -> None:
        tests_dir = MIGRATION_DIR.parent / "tests"
        roundtrip = (tests_dir / "postgres_roundtrip.sh").read_text(encoding="utf-8")
        contract = (tests_dir / "backup_retention_contracts.sh").read_text(encoding="utf-8")
        self.assertIn("CONFIRM_BACKUP_RETENTION_CONTRACTS=disposable-database", roundtrip)
        self.assertIn("sh db/tests/backup_retention_contracts.sh", roundtrip)
        self.assertLess(roundtrip.index("backup_retention_contracts.sh"), roundtrip.index('apply_section "${migration}" down'))
        for boundary in (
            'expect_evidence verified "$reference" "$hash" "$bytes"',
            'expect_evidence unverified "$reference.changed" "$hash" "$bytes"',
            'expect_evidence unverified "$reference" "$other_hash" "$bytes"',
            'expect_evidence unverified "$reference" "$hash" 43',
            "copy_sha256 copy_byte_size restored_sha256",
            "daily:japan:beijing weekly:beijing:japan",
            "--no-psqlrc",
        ):
            self.assertIn(boundary, contract)

    def test_media_master_and_public_derivatives_have_distinct_storage_scopes(self) -> None:
        up, down = sections(MIGRATION_DIR / "00009_media_storage_scope.sql")
        self.assertIn("media_objects_storage_scope_check", up)
        self.assertIn("media_objects_rendition_scope_check", up)
        self.assertIn("rendition IN ('quarantine', 'master') AND storage_scope = 'private'", up)
        self.assertIn("rendition IN ('w320', 'w640', 'w960') AND storage_scope = 'public'", up)
        self.assertIn("DROP COLUMN storage_scope", down)

    def test_media_reconciliation_results_are_audited(self) -> None:
        up, down = sections(MIGRATION_DIR / "00010_media_reconciliation_audit.sql")
        self.assertIn("CREATE TABLE audit.media_reconciliation_runs", up)
        self.assertIn("run_status IN ('running', 'completed', 'failed')", up)
        self.assertIn("expected_count BETWEEN 0 AND 10000", up)
        self.assertIn("GRANT SELECT, INSERT, UPDATE ON audit.media_reconciliation_runs TO audit_writer", up)
        self.assertIn("DROP TABLE IF EXISTS audit.media_reconciliation_runs", down)

    def test_editorial_recommendations_are_audited_and_publication_gated(self) -> None:
        up, down = sections(MIGRATION_DIR / "00011_editorial_recommendations.sql")
        self.assertIn("CREATE TABLE platform.editorial_recommendations", up)
        self.assertIn("status IN ('active', 'paused', 'removed')", up)
        self.assertIn("editorial_recommendations_public_idx", up)
        self.assertIn("GRANT SELECT, INSERT, UPDATE ON platform.editorial_recommendations TO platform_api", up)
        self.assertIn("DROP TABLE IF EXISTS platform.editorial_recommendations", down)

    def test_entity_merges_preserve_mapping_and_redirect(self) -> None:
        up, down = sections(MIGRATION_DIR / "00012_entity_merges.sql")
        self.assertIn("CREATE TABLE platform.entity_merges", up)
        self.assertIn("redirect_id uuid NOT NULL UNIQUE REFERENCES platform.redirects", up)
        self.assertIn("UNIQUE (entity_type, source_entity_id)", up)
        self.assertIn("source_entity_id <> target_entity_id", up)
        self.assertIn("GRANT DELETE ON platform.work_performers, platform.search_documents, platform.content_metrics_hourly TO platform_api", up)
        self.assertIn("REVOKE DELETE ON platform.work_performers, platform.search_documents, platform.content_metrics_hourly FROM platform_api", down)
        self.assertIn("DROP TABLE IF EXISTS platform.entity_merges", down)

    def test_user_invitations_are_single_use_and_role_scoped(self) -> None:
        up, down = sections(MIGRATION_DIR / "00013_user_invitations.sql")
        self.assertIn("CREATE TABLE platform.user_invitations", up)
        self.assertIn("role IN ('admin', 'editor', 'user')", up)
        self.assertIn("attempt_count BETWEEN 0 AND 5", up)
        self.assertIn("status IN ('pending', 'accepted', 'revoked', 'expired')", up)
        self.assertIn("user_invitations_pending_email_uq", up)
        self.assertIn("WHERE status = 'pending'", up)
        self.assertIn("GRANT SELECT, INSERT, UPDATE ON platform.user_invitations TO platform_api", up)
        self.assertIn("DROP TABLE IF EXISTS platform.user_invitations", down)
        self.assertIn("security_rate_limits_action_check", up)

    def test_bootstrap_provisions_runtime_logins_over_init_socket(self) -> None:
        script = (BOOTSTRAP_DIR / "003_create_runtime_logins.sh").read_text(encoding="utf-8")
        self.assertIn("host=/var/run/postgresql", script)
        self.assertNotIn("@127.0.0.1:5432", script)
        self.assertIn("exec sh /database-tools/provision_runtime_logins.sh", script)

        database_root = MIGRATION_DIR.parent
        seed = database_root / "seeds" / "development.sql"
        seed_source = seed.read_text(encoding="utf-8")
        bootstrap_seed = (BOOTSTRAP_DIR / "002_seed_development.sh").read_text(encoding="utf-8")
        fixture_loader = (database_root.parent / "infra" / "database" / "load_release_a_fixtures.sh").read_text(encoding="utf-8")
        self.assertTrue(seed_source.startswith("\\if :{?release_a_fixture}"))
        self.assertIn("release_a_fixture' = '1'", seed_source)
        self.assertIn("SET LOCAL lock_timeout = '5s'", seed_source)
        self.assertIn("IN SHARE ROW EXCLUSIVE MODE", seed_source)
        self.assertIn("current_schema_version IS DISTINCT FROM 21", seed_source)
        self.assertIn("fixtures require schema version 21", seed_source)
        self.assertIn("empty or fixture-only catalog database", seed_source)
        self.assertIn("account_status <> 'closed'", seed_source)
        self.assertIn("'owner', 'closed', now()", seed_source)
        self.assertIn("--set release_a_fixture=1", bootstrap_seed)
        self.assertIn("--no-psqlrc", bootstrap_seed)
        self.assertIn('ALLOW_SYNTHETIC_FIXTURES:-false', fixture_loader)
        self.assertIn('CONFIRM_SYNTHETIC_FIXTURES:-', fixture_loader)
        self.assertIn("release-a-test-only", fixture_loader)
        self.assertIn("--no-psqlrc", fixture_loader)
        self.assertIn("--single-transaction", fixture_loader)
        self.assertIn("--set release_a_fixture=1", fixture_loader)

        roundtrip = (database_root / "tests" / "postgres_roundtrip.sh").read_text(encoding="utf-8")
        self.assertIn("load_release_a_fixtures", roundtrip)
        self.assertIn("--set release_a_fixture=0", roundtrip)
        self.assertIn("roundtrip real-source sentinel", roundtrip)

    def test_performer_view_rollback_rebuilds_view_before_dropping_columns(self) -> None:
        _, down = sections(MIGRATION_DIR / "00005_public_performer_fields.sql")
        self.assertIn("DROP VIEW platform.public_published_performers", down)
        self.assertIn("CREATE VIEW platform.public_published_performers", down)
        self.assertNotIn("CREATE OR REPLACE VIEW platform.public_published_performers", down)
        self.assertIn(
            "GRANT SELECT ON platform.public_published_performers TO public_reader",
            down,
        )

    def test_role_teardown_revokes_schema_privileges(self) -> None:
        down = sections(MIGRATION_DIR / "00002_collector_catalog.sql")[1]
        self.assertEqual(down.count("REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA"), 3)
        self.assertEqual(down.count("REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA"), 3)
        for role in (
            "audit_writer",
            "public_reader",
            "collector_ingest",
            "platform_worker",
            "platform_api",
        ):
            with self.subTest(role=role):
                revoke = re.search(rf"REVOKE ALL ON SCHEMA .+ FROM [^;]*\b{role}\b", down)
                drop = down.find(f"DROP ROLE IF EXISTS {role}")
                self.assertIsNotNone(revoke, f"{role} schema privileges must be revoked")
                self.assertGreater(drop, revoke.start(), f"{role} must be revoked before drop")


if __name__ == "__main__":
    unittest.main()
