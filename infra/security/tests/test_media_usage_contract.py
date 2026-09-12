from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_usage_observer_is_disabled_and_credentials_only_reach_japan_worker() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml", "infra/compose/beijing/compose.yaml"):
        services = yaml.safe_load(read(path))["services"]
        for name, service in services.items():
            env = service.get("environment", {})
            if name == "platform-worker-japan":
                assert env["MEDIA_USAGE_MONITOR_MODE"] == "${MEDIA_USAGE_MONITOR_MODE:-off}"
                assert env["MEDIA_USAGE_INTERVAL"] == "${MEDIA_USAGE_INTERVAL:-15m}"
                assert env["R2_ANALYTICS_API_TOKEN"] == "${R2_ANALYTICS_API_TOKEN:-}"
                assert env["MEDIA_USAGE_PERIOD_START"] == "${MEDIA_USAGE_PERIOD_START:-}"
                assert env["MEDIA_USAGE_PERIOD_END"] == "${MEDIA_USAGE_PERIOD_END:-}"
            else:
                assert "R2_ANALYTICS_API_TOKEN" not in env
                assert "R2_ANALYTICS_ACCOUNT_ID" not in env
                assert "MEDIA_USAGE_MONITOR_MODE" not in env
    main = read("services/platform-worker/cmd/worker/main.go")
    assert 'configuration.MediaUsageMode == "observe"' in main
    assert "mediausage.NewPostgresRepository" in main
    assert "mediausage.Runner" in main
    assert "UsageMetrics:" in main


def test_usage_schema_preserves_private_immutable_evidence() -> None:
    source = read("db/migrations/00019_media_usage_observation.sql")
    for marker in (
        "CREATE TABLE platform.media_usage_state",
        "CREATE TABLE audit.media_usage_runs",
        "NEW.high_class_a < OLD.high_class_a",
        "OLD.stop_recommended AND NOT NEW.stop_recommended",
        "NEW.config_digest IS DISTINCT FROM OLD.config_digest",
        "NEW.policy_snapshot IS DISTINCT FROM OLD.policy_snapshot",
        "OLD.run_status <> 'running'",
        "CREATE UNIQUE INDEX media_usage_one_running_idx",
        "error_code IS NOT NULL",
        "GRANT SELECT, INSERT, UPDATE ON platform.media_usage_state, audit.media_usage_runs TO platform_worker",
        "GRANT SELECT ON platform.media_usage_state, audit.media_usage_runs TO platform_api",
        "UPDATE platform.system_metadata SET schema_version = 19",
    ):
        assert marker in source
    assert "TO public_reader" not in source
    assert "TO audit_writer" not in source
    assert "GRANT DELETE" not in source
    assert "API_TOKEN" not in source


def test_usage_sql_contract_is_guarded_and_serially_wired() -> None:
    workflow = yaml.safe_load(read(".github/workflows/ci.yml"))
    step = next(item for item in workflow["jobs"]["migrations"]["steps"] if "Run Worker lease" in item.get("name", ""))
    assert step["env"]["CONFIRM_MEDIA_USAGE_CONTRACT"] == "disposable-database"
    assert step["env"]["MEDIA_USAGE_CONTRACT_DATABASE_URL"] == step["env"]["WORKER_CONTRACT_DATABASE_URL"]
    assert step["env"]["MEDIA_USAGE_CONTRACT_ADMIN_DATABASE_URL"] == step["env"]["WORKER_CONTRACT_ADMIN_DATABASE_URL"]
    assert step["run"].index("internal/mediainspect") < step["run"].index("internal/mediausage")
    contract = read("services/platform-worker/internal/mediausage/postgres_contract_test.go")
    for marker in ("validUsageContractURL", "version != 21", "waiting == 2", "winners != 1 || notDue != 1", "SET LOCAL ROLE", "usage_interrupted"):
        assert marker in contract


def test_usage_metrics_are_recommendations_not_billing_or_delivery_state() -> None:
    metrics = read("services/platform-worker/internal/mediausage/metrics.go")
    assert "not an executed stop" in metrics
    assert "not billing" in metrics
    assert 'gauge("media_default_only"' not in metrics
    alerts = read("infra/monitoring/alerts.yml")
    for marker in ("PlatformMediaUsageStateUnavailable", "PlatformMediaUsageReviewRequired", "PlatformMediaUsageStopRecommended", "NOT confirmed executed"):
        assert marker in alerts
