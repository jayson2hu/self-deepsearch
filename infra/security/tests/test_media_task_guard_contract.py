from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_scheduled_scan_guard_is_opt_in_and_only_injected_into_japan_worker() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml", "infra/compose/beijing/compose.yaml"):
        services = yaml.safe_load(read(path))["services"]
        for name, service in services.items():
            env = service.get("environment", {})
            if name == "platform-worker-japan":
                assert env["MEDIA_RECONCILE_USAGE_GUARD"] == "${MEDIA_RECONCILE_USAGE_GUARD:-off}"
            else:
                assert "MEDIA_RECONCILE_USAGE_GUARD" not in env
    for path in (".env.example", "infra/compose/japan/.env.example"):
        assert "MEDIA_RECONCILE_USAGE_GUARD=off" in read(path).splitlines()
    main = read("services/platform-worker/cmd/worker/main.go")
    assert 'configuration.MediaReconcileUsageGuard == "enforce"' in main
    assert "runner.Admission = reconcileAdmission" in main
    assert "ReconcileGuardMetrics:" in main
    for path in ("services/platform-worker/internal/outbox/outbox.go", "services/platform-worker/internal/mediainspect/inspect.go"):
        # Protection must not starve rights deletion or fixed default checks.
        assert "TaskGuard" not in read(path)


def test_admission_is_rechecked_and_never_forges_completion_or_usage_recovery() -> None:
    runner = read("services/platform-worker/internal/mediareconcile/reconcile.go")
    assert runner.count("runner.Admission.Allow(ctx)") == 2
    assert runner.index("runner.Admission.Allow(ctx)") < runner.index("runner.Repository.Prepare")
    assert runner.rindex("runner.Admission.Allow(ctx)") < runner.index("runner.Client.Reconcile")
    sql = read("services/platform-worker/internal/mediareconcile/postgres.go")
    assert "AND NOT (run_status = 'failed' AND error_code = 'usage_guard_denied')" in sql
    guard = read("services/platform-worker/internal/mediausage/task_guard.go")
    assert "2*time.Second" in guard or "2 * time.Second" in guard
    assert "ReadMetricsState" in guard and "Assess(" in guard
    assert "media_default_only" not in guard
    assert "UPDATE " not in guard and "DELETE " not in guard
    assert "PlatformMediaReconciliationAdmissionPaused" in read("infra/monitoring/alerts.yml")
