from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from media_worker.upload_guard import (  # noqa: E402
    UploadBusy,
    UploadGuardError,
    UploadRecoveryRequired,
    acknowledge_upload,
    upload_admission,
    upload_status,
)

KEY = "media-public/default/work/w320.webp"
CHILD = """
import sys, time
from pathlib import Path
from media_worker.upload_guard import upload_admission
lock, ready, stop, write = map(Path, sys.argv[1:])
with upload_admission(lock) as journal:
    if str(write) == 'yes':
        journal.before_put('media-public/default/work/w320.webp', 3, 'a'*64)
    ready.write_text('ready')
    deadline = time.monotonic() + 10
    while not stop.exists():
        if time.monotonic() > deadline:
            raise TimeoutError('test parent did not release child')
        time.sleep(.01)
"""


class UploadGuardTests(unittest.TestCase):
    def pending(self, lock: Path) -> dict:
        with self.assertRaisesRegex(OSError, "uncertain"):
            with upload_admission(lock) as journal:
                journal.before_put(KEY, 3, "a" * 64)
                raise OSError("uncertain")
        return upload_status(lock)["pending"]

    def test_success_and_pre_write_failure_leave_no_fence(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            lock = Path(directory) / "upload.lock"
            with upload_admission(lock) as journal:
                journal.before_put(KEY, 3, "a" * 64)
                self.assertTrue(lock.with_name(lock.name + ".pending.json").exists())
            self.assertEqual(upload_status(lock), {"status": "idle", "pending": None})
            self.assertTrue(lock.is_file(), "never unlink the permanent lock inode")
            with self.assertRaises(ValueError):
                with upload_admission(lock):
                    raise ValueError("invalid image, no write attempted")
            self.assertEqual(upload_status(lock)["status"], "idle")

    def test_uncertain_batch_requires_exact_confirmed_ack_and_preserves_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            lock = Path(directory) / "upload.lock"
            pending = self.pending(lock)
            with self.assertRaises(UploadRecoveryRequired):
                with upload_admission(lock):
                    self.fail("unconfirmed batch reopened admission")
            for run_id, confirmed, reason in ((pending["run_id"], False, "reviewed"), ("wrong", True, "reviewed"), (pending["run_id"], True, "")):
                with self.assertRaises(UploadGuardError):
                    acknowledge_upload(lock, expected_run_id=run_id, confirmed=confirmed, reason=reason)
                self.assertEqual(upload_status(lock)["pending"], pending)
            result = acknowledge_upload(lock, expected_run_id=pending["run_id"], confirmed=True, reason="remote quiescent and both buckets checked")
            self.assertEqual(result["status"], "manual_review_acknowledged")
            self.assertTrue(result["fresh_capacity_check_required"])
            review = json.loads(lock.with_name(lock.name + ".reviews.jsonl").read_text())
            self.assertEqual(review["pending"], pending)
            newer = self.pending(lock)
            with self.assertRaises(UploadRecoveryRequired):
                acknowledge_upload(lock, expected_run_id=pending["run_id"], confirmed=True, reason="old retry")
            self.assertEqual(upload_status(lock)["pending"]["run_id"], newer["run_id"])

    def test_shared_os_lock_and_crash_fence_across_real_processes(self) -> None:
        for crash in (False, True):
            with self.subTest(crash=crash), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                lock, ready, stop = root / "upload.lock", root / "ready", root / "stop"
                env = {key: value for key, value in os.environ.items() if key.upper() in {"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP", "TMPDIR", "COMSPEC", "PATHEXT"}}
                env["PYTHONPATH"] = str(ROOT)
                process = subprocess.Popen([sys.executable, "-c", CHILD, str(lock), str(ready), str(stop), "yes" if crash else "no"],
                                           stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env,
                                           creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0))
                try:
                    deadline = time.monotonic() + 5
                    while not ready.exists() and process.poll() is None and time.monotonic() < deadline:
                        time.sleep(.01)
                    self.assertTrue(ready.exists(), "child did not acquire the real OS lock")
                    with self.assertRaises(UploadBusy):
                        with upload_admission(lock):
                            self.fail("second process acquired active upload lock")
                    with self.assertRaises(UploadBusy):
                        acknowledge_upload(lock, expected_run_id="untrusted", confirmed=True, reason="should not clear active writer")
                    if crash:
                        process.terminate()
                    else:
                        stop.write_text("stop")
                    process.communicate(timeout=5)
                    self.assertIsNotNone(process.poll())
                    status = upload_status(lock)
                    self.assertEqual(status["status"], "review_required" if crash else "idle")
                    if crash:
                        with self.assertRaises(UploadRecoveryRequired):
                            with upload_admission(lock):
                                self.fail("crash fence was ignored")
                    self.assertTrue(lock.exists())
                finally:
                    if process.poll() is None:
                        process.kill()
                    process.communicate(timeout=5)

    def test_corrupt_unbounded_or_duplicate_journal_is_never_cleared(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            lock = Path(directory) / "upload.lock"
            good = self.pending(lock)
            pending_path = lock.with_name(lock.name + ".pending.json")
            invalid = [b"{}", b"null", b"not-json", b"x" * 16385, b'{"run_id":"one","run_id":"two"}']
            for field, value in (("byte_size", True), ("storage_key", "media-public/../private"), ("sha256", "bad")):
                altered = json.loads(json.dumps(good))
                altered["objects"][0][field] = value
                invalid.append(json.dumps(altered).encode())
            for body in invalid:
                pending_path.write_bytes(body)
                with self.assertRaises(UploadGuardError):
                    upload_status(lock)
                with self.assertRaises(UploadGuardError):
                    acknowledge_upload(lock, expected_run_id=good["run_id"], confirmed=True, reason="reviewed")
                self.assertEqual(pending_path.read_bytes(), body)

    def test_relative_and_missing_lock_paths_are_rejected(self) -> None:
        for value in (None, Path("relative.lock")):
            with self.assertRaises(UploadGuardError):
                upload_status(value)

    def test_symlink_lock_is_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "real"
            target.write_text("do not alter")
            alias = root / "alias"
            try:
                alias.symlink_to(target)
            except OSError:
                self.skipTest("this account cannot create symlinks")
            with self.assertRaises(UploadGuardError):
                upload_status(alias)
            self.assertEqual(target.read_text(), "do not alter")
