from __future__ import annotations

import hashlib
import http.client
import json
import sys
import tempfile
import threading
import time
import unittest
from http.server import ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from media_worker.server import CloudflarePurger, DeleteService, ReconcileService, handler, signature  # noqa: E402


class FakeDeletingStore:
    def __init__(self) -> None:
        self.deleted: list[str] = []

    def delete_and_verify(self, key: str) -> None:
        self.deleted.append(key)


class FakePurger:
    def __init__(self) -> None:
        self.urls: list[str] = []

    def purge(self, public_url: str) -> None:
        self.urls.append(public_url)


class FakeReconcilingStore:
    def __init__(self, objects: dict[str, tuple[int, str]], listed: list[tuple[str, str]] | None = None) -> None:
        self.objects = objects
        self.listed = listed or []

    def inspect(self, key: str) -> tuple[int, str] | None:
        return self.objects.get(key)

    def list_media_keys(self) -> list[tuple[str, str]]:
        return self.listed


class FakeResponse:
    status = 200

    def __enter__(self) -> "FakeResponse":
        return self

    def __exit__(self, *args: object) -> None:
        return None

    def read(self, _: int) -> bytes:
        return b'{"success":true}'


class MediaDeleteTests(unittest.TestCase):
    def post_delete(self, service: DeleteService, payload: dict[str, object]) -> tuple[int, dict[str, object]]:
        secret = "s" * 32
        body = json.dumps(payload, separators=(",", ":")).encode()
        timestamp = str(int(time.time()))
        event_id = str(payload["event_id"])
        reconcile = ReconcileService(store=FakeReconcilingStore({}), backup_root=service.backup_root)
        server = ThreadingHTTPServer(("127.0.0.1", 0), handler(service, reconcile, secret))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        connection = http.client.HTTPConnection("127.0.0.1", server.server_port, timeout=3)
        try:
            connection.request("POST", "/v1/delete", body, {
                "Content-Type": "application/json", "X-SD-Event-ID": event_id,
                "X-SD-Timestamp": timestamp, "X-SD-Signature": signature(secret.encode(), timestamp, event_id, body),
            })
            response = connection.getresponse()
            self.assertEqual(response.getheader("Content-Type"), "application/json")
            self.assertEqual(response.getheader("Cache-Control"), "no-store")
            return response.status, json.loads(response.read())
        finally:
            connection.close()
            server.shutdown()
            server.server_close()
            thread.join(timeout=3)
            self.assertFalse(thread.is_alive(), "test server did not stop")

    def test_http_delete_acknowledges_the_exact_event_and_removed_object(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            key = "media-public/test.webp"
            backup = root / key
            backup.parent.mkdir(parents=True)
            backup.write_bytes(b"synthetic-image")
            store, purger = FakeDeletingStore(), FakePurger()
            payload = {
                "event_id": "11111111-1111-4111-8111-111111111111", "storage_scope": "public",
                "storage_key": key, "backup_path": key, "public_url": "https://media.example.test/" + key,
            }
            status, ack = self.post_delete(DeleteService(store=store, backup_root=root, purger=purger), payload)
            self.assertEqual(status, 200)
            self.assertEqual(ack, {
                "status": "deleted", "event_id": payload["event_id"],
                "storage_scope": "public", "storage_key": key,
            })
            self.assertEqual(store.deleted, [key])
            self.assertEqual(purger.urls, [payload["public_url"]])
            self.assertFalse(backup.exists())

    def test_http_delete_failure_never_returns_completion_acknowledgement(self) -> None:
        class FailingPurger:
            def purge(self, _: str) -> None:
                raise OSError("synthetic purge failure")

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            key = "media-public/test.webp"
            backup = root / key
            backup.parent.mkdir(parents=True)
            backup.write_bytes(b"synthetic-image")
            status, body = self.post_delete(
                DeleteService(store=FakeDeletingStore(), backup_root=root, purger=FailingPurger()),
                {"event_id": "11111111-1111-4111-8111-111111111111", "storage_scope": "public",
                 "storage_key": key, "backup_path": key, "public_url": "https://media.example.test/" + key},
            )
            self.assertEqual(status, 503)
            self.assertEqual(body, {"error": "delete_unavailable"})
            self.assertTrue(backup.exists())

    def test_cloudflare_purge_uses_single_file_and_bearer_token(self) -> None:
        captured: list[object] = []

        def open_request(request: object, *, timeout: int) -> FakeResponse:
            captured.extend([request, timeout])
            return FakeResponse()

        public_url = "https://media.example.test/media-public/test.webp"
        CloudflarePurger(zone_id="1234567890abcdef", api_token="t" * 24, opener=open_request).purge(public_url)
        request = captured[0]
        self.assertEqual(captured[1], 10)
        self.assertEqual(request.get_header("Authorization"), "Bearer " + "t" * 24)
        self.assertEqual(json.loads(request.data), {"files": [public_url]})

    def test_signature_is_bound_to_exact_body_and_event(self) -> None:
        body = b'{"event_id":"11111111-1111-4111-8111-111111111111"}'
        first = signature(b"a" * 32, "123", "11111111-1111-4111-8111-111111111111", body)
        second = signature(b"a" * 32, "123", "22222222-2222-4222-8222-222222222222", body)
        self.assertNotEqual(first, second)

    def test_reconcile_reports_missing_corrupt_and_orphan_objects(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            good_key = "media-master/a/v1/master.webp"
            corrupt_key = "media-public/a/v1/cover-w320.webp"
            body, digest = b"good", hashlib.sha256(b"good").hexdigest()
            backup = root / Path(*good_key.split("/"))
            backup.parent.mkdir(parents=True)
            backup.write_bytes(body)
            orphan_backup = root / "media-public" / "orphan-backup.webp"
            orphan_backup.parent.mkdir(parents=True, exist_ok=True)
            orphan_backup.write_bytes(b"orphan")
            store = FakeReconcilingStore(
                {good_key: (len(body), digest), corrupt_key: (999, "f" * 64)},
                [("private", good_key), ("public", corrupt_key), ("public", "media-public/orphan.webp")],
            )
            report = ReconcileService(store=store, backup_root=root).reconcile({
                "run_id": "11111111-1111-4111-8111-111111111111",
                "objects": [
                    {"storage_scope": "private", "storage_key": good_key, "backup_path": good_key, "sha256": digest, "byte_size": len(body)},
                    {"storage_scope": "public", "storage_key": corrupt_key, "backup_path": corrupt_key, "sha256": "a" * 64, "byte_size": 12},
                    {"storage_scope": "public", "storage_key": "media-public/missing.webp", "backup_path": "media-public/missing.webp", "sha256": "b" * 64, "byte_size": 10},
                ],
            })
            self.assertEqual(report["expected_count"], 3)
            self.assertEqual(report["missing_s3_count"], 1)
            self.assertEqual(report["corrupt_s3_count"], 1)
            self.assertEqual(report["missing_backup_count"], 2)
            self.assertEqual(report["orphan_s3_count"], 1)
            self.assertEqual(report["orphan_backup_count"], 1)

    def test_reconcile_bounds_long_samples_without_losing_issue_counts(self) -> None:
        # Listing-only orphans need no real files or provider requests. Include
        # Unicode to check the HTTP handler's default JSON ASCII escaping.
        keys = [("public", "media-public/" + "长" * 1800 + f"/{index}.webp") for index in range(80)]
        keys += [("private", "media-master/" + "a" * 1800 + f"/{index}.webp") for index in range(80)]
        with tempfile.TemporaryDirectory() as directory:
            report = ReconcileService(store=FakeReconcilingStore({}, keys), backup_root=Path(directory)).reconcile({
                "run_id": "11111111-1111-4111-8111-111111111111", "objects": [],
            })
        self.assertEqual(report["orphan_s3_count"], 160)
        self.assertLessEqual(len(json.dumps(report, separators=(",", ":")).encode()), 64 * 1024)
        samples = report["issue_samples"]
        self.assertEqual(len(samples), 6)
        self.assertGreater(len(samples["orphan_s3"]), 0)
        self.assertLess(len(samples["orphan_s3"]), 50)
        self.assertTrue(set(samples["orphan_s3"]).issubset({key for _, key in keys}))

    def test_delete_removes_s3_and_matching_backup(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            relative = "media-public/default/works/one/image.webp"
            backup = root / Path(*relative.split("/"))
            backup.parent.mkdir(parents=True)
            backup.write_bytes(b"image")
            store = FakeDeletingStore()
            purger = FakePurger()
            public_url = "https://media.example.test/" + relative
            DeleteService(store=store, backup_root=root, purger=purger).delete({
                "event_id": "11111111-1111-4111-8111-111111111111",
                "storage_scope": "public",
                "storage_key": relative,
                "backup_path": relative,
                "public_url": public_url,
            })
            self.assertEqual(store.deleted, [relative])
            self.assertEqual(purger.urls, [public_url])
            self.assertFalse(backup.exists())

    def test_delete_rejects_backup_mismatch_before_touching_storage(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            store = FakeDeletingStore()
            with self.assertRaises(ValueError):
                DeleteService(store=store, backup_root=Path(directory)).delete({
                    "event_id": "11111111-1111-4111-8111-111111111111",
                    "storage_scope": "private",
                    "storage_key": "media-master/a/v1/master.webp",
                    "backup_path": "media-master/b/v1/master.webp",
                    "public_url": None,
                })
            self.assertEqual(store.deleted, [])

    def test_delete_rejects_path_traversal(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(ValueError):
                DeleteService(store=FakeDeletingStore(), backup_root=Path(directory)).delete(json.loads(
                    '{"event_id":"11111111-1111-4111-8111-111111111111","storage_scope":"public","storage_key":"media-public/../secret","backup_path":"media-public/../secret","public_url":"https://media.example.test/media-public/../secret"}'
                ))


if __name__ == "__main__":
    unittest.main()
