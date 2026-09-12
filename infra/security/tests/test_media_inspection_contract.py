from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_daily_inspection_is_independent_and_disabled_locally() -> None:
    local = yaml.safe_load(read("compose.yaml"))["services"]["platform-worker-japan"]["environment"]
    japan = yaml.safe_load(read("infra/compose/japan/compose.yaml"))["services"]["platform-worker-japan"]["environment"]
    assert local["MEDIA_INSPECT_ENABLED"] == "${MEDIA_INSPECT_ENABLED:-false}"
    assert japan["MEDIA_INSPECT_ENABLED"] == "${MEDIA_INSPECT_ENABLED:-true}"
    assert japan["MEDIA_INSPECT_DISPLAY_ORIGIN"] == "${MEDIA_INSPECT_DISPLAY_ORIGIN:?set MEDIA_INSPECT_DISPLAY_ORIGIN}"
    main = read("services/platform-worker/cmd/worker/main.go")
    assert "if configuration.MediaInspectEnabled" in main
    assert "mediainspect.Runner" in main
    runner = read("services/platform-worker/internal/mediainspect/inspect.go")
    assert "Every = 24 * time.Hour" in runner
    assert "45*time.Second" in runner


def test_inspection_audit_and_operator_alerts_are_separate_from_reconciliation() -> None:
    migration = read("db/migrations/00018_media_inspection_audit.sql")
    assert "CREATE TABLE audit.media_inspection_runs" in migration
    assert "GRANT SELECT, INSERT, UPDATE ON audit.media_inspection_runs TO audit_writer" in migration
    assert "error_code IS NOT NULL" in migration
    assert "jsonb_array_length(default_results) = 2" in migration
    dashboard = read("apps/ops-web/app/page.tsx")
    assert "尚未完成检查，不代表图片正常" in dashboard
    alerts = read("infra/monitoring/alerts.yml")
    for metric in ("media_inspection_age_seconds", "media_publication_issues", "default_image_failures", "media_inspection_failures_24h"):
        assert f"self_deepsearch_{metric}" in alerts
    assert "PlatformMediaInspectionNeverCompleted" in alerts


def test_sql_contracts_are_guarded_and_run_serially_after_other_worker_contracts() -> None:
    workflow = yaml.safe_load(read(".github/workflows/ci.yml"))
    step = next(item for item in workflow["jobs"]["migrations"]["steps"] if "Run Worker lease" in item.get("name", ""))
    assert step["env"]["CONFIRM_MEDIA_INSPECT_CONTRACT"] == "disposable-database"
    assert step["run"].index("internal/mediareconcile") < step["run"].index("internal/mediainspect")
    assert "^TestPostgresInspection.*Contract$" in step["run"]
    schedule = read("services/platform-worker/internal/mediainspect/postgres_contract_test.go")
    snapshot = read("services/platform-worker/internal/mediainspect/snapshot_contract_test.go")
    for marker in ("validInspectionContractURL", "version != 21", "waiting == 2", "winners != 1 || notDue != 1"):
        assert marker in schedule
    for marker in ("SET LOCAL ROLE platform_worker_login", "ROLLBACK TO SAVEPOINT scenario", "rights-not-withdrawn", "dead-retirement"):
        assert marker in snapshot
