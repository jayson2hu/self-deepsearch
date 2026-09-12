from __future__ import annotations

import hashlib
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import Mock, patch

from botocore.exceptions import ClientError, ReadTimeoutError
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from media_worker import cli  # noqa: E402
from media_worker.pipeline import MediaRejected, _write_manifest, prepare_manifest  # noqa: E402
from media_worker.storage import S3ObjectStore  # noqa: E402
from media_worker.upload_guard import (  # noqa: E402
    UploadGuardError,
    UploadRecoveryRequired,
    upload_admission,
    upload_status,
)

ENTITY = "11111111-1111-4111-8111-111111111111"
ASSET = "22222222-2222-4222-8222-222222222222"
KEY = f"media-master/{ASSET}/v1/master.webp"


class MemoryS3:
    """SDK-shaped fake: PUT may commit and then lose its response."""

    def __init__(self, *, timeout_on: str = "", fail_delete: bool = False) -> None:
        self.objects: dict[tuple[str, str], dict] = {}
        self.timeout_on = timeout_on
        self.fail_delete = fail_delete
        self.puts: list[str] = []
        self.deletes: list[str] = []

    def list_objects_v2(self, **args):
        items = [{"Key": key, "Size": len(value["Body"])} for (bucket, key), value in self.objects.items() if bucket == args["Bucket"]]
        return {"IsTruncated": False, "Contents": items, "KeyCount": len(items)}

    def list_multipart_uploads(self, **_):
        return {"IsTruncated": False, "Uploads": []}

    def put_object(self, **args):
        self.puts.append(args["Key"])
        identity = args["Bucket"], args["Key"]
        if identity in self.objects:
            raise ClientError({"Error": {"Code": "PreconditionFailed"}, "ResponseMetadata": {"HTTPStatusCode": 412}}, "PutObject")
        self.objects[identity] = args
        if self.timeout_on and self.timeout_on in args["Key"]:
            raise ReadTimeoutError(endpoint_url="https://secret-must-not-be-logged.invalid")

    def head_object(self, **args):
        item = self.objects.get((args["Bucket"], args["Key"]))
        if item is None:
            raise ClientError({"Error": {"Code": "NoSuchKey"}, "ResponseMetadata": {"HTTPStatusCode": 404}}, "HeadObject")
        return {"ContentLength": len(item["Body"]), "Metadata": item["Metadata"]}

    def delete_object(self, **args):
        self.deletes.append(args["Key"])
        if self.fail_delete:
            raise OSError("synthetic delete failure")
        self.objects.pop((args["Bucket"], args["Key"]), None)


def store_for(client, lock=None):
    if isinstance(client, Mock) and not isinstance(client.list_multipart_uploads.return_value, dict):
        client.list_multipart_uploads.return_value = {"IsTruncated": False, "Uploads": []}
    return S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=client, upload_lock_file=lock)


