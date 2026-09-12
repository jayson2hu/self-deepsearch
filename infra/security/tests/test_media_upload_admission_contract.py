from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_upload_admission_configuration_is_off_and_executor_only() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml", "infra/compose/beijing/compose.yaml"):
        for name, service in yaml.safe_load(read(path))["services"].items():
            env = service.get("environment", {})
            keys = {key for key in env if key.startswith("MEDIA_UPLOAD_ADMISSION_")}
            if name in {"platform-worker-japan", "media-python"}:
                assert env["MEDIA_UPLOAD_ADMISSION_MODE"] == "${MEDIA_UPLOAD_ADMISSION_MODE:-off}"
                assert env["MEDIA_UPLOAD_ADMISSION_SECRET"] == "${MEDIA_UPLOAD_ADMISSION_SECRET:-}"
                expected = {"MEDIA_UPLOAD_ADMISSION_MODE", "MEDIA_UPLOAD_ADMISSION_SECRET"}
                if name == "media-python":
                    expected |= {"MEDIA_UPLOAD_ADMISSION_URL", "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP"}
                    assert env["MEDIA_UPLOAD_ADMISSION_URL"] == "${MEDIA_UPLOAD_ADMISSION_URL:-}"
                    assert env["MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP"] == "${MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP:-false}"
                assert keys == expected
            else:
                assert not keys
    for path in (".env.example", "infra/compose/japan/.env.example", "infra/compose/beijing/.env.example"):
        source = read(path).splitlines()
        assert "MEDIA_UPLOAD_ADMISSION_MODE=off" in source
        assert "MEDIA_UPLOAD_ADMISSION_SECRET=" in source


def test_upload_admission_has_no_default_public_listener_or_browser_route() -> None:
    services = yaml.safe_load(read("infra/compose/japan/compose.yaml"))["services"]
    assert not services["platform-worker-japan"].get("ports")
    override = yaml.safe_load(read("infra/compose/japan/upload-admission.tunnel.yaml"))
    assert override == {"services": {"platform-worker-japan": {"ports": ["127.0.0.1:18081:8081"]}}}
    for path in ROOT.joinpath("infra/reverse-proxy").rglob("*.conf*"):
        assert "/v1/upload-admission" not in path.read_text(encoding="utf-8")
    main = read("services/platform-worker/cmd/worker/main.go")
    assert 'configuration.MediaUploadAdmissionMode == "enforce"' in main
    assert "mediausage.NewTaskGuard(usageRepository" in main
    assert "UploadAdmissionHTTP:" in main


def test_admission_alert_does_not_claim_remote_or_billing_success() -> None:
    rules = yaml.safe_load(read("infra/monitoring/alerts.yml"))
    alerts = [rule for group in rules["groups"] for rule in group["rules"]]
    alert = next(rule for rule in alerts if rule["alert"] == "PlatformMediaUploadAdmissionDenied")
    assert 'outcome=~"denied|busy"' in alert["expr"]
    assert "inspect Beijing" in alert["annotations"]["summary"]
    assert "never reached Japan" in alert["annotations"]["description"]
    assert "explicit review and resume" in alert["annotations"]["description"]


def test_go_ci_installs_real_python_media_dependencies_before_contracts() -> None:
    job = yaml.safe_load(read(".github/workflows/ci.yml"))["jobs"]["go"]
    assert job["env"]["MEDIA_CONTRACT_PYTHON"] == "python3"
    steps = [step.get("run", "") for step in job["steps"]]
    install = next(index for index, run in enumerate(steps) if "pip install" in run and "./workers/media-python" in run)
    execute = next(index for index, run in enumerate(steps) if "go test ./services/platform-api/..." in run)
    assert install < execute
