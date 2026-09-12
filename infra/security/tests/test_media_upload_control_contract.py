from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_upload_control_is_explicit_and_secrets_stay_in_two_executors() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml", "infra/compose/beijing/compose.yaml"):
        services = yaml.safe_load(read(path))["services"]
        for name, service in services.items():
            env = service.get("environment", {})
            if name == "media-python":
                assert env["MEDIA_UPLOAD_CONTROL_MODE"] == "${MEDIA_UPLOAD_CONTROL_MODE:-off}"
                assert env["MEDIA_UPLOAD_CONTROL_SECRET"] == "${MEDIA_UPLOAD_CONTROL_SECRET:-}"
                assert "media-state:/var/lib/self-deepsearch/media-state" in service["volumes"]
            elif name == "platform-worker-japan":
                assert "MEDIA_UPLOAD_CONTROL_URL" in env
                assert env["MEDIA_UPLOAD_CONTROL_SECRET"] == "${MEDIA_UPLOAD_CONTROL_SECRET:-}"
                assert "MEDIA_UPLOAD_CONTROL_MODE" not in env
            else:
                assert not any(key.startswith("MEDIA_UPLOAD_CONTROL") for key in env)
    main = read("services/platform-worker/cmd/worker/main.go")
    assert main.index('os.Args[1] == "media-upload-control"') < main.index("config.FromEnv()")
    assert "mediauploadcontrol.RunCommand" in main
    for path in ("services/platform-worker/internal/mediausage/reviews.go", "services/platform-worker/internal/mediausage/runner.go"):
        # Observation/acknowledgement must not automatically reopen uploads.
        assert "mediauploadcontrol" not in read(path)


def test_readiness_docs_and_cross_language_fixture_do_not_claim_full_quota_control() -> None:
    assert "--enable-upload-control" in read("workers/media-python/tests/http_delete_fixture.py")
    contract = read("services/platform-worker/internal/mediauploadcontrol/python_contract_test.go")
    assert "MEDIA_CONTRACT_PYTHON" in contract and "checkUpload(0)" in contract and "checkUpload(1)" in contract
    assert 'Type: "media_delete"' in contract
    assert "MEDIA_CONTRACT_PYTHON" in read(".github/workflows/ci.yml")
