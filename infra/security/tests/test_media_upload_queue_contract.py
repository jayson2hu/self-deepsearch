from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


def read(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_upload_queue_stays_off_and_secrets_are_executor_only() -> None:
    for path in ("compose.yaml", "infra/compose/japan/compose.yaml", "infra/compose/beijing/compose.yaml"):
        for name, service in yaml.safe_load(read(path))["services"].items():
            env = service.get("environment", {})
            if name == "platform-worker-japan":
                assert env["MEDIA_UPLOAD_QUEUE_MODE"] == "${MEDIA_UPLOAD_QUEUE_MODE:-off}"
                assert env["MEDIA_UPLOAD_CONTROL_ALLOW_HTTP"] == "${MEDIA_UPLOAD_CONTROL_ALLOW_HTTP:-false}"
            else:
                assert "MEDIA_UPLOAD_QUEUE_MODE" not in env
    main = read("services/platform-worker/cmd/worker/main.go")
    assert 'MediaUploadQueueMode == "dispatch"' in main
    assert "mediauploadcontrol.NewPostgresQueue(outboxRepository.Pool()," in main
    assert "ResumeGuard:" in main


def test_upload_queue_migration_keeps_privileges_and_evidence_separate() -> None:
    migration = read("db/migrations/00021_media_upload_control_queue.sql")
    up, down = migration.split("-- +goose Down")
    for marker in ("CREATE UNIQUE INDEX media_upload_one_open_command_idx", "UNIQUE (actor_id,idempotency_key)",
                   "NEW.dispatch_count<>OLD.dispatch_count+1", "platform.lock_media_usage_reviewer(NEW.actor_id)",
                   "upload command request and final evidence are immutable", "interval '310 seconds'",
                   "upload target and confirmed state cannot regress", "platform.valid_media_upload_receipt(receipt)",
                   "SECURITY DEFINER SET search_path=pg_catalog", "FOR UPDATE;"):
        assert marker in up
    assert "GRANT SELECT,INSERT ON audit.media_upload_commands TO platform_api" in up
    assert "GRANT UPDATE(status,first_dispatched_at,dispatch_count,completed_at,error_code,receipt) ON audit.media_upload_commands TO platform_worker" in up
    assert "GRANT SELECT,INSERT,UPDATE ON audit.media_upload_commands" not in up
    assert "schema_version = 20" in down
    ci = read(".github/workflows/ci.yml")
    assert "CONFIRM_MEDIA_UPLOAD_API_CONTRACT: disposable-database" in ci
    assert "CONFIRM_MEDIA_UPLOAD_CONTRACT: disposable-database" in ci
    assert "TestPostgresMediaUploadQueueContract" in ci
