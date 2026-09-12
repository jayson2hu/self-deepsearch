from __future__ import annotations

import hashlib
import io
import json
import os
import sys
import tempfile
import threading
import time
import unittest
import uuid
from contextlib import redirect_stderr
from datetime import UTC, datetime, timedelta
from pathlib import Path
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from media_worker import cli  # noqa: E402
from media_worker.storage import S3ObjectStore  # noqa: E402
from media_worker.upload_control import (  # noqa: E402
    UploadControl,
    UploadControlBlocked,
    UploadControlConflict,
    UploadControlExpired,
    configured_control,
)
from media_worker.upload_guard import (  # noqa: E402
    UploadBusy,
    UploadGuardError,
    UploadRecoveryRequired,
    acknowledge_upload,
    upload_admission,
    upload_status,
)


def command(state, mode="paused", **changes):
    value = {"command_id": str(uuid.uuid4()), "expected_epoch": state["epoch"], "expected_generation": state["generation"],
             "mode": mode, "reason": "test-only operator review", "resume_confirmed": mode == "enabled"}
    return {**value, **changes}


class UploadControlTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.lock = self.root / "state" / "upload.lock"
        self.control = UploadControl(self.lock)
        env = patch.dict(os.environ, {"MEDIA_UPLOAD_CONTROL_MODE": "enforce", "MEDIA_DELIVERY_MODE": "normal"})
        env.start()
        self.addCleanup(env.stop)

    def apply(self, mode="paused", **changes):
        value = command(self.control.status(), mode, **changes)
        return self.control.apply(value, now=time.time())

    def enable(self):
        self.apply()
        return self.apply("enabled")

    def store(self, client):
        return S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=client,
                             upload_lock_file=self.lock)

    def test_missing_state_cannot_resume_and_restart_retains_pause(self):
        self.assertEqual(self.control.status()["generation"], 0)
        self.assertFalse(self.control.ledger.exists())
        with self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()
        with self.assertRaises(ValueError):
            self.apply("enabled")
        first = self.apply()
        self.assertEqual(first["epoch"], first["command_id"])
        self.assertEqual(UploadControl(self.lock).status()["mode"], "paused")
        self.apply("enabled")
        self.control.require_enabled()
        self.apply()
        with self.assertRaises(UploadControlBlocked):
            UploadControl(self.lock).require_enabled()

    def test_exact_retry_is_idempotent_but_stale_changed_or_reused_id_is_not(self):
        first_command = command(self.control.status())
        first = self.control.apply(first_command, now=time.time())
        before = self.control.ledger.read_bytes()
        replay = self.control.apply(first_command, now=time.time())
        self.assertEqual(replay, {**first, "status": "replayed"})
        self.assertEqual(self.control.ledger.read_bytes(), before)
        with self.assertRaises(UploadControlConflict):
            self.control.apply({**first_command, "reason": "different intent"}, now=time.time())
        self.apply("enabled")
        with self.assertRaises(UploadControlConflict):
            self.control.apply(first_command, now=time.time())
        with self.assertRaises(UploadControlConflict):
            self.apply(command_id=first_command["command_id"])
        with self.assertRaises(UploadControlConflict):
            self.apply(expected_epoch=str(uuid.uuid4()))
        self.assertEqual(len(self.control.ledger.read_text().splitlines()), 2)

    def test_invalid_commands_never_create_ledger(self):
        good = command(self.control.status())
        cases = [None, {}, [], {**good, "extra": 1}]
        for key, values in {
            "command_id": ["bad", str(uuid.uuid4()).upper()], "expected_generation": [True, -1, 1.5, 2**53],
            "expected_epoch": [None, "bad"], "mode": ["normal", [], None], "resume_confirmed": [1, True],
            "reason": ["", "x", " x ", "x" * 1001, "has\nnewline", None],
        }.items():
            cases.extend({**good, key: value} for value in values)
        for value in cases:
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.control.apply(value, now=time.time())
        self.assertFalse(self.control.ledger.exists())

    def test_corrupt_incomplete_or_oversized_ledger_fails_closed_without_rewriting(self):
        self.enable()
        good = self.control.ledger.read_bytes()
        record = json.loads(good.splitlines()[-1])
        bad_chain = {**record, "generation": 99}
        cases = [b"", b"{}\n", b"null\n", b"x" * (1024 * 1024 + 1), good[:-1], good + b"partial",
                 good + json.dumps(bad_chain).encode() + b"\n", good.replace(b'"version":1', b'"version":1,"version":1', 1)]
        for body in cases:
            self.control.ledger.write_bytes(body)
            with self.subTest(size=len(body)):
                with self.assertRaises(UploadControlBlocked):
                    self.control.require_enabled()
                with self.assertRaises(UploadControlBlocked):
                    self.control.apply(command({"epoch": "", "generation": 0}), now=time.time())
                self.assertEqual(self.control.ledger.read_bytes(), body)

    def test_durability_failure_has_no_success_and_exact_retry_recovers(self):
        value = command(self.control.status())
        with patch("media_worker.upload_control.os.fsync", side_effect=OSError("synthetic fsync failure")):
            with self.assertRaises(OSError):
                self.control.apply(value, now=time.time())
        self.assertEqual(self.control.apply(value, now=time.time())["status"], "replayed")
        self.assertEqual(len(self.control.ledger.read_text().splitlines()), 1)

    def test_off_flag_does_not_remove_initialized_fence_and_static_stop_still_wins(self):
        with patch.dict(os.environ, {"MEDIA_UPLOAD_CONTROL_MODE": "off"}):
            self.assertIsNone(configured_control(self.lock))
        self.enable()
        with patch.dict(os.environ, {"MEDIA_DELIVERY_MODE": "default_only"}), self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()
        self.apply()
        with patch.dict(os.environ, {"MEDIA_UPLOAD_CONTROL_MODE": "off"}):
            retained = configured_control(self.lock)
            self.assertIsNotNone(retained)
            with self.assertRaises(UploadControlBlocked):
                retained.require_enabled()

    def test_cli_rejects_before_sdk_decoding_listing_or_backup(self):
        stderr = io.StringIO()
        with patch("media_worker.cli.S3ObjectStore") as sdk, redirect_stderr(stderr):
            result = cli.main(["prepare", "--input", str(self.root / "absent.png"), "--entity-type", "work",
                               "--entity-id", str(uuid.uuid4()), "--purpose", "cover", "--reason", "reviewed",
                               "--backup-root", str(self.root / "backup"), "--output", str(self.root / "manifest.json"),
                               "--upload-lock-file", str(self.lock)])
        self.assertEqual(result, 2)
        sdk.assert_not_called()
        self.assertIn("MEDIA_UPLOAD_CONTROL_BLOCKED", stderr.getvalue())
        self.assertFalse((self.root / "backup").exists())

    def test_pause_between_puts_keeps_uncertain_batch_and_resume_does_not_clear_it(self):
        self.enable()
        client = Mock()
        store = self.store(client)
        key = "media-master/test/master.webp"
        with self.assertRaises(UploadControlBlocked):
            with store.upload_guard():
                store.put(key, b"abc", content_type="image/webp", public=False, sha256=hashlib.sha256(b"abc").hexdigest())
                self.apply()
                store.put("media-public/test/w320.webp", b"abc", content_type="image/webp", public=True, sha256="a" * 64)
        self.assertEqual(client.put_object.call_count, 1)
        pending = upload_status(self.lock)["pending"]
        self.assertEqual(len(pending["objects"]), 1)
        self.apply("enabled")
        self.assertEqual(upload_status(self.lock)["pending"], pending)
        with self.assertRaises(UploadRecoveryRequired):
            with store.upload_guard():
                self.fail("resume discarded the uncertain upload")
        self.apply()
        acknowledge_upload(self.lock, expected_run_id=pending["run_id"], reason="manually reviewed", confirmed=True)
        with self.assertRaises(UploadControlBlocked):
            store.upload_control.require_enabled()

    def test_pause_cannot_report_applied_during_sdk_put(self):
        self.enable()
        entered, release = threading.Event(), threading.Event()
        errors = []
        client = Mock()
        def put(**_):
            entered.set()
            if not release.wait(4):
                raise TimeoutError("test did not release SDK")
        client.put_object.side_effect = put
        store = self.store(client)
        pending_pause = command(self.control.status())
        def upload():
            try:
                with store.upload_guard():
                    store.put("media-master/test/master.webp", b"abc", content_type="image/webp", public=False, sha256="a" * 64)
            except BaseException as exc:
                errors.append(exc)
        thread = threading.Thread(target=upload)
        thread.start()
        try:
            self.assertTrue(entered.wait(2))
            with self.assertRaises(UploadBusy):
                self.control.apply(pending_pause, now=time.time())
        finally:
            release.set()
            thread.join(5)
        self.assertFalse(thread.is_alive())
        self.assertEqual(errors, [])
        self.assertEqual(self.control.apply(pending_pause, now=time.time())["mode"], "paused")
        with self.assertRaises(UploadControlBlocked):
            with store.upload_guard():
                self.fail("paused store admitted a later batch")

    def test_each_listing_page_rechecks_and_verification_remains_available(self):
        self.enable()
        client = Mock()
        store = self.store(client)
        def first_page(**_):
            # Cannot take the control lock inside this SDK call; apply before
            # the subsequent operation begins instead.
            return {"IsTruncated": True, "NextContinuationToken": "next", "Contents": []}
        client.list_objects_v2.side_effect = first_page
        original = store.upload_control.operation
        count = 0
        def gate():
            nonlocal count
            count += 1
            if count == 2:
                self.apply()
            return original()
        with patch.object(store.upload_control, "operation", side_effect=gate), self.assertRaises(UploadControlBlocked):
            store.usage_bytes()
        self.assertEqual(client.list_objects_v2.call_count, 1)
        client.head_object.return_value = {"ContentLength": 3, "Metadata": {"sha256": "a" * 64}}
        store.verify("media-master/test/master.webp", byte_size=3, sha256="a" * 64)
        client.head_object.assert_called_once()

    def test_each_multipart_request_rechecks_control_before_storage_access(self):
        self.enable()
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.return_value = {
            "IsTruncated": False, "Uploads": [{"Key": "pending", "UploadId": "upload-1"}],
        }
        store = self.store(client)
        original = store.upload_control.operation
        count = 0

        def gate():
            nonlocal count
            count += 1
            if count == 3:
                self.apply()
            return original()

        with patch.object(store.upload_control, "operation", side_effect=gate), self.assertRaises(UploadControlBlocked):
            store.usage_bytes()
        self.assertEqual(client.list_objects_v2.call_count, 1)
        self.assertEqual(client.list_multipart_uploads.call_count, 1)
        self.assertEqual(client.list_parts.call_count, 0)

    def test_ledger_hardlink_is_rejected(self):
        self.apply()
        os.link(self.control.ledger, self.root / "alias")
        with self.assertRaises(UploadGuardError):
            self.control.status()

    def test_pause_does_not_need_upload_admission(self):
        self.enable()
        with upload_admission(self.lock):
            self.assertEqual(self.apply()["status"], "applied")

    def test_timed_command_expiry_is_checked_after_state_read_but_replay_is_read_only(self):
        self.enable()
        now = datetime.now(UTC).replace(microsecond=0)
        value = command(self.control.status(), issued_at=(now - timedelta(minutes=1)).isoformat().replace("+00:00", "Z"),
                        expires_at=(now + timedelta(seconds=1)).isoformat().replace("+00:00", "Z"))
        before = self.control.ledger.read_bytes()
        with self.assertRaises(UploadControlExpired):
            self.control.apply(value, now=lambda: (now + timedelta(seconds=1)).timestamp())
        self.assertEqual(self.control.ledger.read_bytes(), before)
        self.assertEqual(self.control.apply(value, now=now.timestamp())["status"], "applied")
        applied = self.control.ledger.read_bytes()
        self.assertEqual(self.control.apply(value, now=(now + timedelta(hours=1)).timestamp())["status"], "replayed")
        self.assertEqual(self.control.ledger.read_bytes(), applied)

    def test_invalid_or_future_validity_window_never_mutates(self):
        self.apply()
        now = datetime.now(UTC).replace(microsecond=0)
        def stamp(at: datetime) -> str:
            return at.isoformat().replace("+00:00", "Z")
        original = self.control.ledger.read_bytes()
        for issued, expires in [("bad", stamp(now)), (stamp(now), ""), (stamp(now), stamp(now)),
                                (stamp(now), stamp(now + timedelta(seconds=301))), (stamp(now), stamp(now - timedelta(seconds=1)))]:
            with self.subTest(issued=issued, expires=expires), self.assertRaises(ValueError):
                self.control.apply(command(self.control.status(), issued_at=issued, expires_at=expires), now=now.timestamp())
        with self.assertRaises(ValueError):
            self.control.apply(command(self.control.status(), expires_at=stamp(now)), now=now.timestamp())
        with self.assertRaises(UploadControlExpired):
            self.control.apply(command(self.control.status(), issued_at=stamp(now + timedelta(seconds=1)),
                                       expires_at=stamp(now + timedelta(seconds=60))), now=now.timestamp())
        self.assertEqual(self.control.ledger.read_bytes(), original)

    def test_enable_reserves_ledger_space_for_pause(self):
        self.enable()
        original = self.control.ledger.read_bytes()
        with patch("media_worker.upload_control.MAX_LEDGER_BYTES", len(original) + 16 * 1024 + 1):
            with self.assertRaises(UploadControlBlocked):
                self.apply("enabled")
            self.assertEqual(self.control.ledger.read_bytes(), original)
            self.assertEqual(self.apply()["mode"], "paused")
            with self.assertRaises(UploadControlBlocked):
                self.apply("enabled")
            with self.assertRaises(UploadControlBlocked):
                self.control.require_enabled()

    def test_pipeline_interruption_retains_fence_and_allows_verified_rollback(self):
        from media_worker.pipeline import _write_backup, prepare_manifest
        from PIL import Image
        from test_upload_safety import MemoryS3

        self.enable()
        input_path = self.root / "synthetic.png"
        with Image.new("RGB", (12, 12), "gray") as image:
            image.save(input_path)
        client = MemoryS3()
        calls = 0
        def backup(root, key, body):
            nonlocal calls
            calls += 1
            result = _write_backup(root, key, body)
            if calls == 2:
                self.apply()
            return result
        with patch("media_worker.pipeline._write_backup", side_effect=backup), self.assertRaises(UploadControlBlocked):
            prepare_manifest(input_path=input_path, entity_type="work", entity_id=str(uuid.uuid4()), purpose="cover",
                             source_type="manual", source_url=None, reason="synthetic reviewed image", position=0,
                             is_primary=True, confidence=1.0, asset_id=None, site_id="default", backup_root=self.root / "backup",
                             public_base_url="https://media.example.test", object_store=self.store(client),
                             manifest_path=self.root / "manifest.json")
        self.assertEqual(len(client.puts), 1)
        self.assertEqual(len(client.deletes), 1)
        self.assertFalse(client.objects)
        self.assertFalse((self.root / "manifest.json").exists())
        self.assertEqual(upload_status(self.lock)["status"], "review_required")
