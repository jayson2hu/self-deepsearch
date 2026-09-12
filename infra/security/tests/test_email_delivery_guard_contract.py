from __future__ import annotations

import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


class EmailDeliveryGuardContractTests(unittest.TestCase):
    def test_migration_has_atomic_quota_circuit_and_invitation_audit_contract(self) -> None:
        migration = (ROOT / "db" / "migrations" / "00015_email_delivery_guard.sql").read_text(encoding="utf-8")
        self.assertIn("CREATE TABLE platform.email_delivery_state", migration)
        self.assertIn("CREATE TABLE platform.email_delivery_daily", migration)
        self.assertIn("CHECK (sent_count + failed_count <= reserved_count)", migration)
        self.assertIn("'invitation_code'", migration)
        self.assertIn("UPDATE platform.system_metadata SET schema_version = 15", migration)

        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "email_delivery_store.go").read_text(encoding="utf-8")
        self.assertIn("reserved_count < $2", store)
        self.assertIn("FOR UPDATE", store)
        self.assertIn("'suppressed'", store)
        self.assertIn("SET delivery_status = 'sent'", store)
        self.assertIn("SET delivery_status = 'failed'", store)
        self.assertIn("consecutive_failures + 1 >= $1", store)
        self.assertNotIn("normalized_email", store)
        self.assertNotIn("recipient", store)
        self.assertNotIn("code_hash", store)

    def test_all_email_guard_settings_are_wired_to_runtime(self) -> None:
        config = (ROOT / "services" / "platform-api" / "internal" / "config" / "config.go").read_text(encoding="utf-8")
        main = (ROOT / "services" / "platform-api" / "cmd" / "api" / "main.go").read_text(encoding="utf-8")
        delivery = (ROOT / "services" / "platform-api" / "internal" / "identity" / "email_delivery.go").read_text(encoding="utf-8")
        for key in (
            "EMAIL_DAILY_LIMIT",
            "EMAIL_FAILURE_THRESHOLD",
            "EMAIL_CIRCUIT_COOLDOWN",
            "EMAIL_SEND_TIMEOUT",
        ):
            self.assertIn(key, config)
        self.assertIn("NewServiceWithEmailPolicy", main)
        self.assertIn("context.WithTimeout(ctx, service.emailPolicy.SendTimeout)", delivery)
        self.assertIn("context.WithoutCancel(ctx)", delivery)

    def test_prometheus_and_alerts_cover_quota_and_circuit(self) -> None:
        metrics = (ROOT / "services" / "platform-api" / "internal" / "httpapi" / "metrics.go").read_text(encoding="utf-8")
        for name in (
            "self_deepsearch_email_delivery_total",
            "self_deepsearch_email_daily_usage_ratio",
            "self_deepsearch_email_circuit_open",
        ):
            self.assertIn(name, metrics)

        alerts_path = ROOT / "infra" / "monitoring" / "alerts.yml"
        alerts = alerts_path.read_text(encoding="utf-8")
        yaml.safe_load(alerts)
        for alert in (
            "PlatformEmailQuotaHigh",
            "PlatformEmailQuotaExhausted",
            "PlatformEmailCircuitOpen",
        ):
            self.assertIn(alert, alerts)


if __name__ == "__main__":
    unittest.main()
