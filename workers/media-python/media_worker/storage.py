from __future__ import annotations

import os
from contextlib import contextmanager, nullcontext
from pathlib import Path
from threading import get_ident
from typing import Any, Iterator

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError

from .upload_control import configured_control
from .upload_guard import UploadGuardError, UploadJournal, upload_admission, validate_lock_path

MAX_USAGE_PAGES = 1000
MAX_MULTIPART_UPLOAD_PAGES = 1000
MAX_MULTIPART_PART_PAGES = 1000
MAX_USAGE_REQUESTS = 256
MAX_USAGE_BYTES = (1 << 63) - 1


class _UsageRequestBudget:
    def __init__(self, limit: int) -> None:
        self.remaining = limit

    def consume(self) -> None:
        if self.remaining < 1:
            raise OSError("S3 capacity scan exceeds the bounded request budget")
        self.remaining -= 1


class S3ObjectStore:
    def __init__(self, *, endpoint: str | None, region: str, private_bucket: str, public_bucket: str, client: Any | None = None,
                 upload_lock_file: Path | None = None) -> None:
        if not private_bucket or not public_bucket or private_bucket == public_bucket:
            raise ValueError("private and public media buckets must be different")
        self.private_bucket = private_bucket
        self.public_bucket = public_bucket
        self.upload_lock_file = upload_lock_file
        self.upload_control = configured_control(upload_lock_file)
        self._upload_owner: int | None = None
        self._upload_journal: UploadJournal | None = None
        self.client = client or boto3.client(
            "s3", endpoint_url=endpoint, region_name=region,
            config=Config(signature_version="s3v4", retries={"max_attempts": 3, "mode": "standard"}),
        )

    @contextmanager
    def upload_guard(self) -> Iterator[None]:
        if os.getenv("MEDIA_DELIVERY_MODE", "normal") != "normal":
            raise UploadGuardError("MEDIA_DELIVERY_MODE disables new media uploads")
        path = validate_lock_path(self.upload_lock_file)
        with upload_admission(path) as journal:
            if self.upload_control is not None:
                self.upload_control.require_enabled()
            self._upload_owner, self._upload_journal = get_ident(), journal
            try:
                yield
            finally:
                self._upload_owner, self._upload_journal = None, None

    def usage_bytes(self) -> int:
        total = 0
        request_budget = _UsageRequestBudget(MAX_USAGE_REQUESTS)
        # Billable current objects outside our managed prefixes still consume
        # these two dedicated buckets. Incomplete multipart parts are not
        # returned by ListObjectsV2, so count them separately before admitting
        # a new batch. Prefix-only/current-object-only accounting undercounts.
        for bucket in (self.private_bucket, self.public_bucket):
            total = self._current_object_usage_bytes(bucket, total, request_budget)
            total = self._multipart_usage_bytes(bucket, total, request_budget)
        return total

    def _current_object_usage_bytes(self, bucket: str, total: int, request_budget: _UsageRequestBudget) -> int:
        token: str | None = None
        seen_tokens: set[str] = set()
        seen_keys: set[str] = set()
        try:
            for _ in range(MAX_USAGE_PAGES):
                arguments: dict[str, object] = {"Bucket": bucket, "MaxKeys": 1000}
                if token is not None:
                    arguments["ContinuationToken"] = token
                request_budget.consume()
                with self.upload_control.operation() if self.upload_control is not None else nullcontext():
                    page = self.client.list_objects_v2(**arguments)
                if not isinstance(page, dict) or type(page.get("IsTruncated")) is not bool:
                    raise OSError("S3 usage listing lacks a valid completion marker")
                contents = page.get("Contents", [])
                if not isinstance(contents, list) or len(contents) > 1000:
                    raise OSError("S3 usage listing contains invalid contents")
                if "KeyCount" in page and (type(page["KeyCount"]) is not int or page["KeyCount"] != len(contents)):
                    raise OSError("S3 usage listing count does not match its contents")
                for item in contents:
                    if not isinstance(item, dict) or not isinstance(item.get("Key"), str) or not 1 <= len(item["Key"]) <= 1024:
                        raise OSError("S3 usage listing contains an invalid object key")
                    if item["Key"] in seen_keys:
                        raise OSError("S3 usage listing contains repeated objects or an invalid size")
                    seen_keys.add(item["Key"])
                    total = self._add_usage_bytes(
                        total, item.get("Size"), "S3 usage listing contains repeated objects or an invalid size",
                    )
                if not page["IsTruncated"]:
                    if page.get("NextContinuationToken"):
                        raise OSError("S3 usage listing has contradictory pagination")
                    return total
                token = page.get("NextContinuationToken")
                if not isinstance(token, str) or not 1 <= len(token) <= 4096 or token in seen_tokens:
                    raise OSError("S3 usage listing has missing or repeated continuation tokens")
                seen_tokens.add(token)
            raise OSError("S3 usage listing exceeds the bounded page limit")
        except ClientError as exc:
            raise OSError("S3 media usage listing failed") from exc

    def _multipart_usage_bytes(self, bucket: str, total: int, request_budget: _UsageRequestBudget) -> int:
        key_marker: str | None = None
        upload_marker: str | None = None
        seen_markers: set[tuple[str, str]] = set()
        seen_uploads: set[tuple[str, str]] = set()
        try:
            for _ in range(MAX_MULTIPART_UPLOAD_PAGES):
                arguments: dict[str, object] = {"Bucket": bucket, "MaxUploads": 1000}
                if key_marker is not None:
                    arguments["KeyMarker"] = key_marker
                if upload_marker is not None:
                    arguments["UploadIdMarker"] = upload_marker
                request_budget.consume()
                with self.upload_control.operation() if self.upload_control is not None else nullcontext():
                    page = self.client.list_multipart_uploads(**arguments)
                if not isinstance(page, dict) or type(page.get("IsTruncated")) is not bool:
                    raise OSError("S3 multipart listing lacks a valid completion marker")
                uploads = page.get("Uploads", [])
                if not isinstance(uploads, list) or len(uploads) > 1000:
                    raise OSError("S3 multipart listing contains invalid uploads")
                for item in uploads:
                    if not isinstance(item, dict):
                        raise OSError("S3 multipart listing contains an invalid upload")
                    key, upload_id = item.get("Key"), item.get("UploadId")
                    if not isinstance(key, str) or not 1 <= len(key) <= 1024 or not isinstance(upload_id, str) or not 1 <= len(upload_id) <= 4096:
                        raise OSError("S3 multipart listing contains an invalid upload")
                    identity = key, upload_id
                    if identity in seen_uploads:
                        raise OSError("S3 multipart listing contains a repeated upload")
                    seen_uploads.add(identity)
                    total = self._multipart_part_usage_bytes(bucket, key, upload_id, total, request_budget)
                next_key, next_upload = page.get("NextKeyMarker"), page.get("NextUploadIdMarker")
                if not page["IsTruncated"]:
                    if next_key or next_upload:
                        raise OSError("S3 multipart listing has contradictory pagination")
                    return total
                if not isinstance(next_key, str) or not 1 <= len(next_key) <= 1024:
                    raise OSError("S3 multipart listing has an invalid next marker")
                if next_upload is not None and (not isinstance(next_upload, str) or not 1 <= len(next_upload) <= 4096):
                    raise OSError("S3 multipart listing has an invalid next marker")
                marker = next_key, next_upload or ""
                if marker in seen_markers:
                    raise OSError("S3 multipart listing has a repeated next marker")
                seen_markers.add(marker)
                key_marker, upload_marker = next_key, next_upload
            raise OSError("S3 multipart listing exceeds the bounded page limit")
        except ClientError as exc:
            raise OSError("S3 multipart usage listing failed") from exc

    def _multipart_part_usage_bytes(
        self, bucket: str, key: str, upload_id: str, total: int, request_budget: _UsageRequestBudget,
    ) -> int:
        marker: int | None = None
        seen_markers: set[int] = set()
        seen_parts: set[int] = set()
        try:
            for _ in range(MAX_MULTIPART_PART_PAGES):
                arguments: dict[str, object] = {"Bucket": bucket, "Key": key, "UploadId": upload_id, "MaxParts": 1000}
                if marker is not None:
                    arguments["PartNumberMarker"] = marker
                request_budget.consume()
                with self.upload_control.operation() if self.upload_control is not None else nullcontext():
                    page = self.client.list_parts(**arguments)
                if not isinstance(page, dict) or type(page.get("IsTruncated")) is not bool:
                    raise OSError("S3 multipart part listing lacks a valid completion marker")
                parts = page.get("Parts", [])
                if not isinstance(parts, list) or len(parts) > 1000:
                    raise OSError("S3 multipart part listing contains invalid parts")
                page_numbers: list[int] = []
                for item in parts:
                    if not isinstance(item, dict) or type(item.get("PartNumber")) is not int or not 1 <= item["PartNumber"] <= 10000:
                        raise OSError("S3 multipart part listing contains an invalid part")
                    number = item["PartNumber"]
                    if number in seen_parts:
                        raise OSError("S3 multipart part listing contains a repeated part")
                    seen_parts.add(number)
                    page_numbers.append(number)
                    total = self._add_usage_bytes(
                        total, item.get("Size"), "S3 multipart part listing contains an invalid size",
                    )
                next_marker = page.get("NextPartNumberMarker")
                if not page["IsTruncated"]:
                    if next_marker not in (None, 0):
                        raise OSError("S3 multipart part listing has contradictory pagination")
                    return total
                if type(next_marker) is not int or not 1 <= next_marker <= 10000 or next_marker in seen_markers:
                    raise OSError("S3 multipart part listing has an invalid or repeated next marker")
                if not page_numbers or next_marker != max(page_numbers) or (marker is not None and next_marker <= marker):
                    raise OSError("S3 multipart part listing has an inconsistent next marker")
                seen_markers.add(next_marker)
                marker = next_marker
            raise OSError("S3 multipart part listing exceeds the bounded page limit")
        except ClientError as exc:
            raise OSError("S3 multipart part usage listing failed") from exc

    @staticmethod
    def _add_usage_bytes(total: int, value: object, message: str) -> int:
        if type(value) is not int or value < 0 or value > MAX_USAGE_BYTES - total:
            raise OSError(message)
        return total + value

    def put(self, key: str, body: bytes, *, content_type: str, public: bool, sha256: str) -> None:
        if self._upload_owner != get_ident() or self._upload_journal is None:
            raise UploadGuardError("media writes require shared upload admission")
        if self._bucket(key) != (self.public_bucket if public else self.private_bucket):
            raise ValueError("media public flag does not match its storage key")
        with self.upload_control.operation() if self.upload_control is not None else nullcontext():
            self._upload_journal.before_put(key, len(body), sha256)
            cache_control = "public, max-age=31536000, immutable" if public else "no-store"
            self.client.put_object(Bucket=self.public_bucket if public else self.private_bucket, Key=key, Body=body, ContentType=content_type,
                                   CacheControl=cache_control, Metadata={"sha256": sha256}, IfNoneMatch="*")

    def verify(self, key: str, *, byte_size: int, sha256: str) -> None:
        response = self.client.head_object(Bucket=self._bucket(key), Key=key)
        metadata = {str(name).lower(): str(value).lower() for name, value in response.get("Metadata", {}).items()}
        if int(response.get("ContentLength", -1)) != byte_size or metadata.get("sha256") != sha256:
            raise ValueError(f"uploaded object verification failed for {key}")

    def delete(self, key: str) -> None:
        self.client.delete_object(Bucket=self._bucket(key), Key=key)

    def delete_and_verify(self, key: str) -> None:
        try:
            self.delete(key)
            self.client.head_object(Bucket=self._bucket(key), Key=key)
        except ClientError as exc:
            code = str(exc.response.get("Error", {}).get("Code", ""))
            status = int(exc.response.get("ResponseMetadata", {}).get("HTTPStatusCode", 0))
            if status == 404 or code in {"404", "NoSuchKey", "NotFound"}:
                return
            raise OSError("S3 deletion verification failed") from exc
        raise OSError(f"object remains accessible after deletion: {key}")

    def _bucket(self, key: str) -> str:
        if key.startswith("media-master/"):
            return self.private_bucket
        if key.startswith("media-public/"):
            return self.public_bucket
        raise ValueError("media object key has an unsupported scope")

    def inspect(self, key: str) -> tuple[int, str] | None:
        try:
            response = self.client.head_object(Bucket=self._bucket(key), Key=key)
        except ClientError as exc:
            code = str(exc.response.get("Error", {}).get("Code", ""))
            status = int(exc.response.get("ResponseMetadata", {}).get("HTTPStatusCode", 0))
            if status == 404 or code in {"404", "NoSuchKey", "NotFound"}:
                return None
            raise OSError("S3 object inspection failed") from exc
        metadata = {str(name).lower(): str(value).lower() for name, value in response.get("Metadata", {}).items()}
        return int(response.get("ContentLength", -1)), metadata.get("sha256", "")

    def list_media_keys(self) -> list[tuple[str, str]]:
        result: list[tuple[str, str]] = []
        for scope, bucket, prefix in (
            ("private", self.private_bucket, "media-master/"),
            ("public", self.public_bucket, "media-public/"),
        ):
            paginator = self.client.get_paginator("list_objects_v2")
            try:
                for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
                    result.extend((scope, str(item["Key"])) for item in page.get("Contents", []))
            except ClientError as exc:
                raise OSError("S3 object listing failed") from exc
        return result
