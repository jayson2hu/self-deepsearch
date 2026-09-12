from __future__ import annotations

import http.client
import json
import sys
import tempfile
import threading
import time
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path
from unittest.mock import Mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from media_worker.server import handler  # noqa: E402
from media_worker.upload_control import UploadControl  # noqa: E402
from media_worker.upload_control_http import request_signature, response_signature  # noqa: E402
from media_worker.upload_guard import locked_upload_file  # noqa: E402
from test_upload_control import command  # noqa: E402

SECRET = "test-only-control-secret-" * 2
DELETE_SECRET = "test-only-delete-secret-" * 2
STATUS = "/v1/upload-control/status"
APPLY = "/v1/upload-control/apply"


class UploadControlHTTPTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.control = UploadControl(Path(temporary.name) / "upload.lock")
        self.delete, self.reconcile = Mock(), Mock()
        self.now = int(time.time())
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler(self.delete, self.reconcile, DELETE_SECRET,
                                          now=lambda: self.now, upload_control=self.control, control_secret=SECRET))
        thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(thread.join, 3)
        self.addCleanup(self.server.shutdown)

    def request(self, path=STATUS, body=b"{}", *, signed_path=None, timestamp=None, nonce=None, signature_secret=SECRET, headers=None):
        timestamp = str(self.now) if timestamp is None else timestamp
        nonce = "a" * 64 if nonce is None else nonce
        request_headers = {"Content-Type": "application/json", "X-SD-Control-Timestamp": timestamp, "X-SD-Control-Nonce": nonce,
                           "X-SD-Control-Signature": request_signature(signature_secret.encode(), signed_path or path, timestamp, nonce, body)}
        request_headers.update(headers or {})
        conn = http.client.HTTPConnection("127.0.0.1", self.server.server_port, timeout=3)
        try:
            conn.request("POST", path, body, request_headers)
            result = conn.getresponse()
            response_body = result.read()
            response_mac = result.getheader("X-SD-Control-Response")
            if response_mac:
                self.assertEqual(response_mac, response_signature(SECRET.encode(), path, timestamp, nonce, body, result.status, response_body))
            self.assertEqual(result.getheader("Cache-Control"), "no-store")
            return result.status, json.loads(response_body), response_mac
        finally:
            conn.close()

    def test_authenticated_status_apply_and_exact_retry_have_bound_receipts(self):
        status, state, signature = self.request()
        self.assertEqual(status, 200)
        self.assertTrue(signature)
        body = json.dumps(command(state)).encode()
        status, receipt, signature = self.request(APPLY, body)
        self.assertEqual((status, receipt["status"], receipt["generation"]), (200, "applied", 1))
        self.assertTrue(signature)
        status, replay, new_mac = self.request(APPLY, body, nonce="b" * 64)
        self.assertEqual(replay, {**receipt, "status": "replayed"})
        self.assertNotEqual(new_mac, signature)
        self.assertEqual(self.request(APPLY, json.dumps(command(receipt, "enabled")).encode())[0], 200)
        self.assertEqual(self.request(APPLY, body)[0], 409)
        self.delete.delete.assert_not_called()
        self.reconcile.reconcile.assert_not_called()

    def test_cross_path_wrong_secret_old_timestamp_or_invalid_nonce_is_rejected(self):
        cases = [{"signed_path": APPLY}, {"signature_secret": DELETE_SECRET}, {"timestamp": str(self.now - 301)},
                 {"timestamp": str(self.now + 301)}, {"timestamp": "1" * 13}, {"nonce": "bad"}]
        for changes in cases:
            with self.subTest(changes=changes):
                status, _, signed = self.request(**changes)
                self.assertEqual(status, 401)
                self.assertIsNone(signed)
        self.assertFalse(self.control.ledger.exists())

    def test_invalid_body_or_encoding_does_not_mutate(self):
        for body in [b"null", b"[]", b'{"a":1,"a":2}', b"{}{}", b"\xff", b'{"reason":"' + b"[" * 10000]:
            with self.subTest(body=body[:20]):
                self.assertEqual(self.request(APPLY, body)[0], 400)
        self.assertEqual(self.request(headers={"Content-Type": "text/plain"})[0], 400)
        self.assertEqual(self.request(headers={"Content-Encoding": "gzip"})[0], 400)
        self.assertEqual(self.request(body=b"x" * 16385)[0], 400)
        self.assertFalse(self.control.ledger.exists())

    def test_busy_and_corrupt_state_never_acknowledge_application(self):
        body = json.dumps(command(self.control.status())).encode()
        with locked_upload_file(self.control.lock):
            status, result, signed = self.request(APPLY, body)
            self.assertEqual((status, result["error"]), (503, "control_busy"))
            self.assertTrue(signed)
        self.control.ledger.write_bytes(b"broken")
        status, result, signed = self.request(APPLY, body)
        self.assertEqual((status, result["error"]), (503, "control_unavailable"))
        self.assertTrue(signed)
        self.assertEqual(self.control.ledger.read_bytes(), b"broken")

    def test_control_secret_must_be_separate(self):
        for secret in ("", "short", DELETE_SECRET):
            with self.subTest(secret_length=len(secret)), self.assertRaises(ValueError):
                handler(self.delete, self.reconcile, DELETE_SECRET, upload_control=self.control, control_secret=secret)