class UsageListingTests(unittest.TestCase):
    def test_pagination_includes_sparse_pages_and_both_full_buckets(self):
        client = Mock()
        client.list_objects_v2.side_effect = [
            {"IsTruncated": True, "KeyCount": 0, "NextContinuationToken": "next"},
            {"IsTruncated": False, "Contents": [{"Key": "outside-prefix.bin", "Size": 12}]},
            {"IsTruncated": False, "Contents": [{"Key": "also-outside.bin", "Size": 13}]},
        ]
        self.assertEqual(store_for(client).usage_bytes(), 25)
        self.assertEqual([call.kwargs for call in client.list_objects_v2.call_args_list], [
            {"Bucket": "private", "MaxKeys": 1000},
            {"Bucket": "private", "MaxKeys": 1000, "ContinuationToken": "next"},
            {"Bucket": "public", "MaxKeys": 1000},
        ])
        self.assertEqual([call.kwargs for call in client.list_multipart_uploads.call_args_list], [
            {"Bucket": "private", "MaxUploads": 1000},
            {"Bucket": "public", "MaxUploads": 1000},
        ])

    def test_incomplete_multipart_parts_are_counted_across_pages(self):
        client = Mock()
        client.list_objects_v2.side_effect = [
            {"IsTruncated": False, "Contents": [{"Key": "current.bin", "Size": 11}]},
            {"IsTruncated": False, "Contents": []},
        ]
        client.list_multipart_uploads.side_effect = [
            {"IsTruncated": False, "Uploads": [{"Key": "pending.bin", "UploadId": "upload-1"}]},
            {"IsTruncated": False, "Uploads": []},
        ]
        client.list_parts.side_effect = [
            {"IsTruncated": True, "Parts": [{"PartNumber": 1, "Size": 5}], "NextPartNumberMarker": 1},
            {"IsTruncated": False, "Parts": [{"PartNumber": 2, "Size": 7}]},
        ]
        self.assertEqual(store_for(client).usage_bytes(), 23)
        self.assertEqual([call.kwargs for call in client.list_parts.call_args_list], [
            {"Bucket": "private", "Key": "pending.bin", "UploadId": "upload-1", "MaxParts": 1000},
            {"Bucket": "private", "Key": "pending.bin", "UploadId": "upload-1", "MaxParts": 1000, "PartNumberMarker": 1},
        ])

    def test_multipart_pages_and_parts_fail_closed_on_ambiguous_evidence(self):
        cases = [
            ({}, None),
            ({"IsTruncated": 0}, None),
            ({"IsTruncated": False, "Uploads": [None]}, None),
            ({"IsTruncated": False, "Uploads": [{"Key": "", "UploadId": "id"}]}, None),
            ({"IsTruncated": False, "Uploads": [{"Key": "key", "UploadId": ""}]}, None),
            (
                {"IsTruncated": False, "Uploads": [{"Key": "key", "UploadId": "id"}]},
                {"IsTruncated": False, "Parts": [{"PartNumber": 1, "Size": True}]},
            ),
            (
                {"IsTruncated": False, "Uploads": [{"Key": "key", "UploadId": "id"}]},
                {"IsTruncated": True, "Parts": [], "NextPartNumberMarker": 1},
            ),
        ]
        for uploads, parts in cases:
            with self.subTest(uploads=uploads, parts=parts):
                client = Mock()
                client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
                client.list_multipart_uploads.return_value = uploads
                if parts is not None:
                    client.list_parts.return_value = parts
                with self.assertRaises(OSError):
                    store_for(client).usage_bytes()

    def test_multipart_repeated_markers_limits_and_provider_failure_do_not_return_partial_usage(self):
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.return_value = {
            "IsTruncated": True, "Uploads": [], "NextKeyMarker": "next", "NextUploadIdMarker": "upload-marker",
        }
        with self.assertRaisesRegex(OSError, "repeated next marker"):
            store_for(client).usage_bytes()
        client.reset_mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.return_value = {
            "IsTruncated": True, "Uploads": [], "NextKeyMarker": "next", "NextUploadIdMarker": "upload-marker",
        }
        with patch("media_worker.storage.MAX_MULTIPART_UPLOAD_PAGES", 1), self.assertRaisesRegex(OSError, "page limit"):
            store_for(client).usage_bytes()
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.side_effect = ClientError({"Error": {"Code": "secret-error"}}, "ListMultipartUploads")
        with self.assertRaisesRegex(OSError, "^S3 multipart usage listing failed$"):
            store_for(client).usage_bytes()
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.return_value = {
            "IsTruncated": False, "Uploads": [{"Key": "pending", "UploadId": "upload-1"}],
        }
        client.list_parts.side_effect = ClientError({"Error": {"Code": "NoSuchUpload"}}, "ListParts")
        with self.assertRaisesRegex(OSError, "^S3 multipart part usage listing failed$"):
            store_for(client).usage_bytes()

    def test_malformed_or_ambiguous_pages_fail_closed(self):
        pages = [None, {}, {"IsTruncated": 0}, {"IsTruncated": False, "Contents": None},
                 {"IsTruncated": False, "KeyCount": True}, {"IsTruncated": False, "KeyCount": 1},
                 {"IsTruncated": False, "Contents": [None]}, {"IsTruncated": False, "Contents": [{}]},
                 {"IsTruncated": False, "Contents": [{"Key": "", "Size": 1}]},
                 {"IsTruncated": False, "Contents": [{"Key": "a", "Size": 1}] * 1001},
                 {"IsTruncated": False, "Contents": [{"Key": "a", "Size": 1}] * 2},
                 {"IsTruncated": False, "NextContinuationToken": "contradiction"},
                 {"IsTruncated": True}, {"IsTruncated": True, "NextContinuationToken": 1},
                 {"IsTruncated": True, "NextContinuationToken": "x" * 4097}]
        pages += [{"IsTruncated": False, "Contents": [{"Key": "a", "Size": size}]} for size in (None, True, -1, "1", 1.5)]
        for page in pages:
            with self.subTest(page=page):
                client = Mock()
                client.list_objects_v2.return_value = page
                with self.assertRaises(OSError):
                    store_for(client).usage_bytes()
                self.assertEqual(client.list_objects_v2.call_count, 1)

    def test_repeated_tokens_page_limit_and_provider_failure_do_not_return_partial_usage(self):
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": True, "NextContinuationToken": "loop"}
        with self.assertRaisesRegex(OSError, "repeated"):
            store_for(client).usage_bytes()
        self.assertEqual(client.list_objects_v2.call_count, 2)
        client.reset_mock()
        with patch("media_worker.storage.MAX_USAGE_PAGES", 1), self.assertRaisesRegex(OSError, "page limit"):
            store_for(client).usage_bytes()
        self.assertEqual(client.list_objects_v2.call_count, 1)
        client.list_objects_v2.side_effect = ClientError({"Error": {"Code": "secret-error"}}, "ListObjectsV2")
        with self.assertRaisesRegex(OSError, "^S3 media usage listing failed$"):
            store_for(client).usage_bytes()

    def test_current_object_repeated_across_pages_is_rejected(self):
        client = Mock()
        client.list_objects_v2.side_effect = [
            {"IsTruncated": True, "Contents": [{"Key": "same", "Size": 1}], "NextContinuationToken": "next"},
            {"IsTruncated": False, "Contents": [{"Key": "same", "Size": 1}]},
        ]
        with self.assertRaisesRegex(OSError, "repeated objects"):
            store_for(client).usage_bytes()

    def test_usage_integer_overflow_is_rejected_for_objects_and_parts(self):
        client = Mock()
        client.list_objects_v2.return_value = {
            "IsTruncated": False,
            "Contents": [{"Key": "huge", "Size": (1 << 63) - 1}, {"Key": "overflow", "Size": 1}],
        }
        with self.assertRaisesRegex(OSError, "invalid size"):
            store_for(client).usage_bytes()

    def test_capacity_scan_has_one_global_request_budget(self):
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        with patch("media_worker.storage.MAX_USAGE_REQUESTS", 1), self.assertRaisesRegex(OSError, "request budget"):
            store_for(client).usage_bytes()
        self.assertEqual(client.list_objects_v2.call_count, 1)
        self.assertEqual(client.list_multipart_uploads.call_count, 0)
        client = Mock()
        client.list_objects_v2.return_value = {"IsTruncated": False, "Contents": []}
        client.list_multipart_uploads.return_value = {
            "IsTruncated": False, "Uploads": [{"Key": "pending", "UploadId": "upload-1"}],
        }
        client.list_parts.return_value = {
            "IsTruncated": False,
            "Parts": [{"PartNumber": 1, "Size": (1 << 63) - 1}, {"PartNumber": 2, "Size": 1}],
        }
        with self.assertRaisesRegex(OSError, "invalid size"):
            store_for(client).usage_bytes()


