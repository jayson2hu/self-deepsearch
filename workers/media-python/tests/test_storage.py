from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

from media_worker.storage import S3ObjectStore  # noqa: E402


class FakeClient:
    def __init__(self) -> None:
        self.puts: list[dict[str, object]] = []
        self.heads: list[tuple[str, str]] = []
        self.lists: list[tuple[str, str]] = []
        self.objects: dict[str, list[int]] = {"private": [], "public": []}

    def put_object(self, **values: object) -> None:
        self.puts.append(values)

    def head_object(self, *, Bucket: str, Key: str) -> dict[str, object]:  # noqa: N803
        self.heads.append((Bucket, Key))
        return {"ContentLength": 3, "Metadata": {"sha256": "a" * 64}}

    def get_paginator(self, operation: str) -> "FakePaginator":
        if operation != "list_objects_v2":
            raise AssertionError(f"unexpected paginator: {operation}")
        return FakePaginator(self)

    def list_objects_v2(self, *, Bucket: str, MaxKeys: int) -> dict[str, object]:  # noqa: N803
        self.lists.append((Bucket, ""))
        if MaxKeys != 1000:
            raise AssertionError("usage listing must be bounded")
        return {"IsTruncated": False, "Contents": [{"Key": f"outside-media-prefix/{index}.bin", "Size": size}
                                                    for index, size in enumerate(self.objects[Bucket])]}

    def list_multipart_uploads(self, *, Bucket: str, MaxUploads: int) -> dict[str, object]:  # noqa: N803
        self.lists.append((Bucket, "multipart"))
        if MaxUploads != 1000:
            raise AssertionError("multipart listing must be bounded")
        return {"IsTruncated": False, "Uploads": []}


class FakePaginator:
    def __init__(self, client: FakeClient) -> None:
        self.client = client

    def paginate(self, *, Bucket: str, Prefix: str) -> list[dict[str, object]]:  # noqa: N803
        self.client.lists.append((Bucket, Prefix))
        return [{"Contents": [{"Size": size} for size in self.client.objects[Bucket]]}]


class StorageScopeTests(unittest.TestCase):
    def test_master_and_public_derivative_use_different_buckets(self) -> None:
        client = FakeClient()
        with tempfile.TemporaryDirectory() as directory:
            store = S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=client,
                                  upload_lock_file=Path(directory) / "upload.lock")
            with store.upload_guard():
                store.put("media-master/a/master.webp", b"abc", content_type="image/webp", public=False, sha256="a" * 64)
                store.put("media-public/a/w320.webp", b"abc", content_type="image/webp", public=True, sha256="a" * 64)
                store.verify("media-master/a/master.webp", byte_size=3, sha256="a" * 64)
                store.verify("media-public/a/w320.webp", byte_size=3, sha256="a" * 64)
        self.assertEqual([item["Bucket"] for item in client.puts], ["private", "public"])
        self.assertTrue(all(item["IfNoneMatch"] == "*" for item in client.puts))
        self.assertEqual(client.heads, [("private", "media-master/a/master.webp"), ("public", "media-public/a/w320.webp")])

    def test_rejects_same_bucket_for_private_and_public_objects(self) -> None:
        with self.assertRaises(ValueError):
            S3ObjectStore(endpoint=None, region="auto", private_bucket="same", public_bucket="same", client=FakeClient())

    def test_usage_sums_private_and_public_media_objects(self) -> None:
        client = FakeClient()
        client.objects = {"private": [7, 11], "public": [13, 17]}
        store = S3ObjectStore(endpoint=None, region="auto", private_bucket="private", public_bucket="public", client=client)
        self.assertEqual(store.usage_bytes(), 48)
        self.assertEqual(client.lists, [("private", ""), ("private", "multipart"), ("public", ""), ("public", "multipart")])


if __name__ == "__main__":
    unittest.main()
