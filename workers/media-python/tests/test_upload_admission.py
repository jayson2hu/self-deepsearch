from __future__ import annotations

import hashlib
import json
import os
import sys
import tempfile
import threading
import time
import unittest
import uuid
from datetime import UTC, datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import Mock, patch

from botocore.exceptions import ClientError

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from media_worker.storage import S3ObjectStore  # noqa: E402
from media_worker.upload_admission import (  # noqa: E402
    PATH,
    AdmissionClient,
    configured_decision,
    request_signature,
    response_signature,
)
from media_worker.upload_control import (  # noqa: E402
    UploadControl,
    UploadControlBlocked,
    UploadControlConflict,
    UploadControlExpired,
)
from media_worker.upload_guard import UploadRecoveryRequired, upload_status  # noqa: E402

SECRET = "synthetic-admission-secret-at-least-32-bytes"


def command(control, mode):
    state = control.status()
    return {"command_id": str(uuid.uuid4()), "expected_epoch": state["epoch"], "expected_generation": state["generation"],
            "mode": mode, "reason": "synthetic operator review", "resume_confirmed": mode == "enabled"}


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.mode, self.requests, self.replay = "allow", [], None
        self.stop = threading.Event()
        case = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_POST(self):  # noqa: N802
                body = self.rfile.read(int(self.headers["Content-Length"]))
                timestamp, nonce = self.headers["X-SD-Admission-Timestamp"], self.headers["X-SD-Admission-Nonce"]
                case.requests.append(nonce)
                if self.path != PATH or body != b"{}" or self.headers["X-SD-Admission-Signature"] != request_signature(SECRET.encode(), timestamp, nonce):
                    self.send_error(401)
                    return
                if case.mode == "slow-header":
                    try:
                        for value in b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n":
                            self.wfile.write(bytes([value]))
                            self.wfile.flush()
                            if case.stop.wait(0.12):
                                break
                    except OSError:
                        pass
                    return
                status = 429 if case.mode == "busy" else 302 if case.mode == "redirect" else 200
                value = {"decision": "deny" if case.mode == "deny" else "allow", "checked_at": datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ"), "request_nonce": nonce}
                if case.mode == "stale":
                    value["checked_at"] = (datetime.now(UTC) - timedelta(seconds=10)).strftime("%Y-%m-%dT%H:%M:%SZ")
                if case.mode == "future":
                    value["checked_at"] = (datetime.now(UTC) + timedelta(seconds=10)).strftime("%Y-%m-%dT%H:%M:%SZ")
                if case.mode == "wrong-nonce":
                    value["request_nonce"] = "f" * 64
                if case.mode == "wrong-type":
                    value["decision"] = ["allow"]
                if case.mode == "extra":
                    value["private_sample"] = "never display this"
                encoded = json.dumps(value, separators=(",", ":")).encode()
                if case.mode == "invalid-json":
                    encoded = b"private malformed response"
                if case.mode == "invalid-utf8":
                    encoded = b"\xff"
                if case.mode == "duplicate-json":
                    encoded = encoded[:-1] + b',"decision":"allow"}'
                signature = response_signature(SECRET.encode(), timestamp, nonce, status, encoded)
                if case.mode == "bad-signature":
                    signature = "a1=" + "0" * 64
                if case.mode == "replay":
                    encoded, signature = case.replay
                else:
                    case.replay = encoded, signature
                self.send_response(status)
                self.send_header("Content-Type", "text/plain" if case.mode == "wrong-content-type" else "application/json")
                if case.mode != "missing-length":
                    self.send_header("Content-Length", "2000" if case.mode == "oversize" else str(len(encoded)))
                if case.mode == "large-headers":
                    self.send_header("X-Padding", "x" * 9000)
                if case.mode == "duplicate-header":
                    self.send_header("Content-Type", "application/json")
                if case.mode == "compressed":
                    self.send_header("Content-Encoding", "gzip")
                if case.mode == "redirect":
                    self.send_header("Location", "http://127.0.0.1:1/private")
                if case.mode != "unsigned":
                    self.send_header("X-SD-Admission-Response", signature)
                self.end_headers()
                try:
                    self.wfile.write(encoded)
                except OSError:
                    pass

        self.server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.server.daemon_threads = True
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(self.shutdown)
        self.origin = f"http://127.0.0.1:{self.server.server_port}"
        self.client = AdmissionClient(self.origin, SECRET, allow_http=True)
        env = patch.dict(os.environ, {"MEDIA_UPLOAD_ADMISSION_MODE": "off", "MEDIA_UPLOAD_CONTROL_MODE": "enforce", "MEDIA_DELIVERY_MODE": "normal",
                                      "MEDIA_UPLOAD_ADMISSION_URL": self.origin, "MEDIA_UPLOAD_ADMISSION_SECRET": SECRET, "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP": "true",
                                      "MEDIA_HMAC_SECRET": "m" * 40, "MEDIA_UPLOAD_CONTROL_SECRET": "u" * 40})
        env.start()
        self.addCleanup(env.stop)
        self.lock = self.root / "state" / "upload.lock"
        self.control = UploadControl(self.lock)
        self.control.apply(command(self.control, "paused"))
        self.control.apply(command(self.control, "enabled"))

    def shutdown(self):
        self.stop.set()
        self.server.shutdown()
        self.server.server_close()

    def enforce(self):
        os.environ["MEDIA_UPLOAD_ADMISSION_MODE"] = "enforce"

    def store(self, sdk):
        return S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=sdk, upload_lock_file=self.lock)

    def test_actual_http_allow_denial_replay_and_malformed_evidence(self):
        first = self.client.check()
        self.assertEqual(first.outcome, "allow")
        self.mode = "replay"
        self.assertEqual(self.client.check().outcome, "unavailable")
        for mode in ["unsigned", "bad-signature", "wrong-content-type", "duplicate-header", "missing-length", "oversize", "large-headers",
                     "compressed", "redirect", "wrong-nonce", "wrong-type", "extra", "invalid-json", "invalid-utf8", "duplicate-json", "stale", "future"]:
            with self.subTest(mode=mode):
                self.mode = mode
                self.assertEqual(self.client.check().outcome, "unavailable")
        for mode in ["deny", "busy"]:
            self.mode = mode
            result = self.client.check()
            self.assertEqual(result.outcome, "denied")
            self.assertEqual(len(result.response_hash), 64)
        self.assertEqual(len(set(self.requests)), len(self.requests))

    def test_slow_headers_and_connection_failure_have_total_deadline(self):
        self.mode = "slow-header"
        start = time.monotonic()
        self.assertEqual(self.client.check().outcome, "unavailable")
        self.assertLess(time.monotonic() - start, 4.5)
        self.stop.set()
        self.shutdown()
        start = time.monotonic()
        self.assertEqual(self.client.check().outcome, "unavailable")
        self.assertLess(time.monotonic() - start, 4.5)

    def test_configuration_does_not_allow_arbitrary_urls_or_shared_keys(self):
        for origin in ["https://example.test", "https://user:password@127.0.0.1", "https://127.0.0.1/path", "https://127.0.0.1?", "https://127.0.0.1#",
                       "https://0.0.0.0", "https://224.0.0.1", "http://127.0.0.1", "https://[::1%zone]",
                       "https://@127.0.0.1", "https://127.0.0.1:0", "https://127.0.0.1:65536", " https://127.0.0.1"]:
            with self.subTest(origin=origin), self.assertRaises(ValueError):
                AdmissionClient(origin, SECRET)
        self.assertIsNone(configured_decision())
        self.enforce()
        for key, value in [("MEDIA_UPLOAD_ADMISSION_MODE", "typo"), ("MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "typo"),
                           ("MEDIA_UPLOAD_ADMISSION_SECRET", "m" * 40), ("MEDIA_UPLOAD_ADMISSION_URL", "http://bad-host")]:
            with patch.dict(os.environ, {key: value}):
                self.assertEqual(configured_decision().outcome, "unavailable")

    def test_automatic_pause_uses_system_evidence_same_cas_and_never_auto_resumes(self):
        old = command(self.control, "enabled")
        before = self.control.status()
        self.enforce()
        self.mode = "deny"
        with self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()
        paused = self.control.status()
        self.assertEqual(paused["generation"], before["generation"] + 1)
        self.assertEqual(paused["epoch"], before["epoch"])
        self.assertEqual(paused["mode"], "paused")
        record = json.loads(self.control.ledger.read_text().splitlines()[-1])
        self.assertEqual(record["source"], "usage_admission")
        self.assertEqual(record["version"], 2)
        self.assertEqual(record["decision"]["nonce"], self.requests[-1])
        self.assertNotIn("actor_id", record)
        self.mode = "allow"
        requests = len(self.requests)
        for _ in range(2):
            with self.assertRaises(UploadControlBlocked):
                UploadControl(self.lock).require_enabled()
        self.assertEqual(len(self.requests), requests)
        with patch.dict(os.environ, {"MEDIA_UPLOAD_ADMISSION_MODE": "off", "MEDIA_UPLOAD_CONTROL_MODE": "off"}), self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()
        with self.assertRaises(UploadControlConflict):
            self.control.apply(old)
        self.control.apply(command(self.control, "enabled"))
        self.control.require_enabled()
        self.assertEqual(self.control.status()["generation"], paused["generation"] + 1)

    def test_resume_expiring_during_admission_never_appends_enabled(self):
        self.control.apply(command(self.control, "paused"))
        before = self.control.status()
        now = datetime.now(UTC).replace(microsecond=0)
        request = command(self.control, "enabled")
        request.update(issued_at=now.strftime("%Y-%m-%dT%H:%M:%SZ"),
                       expires_at=(now + timedelta(seconds=1)).strftime("%Y-%m-%dT%H:%M:%SZ"))
        self.enforce()
        with self.assertRaises(UploadControlExpired):
            self.control.apply(request, now=Mock(side_effect=[now.timestamp(), now.timestamp() + 1]))
        self.assertEqual(self.control.status(), before)
        self.assertEqual(len(self.requests), 1)

    def test_admission_before_each_listing_and_put_preserves_pending_and_deletion(self):
        self.enforce()
        sdk = Mock()
        sdk.head_object.side_effect = ClientError({"Error": {"Code": "NoSuchKey"}, "ResponseMetadata": {"HTTPStatusCode": 404}}, "HeadObject")
        store = self.store(sdk)
        with self.assertRaises(UploadControlBlocked):
            with store.upload_guard():
                store.put("media-master/test/master.webp", b"abc", content_type="image/webp", public=False, sha256=hashlib.sha256(b"abc").hexdigest())
                self.mode = "deny"
                store.put("media-public/test/w320.webp", b"abc", content_type="image/webp", public=True, sha256="a" * 64)
        self.assertEqual(sdk.put_object.call_count, 1)
        pending = upload_status(self.lock)["pending"]
        self.assertEqual(len(pending["objects"]), 1)
        store.delete_and_verify("media-master/test/master.webp")
        sdk.delete_object.assert_called_once()
        self.assertEqual(upload_status(self.lock)["pending"], pending)
        with self.assertRaises(UploadControlBlocked):
            self.control.apply(command(self.control, "enabled"))
        self.mode = "allow"
        self.control.apply(command(self.control, "enabled"))
        with self.assertRaises(UploadRecoveryRequired):
            with store.upload_guard():
                self.fail("resume discarded pending writes")
        def first_page(**_):
            self.mode = "deny"
            return {"Contents": [], "IsTruncated": True, "NextContinuationToken": "next-page"}
        sdk.list_objects_v2.side_effect = first_page
        with self.assertRaises(UploadControlBlocked):
            store.usage_bytes()
        self.assertEqual(sdk.list_objects_v2.call_count, 1)

    def test_bad_admission_and_fsync_failure_block_without_confirming_success(self):
        self.enforce()
        self.mode = "unsigned"
        with patch("media_worker.upload_control.os.fsync", side_effect=OSError("synthetic persistence failure")), self.assertRaises(OSError):
            self.control.require_enabled()
        record = json.loads(self.control.ledger.read_text().splitlines()[-1])
        self.assertEqual(record["decision"]["outcome"], "unavailable")
        self.assertEqual(record["decision"]["response_hash"], "")
        self.assertEqual(UploadControl(self.lock).status()["mode"], "paused")
        self.mode = "allow"
        with self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()

    def test_invalid_system_origin_and_incomplete_ledger_do_not_open_uploads(self):
        self.enforce()
        self.mode = "deny"
        with self.assertRaises(UploadControlBlocked):
            self.control.require_enabled()
        lines = self.control.ledger.read_text().splitlines()
        original = json.loads(lines[-1])
        for field, value in [("source", "operator"), ("version", 3), ("decision", {}), ("command", {**original["command"], "mode": "enabled", "resume_confirmed": True})]:
            with self.subTest(field=field):
                changed = {**original, field: value}
                self.control.ledger.write_text("\n".join([*lines[:-1], json.dumps(changed)]) + "\n")
                with self.assertRaises(UploadControlBlocked):
                    self.control.status()