class UploadSafetyTests(unittest.TestCase):
    def test_delivery_stop_blocks_store_admission_but_preserves_deletion(self):
        client = MemoryS3()
        client.objects["private", KEY] = {"Body": b"old", "Metadata": {"sha256": "a" * 64}}
        with patch.dict("os.environ", {"MEDIA_DELIVERY_MODE": "default_only"}):
            with self.assertRaises(UploadGuardError):
                with store_for(client, self.lock).upload_guard():
                    self.fail("stopped uploader acquired admission")
            store_for(client).delete_and_verify(KEY)
            self.assertFalse(client.objects)
            self.assertFalse(client.puts)
            self.assertFalse(self.lock.exists())

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.lock = self.root / "state" / "upload.lock"
        self.input = self.root / "image.png"
        with Image.new("RGB", (60, 40), "gray") as image:
            image.save(self.input)

    def prepare(self, client, **overrides):
        args = dict(input_path=self.input, entity_type="work", entity_id=ENTITY, purpose="cover", source_type="manual",
                    source_url=None, reason="synthetic reviewed image", position=0, is_primary=True, confidence=1.0,
                    asset_id=ASSET, site_id="default", backup_root=self.root / "backup",
                    public_base_url="https://media.example.test", object_store=store_for(client, self.lock))
        args.update(overrides)
        return prepare_manifest(**args)

    def test_unknown_put_retains_matching_backup_and_fence_but_does_not_block_deletion(self):
        client = MemoryS3(timeout_on="w640")
        with self.assertRaises(ReadTimeoutError):
            self.prepare(client)
        self.assertEqual(len(client.objects), 1)
        self.assertEqual(len(list((self.root / "backup").rglob("*.webp"))), 1)
        pending = upload_status(self.lock)["pending"]
        self.assertEqual(len(pending["objects"]), 3)
        with self.assertRaises(UploadRecoveryRequired):
            self.prepare(client)
        self.assertEqual(len(client.puts), 3)
        key = next(iter(client.objects))[1]
        store_for(client).delete_and_verify(key)
        self.assertFalse(client.objects)
        self.assertEqual(upload_status(self.lock)["pending"], pending, "DELETE does not prove a timed-out writer stopped")

    def test_failed_rollback_keeps_all_unconfirmed_backups(self):
        client = MemoryS3(timeout_on="w640", fail_delete=True)
        with self.assertRaises(ReadTimeoutError):
            self.prepare(client)
        self.assertEqual(len(client.objects), 3)
        self.assertEqual(len(list((self.root / "backup").rglob("*.webp"))), 3)
        self.assertEqual(upload_status(self.lock)["status"], "review_required")

    def test_collision_never_deletes_or_overwrites_the_preexisting_remote_object(self):
        client = MemoryS3()
        existing = {"Body": b"existing", "Metadata": {"sha256": hashlib.sha256(b"existing").hexdigest()}}
        client.objects["private", KEY] = existing
        with self.assertRaises(ClientError):
            self.prepare(client)
        self.assertEqual(client.objects["private", KEY], existing)
        self.assertFalse(client.deletes)
        self.assertEqual(upload_status(self.lock)["status"], "review_required")

    def test_durable_manifest_is_part_of_admission_and_never_overwrites(self):
        client = MemoryS3()
        path = self.root / "manifest.json"
        result = self.prepare(client, manifest_path=path)
        self.assertEqual(json.loads(path.read_text()), result)
        self.assertEqual(upload_status(self.lock)["status"], "idle")
        with self.assertRaises(MediaRejected):
            self.prepare(client, manifest_path=path)
        self.assertEqual(len(client.puts), 4)
        with self.assertRaises(MediaRejected):
            _write_manifest(path, {"overwrite": True})
        self.assertEqual(json.loads(path.read_text()), result)
        self.assertFalse(list(self.root.glob("*.tmp")))

    def test_manifest_write_failure_keeps_remote_objects_backups_and_fence(self):
        client = MemoryS3()
        with patch("media_worker.pipeline._write_manifest", side_effect=OSError("disk full")), self.assertRaises(OSError):
            self.prepare(client, manifest_path=self.root / "manifest.json")
        self.assertEqual(len(client.objects), 4)
        self.assertEqual(len(list((self.root / "backup").rglob("*.webp"))), 4)
        self.assertEqual(len(upload_status(self.lock)["pending"]["objects"]), 4)

    def test_capacity_and_invalid_usage_fail_before_any_backup_or_sdk_write(self):
        for usage in (0, -1, True, None):
            with self.subTest(usage=usage), patch.object(S3ObjectStore, "usage_bytes", return_value=usage):
                client = MemoryS3()
                with self.assertRaises(MediaRejected):
                    self.prepare(client, storage_limit_bytes=1)
                self.assertFalse(client.puts)
                self.assertFalse((self.root / "backup").exists())
                self.assertEqual(upload_status(self.lock)["status"], "idle")

    def test_journal_failure_and_writes_without_admission_never_dispatch_sdk_put(self):
        client = MemoryS3()
        store = store_for(client, self.lock)
        args = dict(content_type="image/webp", public=False, sha256="a" * 64)
        with self.assertRaises(UploadGuardError):
            store.put(KEY, b"abc", **args)
        with patch("media_worker.upload_guard._atomic_json", side_effect=OSError("disk full")), self.assertRaises(OSError):
            with store.upload_guard():
                store.put(KEY, b"abc", **args)
        self.assertFalse(client.puts)
        self.assertEqual(upload_status(self.lock)["status"], "idle")
        with self.assertRaises(ValueError):
            with store.upload_guard():
                store.put(KEY, b"abc", content_type="image/webp", public=True, sha256="a" * 64)
        self.assertFalse(client.puts)


