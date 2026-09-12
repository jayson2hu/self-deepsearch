from __future__ import annotations

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
INVENTORY = ROOT / "infra/policy/media-storage-tasks.json"


def load_inventory() -> dict[str, object]:
    return json.loads(INVENTORY.read_text(encoding="utf-8"))


def test_release_a_object_store_task_classes_are_complete_and_safe() -> None:
    inventory = load_inventory()
    assert inventory["schema_version"] == 1
    tasks = inventory["tasks"]
    assert isinstance(tasks, list)
    by_id = {task["id"]: task for task in tasks}
    assert len(by_id) == len(tasks)
    assert set(by_id) == {
        "controlled_media_upload",
        "scheduled_full_reconciliation",
        "rights_media_deletion",
        "public_media_delivery",
    }

    upload = by_id["controlled_media_upload"]
    assert upload["budget_class"] == "nonessential"
    assert upload["automatic"] is False and upload["default_enabled"] is False
    assert {"per_operation_usage_admission", "two_bucket_capacity_scan"} <= set(upload["controls"])

    reconcile = by_id["scheduled_full_reconciliation"]
    assert reconcile["budget_class"] == "nonessential"
    assert reconcile["automatic"] is True and reconcile["default_enabled"] is False
    assert {"MEDIA_RECONCILE_USAGE_GUARD", "pre_schedule_usage_admission", "pre_dispatch_usage_admission"} <= set(
        reconcile["controls"]
    )

    deletion = by_id["rights_media_deletion"]
    assert deletion["budget_class"] == "necessary"
    assert deletion["usage_guard_bypass_required"] is True
    assert "never pause rights takedown" in deletion["failure_behavior"]

    delivery = by_id["public_media_delivery"]
    assert delivery["budget_class"] == "request_path"
    assert {"private_r2_binding", "default_image_fallback"} <= set(delivery["controls"])

    for task in tasks:
        assert task["source_files"]
        assert task["object_operations"]
        assert task["controls"]
        for relative in task["source_files"]:
            assert (ROOT / relative).is_file(), f"inventory source is missing: {relative}"


def test_direct_object_store_clients_are_registered() -> None:
    inventory = load_inventory()
    expected = set(inventory["direct_object_store_clients"])
    discovered: set[str] = set()
    sdk_call = re.compile(
        r"\.(?:put_object|head_object|delete_object|list_objects_v2|list_multipart_uploads|list_parts)\("
    )
    for root, suffix in (
        (ROOT / "workers/media-python/media_worker", "*.py"),
        (ROOT / "infra/cloudflare/media-gateway/src", "*.mjs"),
    ):
        for path in root.rglob(suffix):
            source = path.read_text(encoding="utf-8")
            if sdk_call.search(source) or ".bucket.get(" in source:
                discovered.add(path.relative_to(ROOT).as_posix())
    assert discovered == expected


def test_runtime_wiring_matches_the_task_inventory() -> None:
    main = (ROOT / "services/platform-worker/cmd/worker/main.go").read_text(encoding="utf-8")
    reconciliation = (ROOT / "services/platform-worker/internal/mediareconcile/reconcile.go").read_text(encoding="utf-8")
    storage = (ROOT / "workers/media-python/media_worker/storage.py").read_text(encoding="utf-8")
    server = (ROOT / "workers/media-python/media_worker/server.py").read_text(encoding="utf-8")
    gateway = (ROOT / "infra/cloudflare/media-gateway/src/index.mjs").read_text(encoding="utf-8")

    assert 'configuration.MediaReconcileUsageGuard == "enforce"' in main
    assert reconciliation.count("runner.Admission.Allow(ctx)") == 2
    assert "with upload_admission(path) as journal" in storage
    assert storage.count("self.upload_control.operation()") >= 4
    assert "self.store.delete_and_verify(storage_key)" in server
    assert 'policy.mode !== "normal"' in gateway
    assert gateway.index('policy.mode !== "normal"') < gateway.index("configuration.bucket.get(key)")


def test_non_object_store_scheduled_jobs_are_explicitly_out_of_scope() -> None:
    inventory = load_inventory()
    assert set(inventory["scheduled_jobs_without_object_store_data_plane_access"]) == {
        "catalog_cache_revalidation",
        "daily_default_image_inspection",
        "email_delivery",
        "history_archive",
        "media_usage_analytics_observation",
    }
