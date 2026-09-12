from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]
STATE_ROOT = "/var/lib/self-deepsearch/media-state"
LOCK_FILE = STATE_ROOT + "/upload.lock"


def test_every_compose_uploader_uses_a_persistent_private_shared_volume() -> None:
    for filename in ("compose.yaml", "infra/compose/beijing/compose.yaml"):
        config = yaml.safe_load((ROOT / filename).read_text(encoding="utf-8"))
        media = config["services"]["media-python"]
        assert "media-state" in config["volumes"]
        assert f"media-state:{STATE_ROOT}" in media["volumes"]
        assert media["environment"]["MEDIA_UPLOAD_LOCK_FILE"] == "${MEDIA_UPLOAD_LOCK_FILE:-" + LOCK_FILE + "}"
        assert media["environment"]["MEDIA_STORAGE_LIMIT_BYTES"] == "${MEDIA_STORAGE_LIMIT_BYTES:-10737418240}"
        assert "S3_PUBLIC_BASE_URL" in media["environment"]
        assert "ports" not in media, "private state/recovery must not introduce a public upload service"
        if "platform-worker-beijing" in config["services"]:
            assert "AWS_ACCESS_KEY_ID" not in config["services"]["platform-worker-beijing"]["environment"]


def test_upload_state_is_precreated_for_nonroot_image_user_and_env_templates_match() -> None:
    dockerfile = (ROOT / "workers/media-python/Dockerfile").read_text(encoding="utf-8")
    assert "--uid 10001 media" in dockerfile
    assert f"install -d -o media -g media -m 0700 /var/lib/self-deepsearch/media {STATE_ROOT}" in dockerfile
    assert dockerfile.index("install -d") < dockerfile.index("USER media")
    for filename in (".env.example", "infra/compose/beijing/.env.example"):
        assert f"MEDIA_UPLOAD_LOCK_FILE={LOCK_FILE}" in (ROOT / filename).read_text(encoding="utf-8").splitlines()
    japan = yaml.safe_load((ROOT / "infra/compose/japan/compose.yaml").read_text(encoding="utf-8"))
    assert "media-python" not in japan["services"], "Release A must not silently add a second upload host"


def test_capacity_guard_counts_incomplete_multipart_parts_before_upload() -> None:
    storage = (ROOT / "workers/media-python/media_worker/storage.py").read_text(encoding="utf-8")
    assert "list_objects_v2" in storage
    assert "list_multipart_uploads" in storage
    assert "list_parts" in storage
    assert storage.index("_current_object_usage_bytes") < storage.index("_multipart_usage_bytes")
    assert "S3 multipart part usage listing failed" in storage
    assert "MAX_MULTIPART_UPLOAD_PAGES" in storage
    assert "MAX_MULTIPART_PART_PAGES" in storage
    assert "MAX_USAGE_REQUESTS = 256" in storage
    assert "request_budget.consume()" in storage