class UploadCLITests(unittest.TestCase):
    def test_delivery_stop_keeps_local_inspection_and_status_available_without_sdk(self):
        with tempfile.TemporaryDirectory() as directory, patch("media_worker.cli.S3ObjectStore") as factory:
            root = Path(directory)
            source = root / "image.png"
            with Image.new("RGB", (3, 3), "gray") as image:
                image.save(source)
            with patch.dict("os.environ", {"MEDIA_DELIVERY_MODE": "default_only"}, clear=True), redirect_stdout(io.StringIO()):
                self.assertEqual(cli.main(["inspect", "--input", str(source)]), 0)
                self.assertEqual(cli.main(["upload-status", "--upload-lock-file", str(root / "upload.lock")]), 0)
            factory.assert_not_called()

    def test_default_only_and_invalid_mode_refuse_prepare_before_sdk_and_files(self):
        with tempfile.TemporaryDirectory() as directory, patch("media_worker.cli.S3ObjectStore") as factory:
            root = Path(directory)
            for mode in ("default_only", "typo", ""):
                with patch.dict("os.environ", {"MEDIA_DELIVERY_MODE": mode}, clear=True), redirect_stderr(io.StringIO()):
                    self.assertEqual(cli.main(self.prepare_args(root)), 2)
                self.assertFalse(list(root.iterdir()))
            factory.assert_not_called()

    def call_cli(self, args):
        stdout, stderr = io.StringIO(), io.StringIO()
        with redirect_stdout(stdout), redirect_stderr(stderr), patch.dict("os.environ", {}, clear=True):
            result = cli.main(args)
        return result, stdout.getvalue(), stderr.getvalue()

    def prepare_args(self, root):
        return ["prepare", "--input", str(root / "input.png"), "--entity-type", "work", "--entity-id", ENTITY,
                "--purpose", "cover", "--reason", "synthetic review", "--backup-root", str(root / "backup"),
                "--output", str(root / "manifest.json"), "--private-bucket", "private", "--public-bucket", "public",
                "--public-base-url", "https://media.example.test"]

    def test_missing_or_relative_lock_fails_before_sdk_creation(self):
        with tempfile.TemporaryDirectory() as directory, patch("media_worker.cli.S3ObjectStore") as factory:
            args = self.prepare_args(Path(directory))
            for extra in ([], ["--upload-lock-file", "relative.lock"]):
                code, output, error = self.call_cli(args + extra)
                self.assertEqual(code, 2)
                self.assertEqual(output, "")
                self.assertEqual(json.loads(error)["code"], "MEDIA_REJECTED")
            factory.assert_not_called()

    def test_status_and_exact_ack_are_local_only(self):
        with tempfile.TemporaryDirectory() as directory, patch("media_worker.cli.S3ObjectStore") as factory:
            lock = Path(directory) / "upload.lock"
            with self.assertRaises(OSError):
                with upload_admission(lock) as journal:
                    journal.before_put(KEY, 3, "a" * 64)
                    raise OSError("uncertain")
            code, output, error = self.call_cli(["upload-status", "--upload-lock-file", str(lock)])
            self.assertEqual((code, error), (0, ""))
            pending = json.loads(output)["pending"]
            args = ["acknowledge-upload", "--upload-lock-file", str(lock), "--run-id", pending["run_id"], "--reason", "storage reviewed"]
            self.assertEqual(self.call_cli(args)[0], 2)
            code, output, error = self.call_cli(args + ["--confirm-storage-reconciled"])
            self.assertEqual((code, error), (0, ""))
            self.assertEqual(json.loads(output)["status"], "manual_review_acknowledged")
            self.assertEqual(self.call_cli(args + ["--confirm-storage-reconciled"])[0], 2)
            factory.assert_not_called()

    def test_sdk_transport_errors_are_structured_and_redacted(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            args = self.prepare_args(root) + ["--upload-lock-file", str(root / "upload.lock")]
            with patch("media_worker.cli.S3ObjectStore", side_effect=ReadTimeoutError(endpoint_url="https://secret.invalid")):
                code, output, error = self.call_cli(args)
            self.assertEqual((code, output), (2, ""))
            self.assertEqual(json.loads(error)["code"], "MEDIA_STORAGE_UNAVAILABLE")
            self.assertNotIn("secret", error)
            self.assertNotIn("Traceback", error)
