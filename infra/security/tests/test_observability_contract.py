from __future__ import annotations

import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


class ObservabilityContractTests(unittest.TestCase):
    def test_http_duration_is_a_bounded_histogram(self) -> None:
        metrics = (
            ROOT / "services" / "platform-api" / "internal" / "httpapi" / "metrics.go"
        ).read_text(encoding="utf-8")

        self.assertIn("# TYPE self_deepsearch_http_request_duration_seconds histogram", metrics)
        self.assertIn("self_deepsearch_http_request_duration_seconds_bucket", metrics)
        self.assertIn('return "OTHER"', metrics)
        for boundary in (
            "10 * time.Millisecond",
            "300 * time.Millisecond",
            "500 * time.Millisecond",
            "5 * time.Second",
        ):
            self.assertIn(boundary, metrics)

    def test_latency_alerts_use_p95_thresholds_and_minimum_traffic(self) -> None:
        document = yaml.safe_load(
            (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        )
        rules = {
            rule["alert"]: rule
            for group in document["groups"]
            for rule in group["rules"]
            if "alert" in rule
        }

        public = rules["PlatformPublicAPIP95LatencyHigh"]
        public_expression = public["expr"]
        self.assertIn("histogram_quantile", public_expression)
        self.assertIn("self_deepsearch_http_request_duration_seconds_bucket", public_expression)
        self.assertIn(">= 0.3", public_expression)
        self.assertIn(">= 20", public_expression)
        self.assertIn("/api/v1/(site/.*|works(/.*)?|performers(/.*)?|studios(/.*)?)", public_expression)
        self.assertEqual(public["for"], "10m")

        search = rules["PlatformSearchP95LatencyHigh"]
        search_expression = search["expr"]
        self.assertIn("histogram_quantile", search_expression)
        self.assertIn('route="/api/v1/search/works"', search_expression)
        self.assertIn(">= 0.5", search_expression)
        self.assertIn(">= 10", search_expression)
        self.assertEqual(search["for"], "10m")

    def test_host_saturation_alerts_have_actionable_thresholds(self) -> None:
        document = yaml.safe_load(
            (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        )
        rules = {
            rule["alert"]: rule
            for group in document["groups"]
            for rule in group["rules"]
            if "alert" in rule
        }

        self.assertEqual(rules["PlatformNodeExporterDown"]["labels"]["severity"], "warning")
        self.assertIn(">= 0.8", rules["PlatformHostDiskUsageHigh"]["expr"])
        self.assertIn("< 0.9", rules["PlatformHostDiskUsageHigh"]["expr"])
        self.assertEqual(rules["PlatformHostDiskUsageCritical"]["labels"]["severity"], "critical")
        self.assertIn(">= 0.9", rules["PlatformHostDiskUsageCritical"]["expr"])
        self.assertIn("node_memory_MemAvailable_bytes", rules["PlatformHostMemoryPressure"]["expr"])
        self.assertIn(">= 0.8", rules["PlatformHostMemoryPressure"]["expr"])
        self.assertIn('mode="idle"', rules["PlatformHostCPUSaturation"]["expr"])
        self.assertIn(">= 0.85", rules["PlatformHostCPUSaturation"]["expr"])

    def test_database_pool_metrics_and_saturation_alert_are_wired(self) -> None:
        metrics = (
            ROOT / "services" / "platform-api" / "internal" / "httpapi" / "metrics.go"
        ).read_text(encoding="utf-8")
        store = (
            ROOT / "services" / "platform-api" / "internal" / "database" / "store.go"
        ).read_text(encoding="utf-8")
        for name in (
            "self_deepsearch_database_pool_connections",
            "self_deepsearch_database_pool_max_connections",
            "self_deepsearch_database_pool_utilization_ratio",
            "self_deepsearch_database_pool_empty_acquire_total",
            "self_deepsearch_database_pool_canceled_acquire_total",
        ):
            self.assertIn(name, metrics)
        self.assertIn("stats.AcquiredConns()", store)
        self.assertIn("stats.MaxConns()", store)

        document = yaml.safe_load(
            (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        )
        rule = next(
            rule
            for group in document["groups"]
            for rule in group["rules"]
            if rule.get("alert") == "PlatformDatabasePoolUtilizationHigh"
        )
        self.assertEqual(rule["expr"], "self_deepsearch_database_pool_utilization_ratio >= 0.8")
        self.assertEqual(rule["for"], "10m")

    def test_worker_runtime_heartbeat_and_lease_metrics_are_wired(self) -> None:
        health = (
            ROOT / "services" / "platform-worker" / "internal" / "health" / "handler.go"
        ).read_text(encoding="utf-8")
        dispatcher = (
            ROOT / "services" / "platform-worker" / "internal" / "outbox" / "outbox.go"
        ).read_text(encoding="utf-8")
        main = (
            ROOT / "services" / "platform-worker" / "cmd" / "worker" / "main.go"
        ).read_text(encoding="utf-8")
        for name in (
            "self_deepsearch_worker_last_heartbeat_age_seconds",
            "self_deepsearch_worker_heartbeat_max_age_seconds",
            "self_deepsearch_worker_last_poll_success_age_seconds",
            "self_deepsearch_worker_poll_failures_total",
            "self_deepsearch_worker_consecutive_poll_failures",
            "self_deepsearch_worker_last_success_age_seconds",
            "self_deepsearch_worker_active_leases",
            "self_deepsearch_worker_events_total",
        ):
            self.assertIn(name, health)
        self.assertIn("dispatcher.Telemetry.Heartbeat", dispatcher)
        self.assertIn("dispatcher.Telemetry.SetActiveLeases", dispatcher)
        self.assertIn("dispatcher.Telemetry.PollSucceeded", dispatcher)
        self.assertIn("dispatcher.Telemetry.PollFailed", dispatcher)
        self.assertRegex(main, r'RequireTelemetry:\s+configuration\.Region == "japan"')

        document = yaml.safe_load(
            (ROOT / "infra" / "monitoring" / "alerts.yml").read_text(encoding="utf-8")
        )
        rule = next(
            rule
            for group in document["groups"]
            for rule in group["rules"]
            if rule.get("alert") == "PlatformWorkerHeartbeatStale"
        )
        self.assertIn("self_deepsearch_worker_last_heartbeat_age_seconds", rule["expr"])
        self.assertIn("self_deepsearch_worker_heartbeat_max_age_seconds", rule["expr"])
        self.assertEqual(rule["labels"]["severity"], "critical")

        poll_failure_rule = next(
            rule
            for group in document["groups"]
            for rule in group["rules"]
            if rule.get("alert") == "PlatformWorkerPollFailures"
        )
        self.assertEqual(
            poll_failure_rule["expr"],
            "self_deepsearch_worker_consecutive_poll_failures >= 3",
        )
        self.assertEqual(poll_failure_rule["for"], "2m")
        self.assertEqual(poll_failure_rule["labels"]["severity"], "critical")


if __name__ == "__main__":
    unittest.main()
