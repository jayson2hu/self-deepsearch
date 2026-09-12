from __future__ import annotations

import hashlib
import hmac
import json
import re
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Callable, Protocol, cast
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

from .upload_control import UploadControl
from .upload_control_http import PATHS as CONTROL_PATHS
from .upload_control_http import handle_control

MAX_BODY_BYTES = 16 * 1024
MAX_RECONCILE_BODY_BYTES = 16 * 1024 * 1024
MAX_RECONCILE_OBJECTS = 10_000
MAX_RECONCILE_SAMPLE_BYTES = 8 * 1024  # Per category; six categories fit the Go 64 KiB report limit.
MAX_CLOCK_SKEW_SECONDS = 300
SIGNATURE = re.compile(r"^v1=([a-f0-9]{64})$")


class DeletingStore(Protocol):
    def delete_and_verify(self, key: str) -> None: ...


class CachePurger(Protocol):
    def purge(self, public_url: str) -> None: ...


class ReconcilingStore(Protocol):
    def inspect(self, key: str) -> tuple[int, str] | None: ...
    def list_media_keys(self) -> list[tuple[str, str]]: ...


class PurgeHTTPResponse(Protocol):
    status: int

    def read(self, amount: int = -1) -> bytes: ...
    def __enter__(self) -> PurgeHTTPResponse: ...
    def __exit__(self, exc_type: object, exc_value: object, traceback: object) -> None: ...


class CloudflarePurger:
    def __init__(
        self,
        *,
        zone_id: str,
        api_token: str,
        opener: Callable[..., PurgeHTTPResponse] = cast(Callable[..., PurgeHTTPResponse], urlopen),
    ) -> None:
        if not re.fullmatch(r"[A-Za-z0-9_-]{10,64}", zone_id) or len(api_token.strip()) < 20:
            raise ValueError("Cloudflare purge configuration is invalid")
        self.endpoint = f"https://api.cloudflare.com/client/v4/zones/{zone_id}/purge_cache"
        self.api_token = api_token.strip()
        self.opener = opener

    def purge(self, public_url: str) -> None:
        body = json.dumps({"files": [public_url]}, separators=(",", ":")).encode()
        request = Request(self.endpoint, data=body, method="POST", headers={
            "Authorization": f"Bearer {self.api_token}",
            "Content-Type": "application/json",
            "Accept": "application/json",
        })
        try:
            with self.opener(request, timeout=10) as response:
                result = json.loads(response.read(4096))
                if response.status < 200 or response.status >= 300 or not isinstance(result, dict) or result.get("success") is not True:
                    raise OSError("Cloudflare cache purge was not accepted")
        except (HTTPError, URLError, TimeoutError, json.JSONDecodeError) as exc:
            raise OSError("Cloudflare cache purge failed") from exc


class DeleteService:
    def __init__(self, *, store: DeletingStore, backup_root: Path, purger: CachePurger | None = None) -> None:
        self.store = store
        self.backup_root = backup_root.resolve()
        self.purger = purger

    def delete(self, payload: object) -> dict[str, str]:
        if not isinstance(payload, dict) or set(payload) != {"event_id", "storage_scope", "storage_key", "backup_path", "public_url"}:
            raise ValueError("delete payload fields are invalid")
        uuid.UUID(str(payload["event_id"]))
        storage_key = _relative_path(str(payload["storage_key"]), "storage key")
        storage_scope = str(payload["storage_scope"])
        backup_path = _relative_path(str(payload["backup_path"]), "backup path")
        public_url = payload["public_url"]
        if storage_key != backup_path:
            raise ValueError("storage key and backup path must identify the same generated object")
        if (storage_scope == "private") != storage_key.startswith("media-master/") or storage_scope not in {"private", "public"}:
            raise ValueError("storage scope does not match object key")
        if storage_scope == "private":
            if public_url is not None:
                raise ValueError("private object cannot have a public URL")
        else:
            parsed = urlparse(str(public_url))
            if parsed.scheme != "https" or not parsed.netloc or not parsed.path.endswith("/" + storage_key):
                raise ValueError("public URL does not match object key")
            if self.purger is None:
                raise OSError("Cloudflare cache purger is not configured")
        destination = (self.backup_root / Path(*backup_path.split("/"))).resolve()
        if self.backup_root not in destination.parents:
            raise ValueError("backup path escapes the configured root")
        self.store.delete_and_verify(storage_key)
        if storage_scope == "public":
            purger = self.purger
            if purger is None:
                raise OSError("Cloudflare cache purger is not configured")
            purger.purge(str(public_url))
        destination.unlink(missing_ok=True)
        if destination.exists():
            raise OSError("backup remains accessible after deletion")
        return {
            "status": "deleted", "event_id": str(payload["event_id"]),
            "storage_scope": storage_scope, "storage_key": storage_key,
        }


class ReconcileService:
    def __init__(self, *, store: ReconcilingStore, backup_root: Path) -> None:
        self.store = store
        self.backup_root = backup_root.resolve()

    def reconcile(self, payload: object) -> dict[str, object]:
        if not isinstance(payload, dict) or set(payload) != {"run_id", "objects"}:
            raise ValueError("reconciliation payload fields are invalid")
        uuid.UUID(str(payload["run_id"]))
        objects = payload["objects"]
        if not isinstance(objects, list) or len(objects) > MAX_RECONCILE_OBJECTS:
            raise ValueError("reconciliation object count is invalid")
        expected: set[tuple[str, str]] = set()
        issues: dict[str, list[str]] = {
            "missing_s3": [], "corrupt_s3": [], "missing_backup": [], "corrupt_backup": [], "orphan_s3": [], "orphan_backup": [],
        }
        for item in objects:
            if not isinstance(item, dict) or set(item) != {"storage_scope", "storage_key", "backup_path", "sha256", "byte_size"}:
                raise ValueError("reconciliation object fields are invalid")
            scope, key = str(item["storage_scope"]), _relative_path(str(item["storage_key"]), "storage key")
            backup_path = _relative_path(str(item["backup_path"]), "backup path")
            digest, byte_size = str(item["sha256"]), item["byte_size"]
            if scope not in {"private", "public"} or (scope == "private") != key.startswith("media-master/"):
                raise ValueError("reconciliation storage scope is invalid")
            if key != backup_path or not re.fullmatch(r"[a-f0-9]{64}", digest) or not isinstance(byte_size, int) or not 1 <= byte_size <= 10 * 1024 * 1024:
                raise ValueError("reconciliation object evidence is invalid")
            location = (scope, key)
            if location in expected:
                raise ValueError("reconciliation object is duplicated")
            expected.add(location)
            remote = self.store.inspect(key)
            if remote is None:
                issues["missing_s3"].append(key)
            elif remote != (byte_size, digest):
                issues["corrupt_s3"].append(key)
            backup = (self.backup_root / Path(*backup_path.split("/"))).resolve()
            if self.backup_root not in backup.parents:
                raise ValueError("backup path escapes the configured root")
            if not backup.is_file():
                issues["missing_backup"].append(key)
            elif backup.stat().st_size != byte_size or _sha256_file(backup) != digest:
                issues["corrupt_backup"].append(key)
        actual = set(self.store.list_media_keys())
        issues["orphan_s3"] = sorted(key for scope, key in actual - expected)
        actual_backups: set[tuple[str, str]] = set()
        for scope, prefix in (("private", "media-master"), ("public", "media-public")):
            directory = self.backup_root / prefix
            if directory.is_dir():
                actual_backups.update((scope, path.relative_to(self.backup_root).as_posix()) for path in directory.rglob("*.webp") if path.is_file())
        issues["orphan_backup"] = sorted(key for scope, key in actual_backups - expected)
        counts = {name + "_count": len(values) for name, values in issues.items()}
        return {
            "run_id": str(payload["run_id"]), "expected_count": len(expected),
            **counts, "issue_samples": {name: _bounded_issue_samples(values) for name, values in issues.items()},
        }


def _bounded_issue_samples(values: list[str]) -> list[str]:
    samples: list[str] = []
    used = 0
    for value in values:
        # Match Handler._json's ASCII escaping, including quotes/separators.
        size = len(json.dumps(value, ensure_ascii=True).encode("utf-8")) + 1
        if used + size > MAX_RECONCILE_SAMPLE_BYTES:
            continue
        samples.append(value)
        used += size
        if len(samples) == 50:
            break
    return samples


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _relative_path(value: str, field: str) -> str:
    value = value.strip()
    if not value or len(value) > 2048 or "\\" in value or value.startswith("/"):
        raise ValueError(f"{field} is invalid")
    segments = value.split("/")
    if any(segment in {"", ".", ".."} for segment in segments):
        raise ValueError(f"{field} is invalid")
    if not (value.startswith("media-master/") or value.startswith("media-public/")):
        raise ValueError(f"{field} prefix is invalid")
    return value


def signature(secret: bytes, timestamp: str, event_id: str, body: bytes) -> str:
    message = timestamp.encode() + b"\n" + event_id.encode() + b"\n" + body
    return "v1=" + hmac.new(secret, message, hashlib.sha256).hexdigest()


def handler(service: DeleteService, reconcile_service: ReconcileService, secret: str, now: Callable[[], float] = time.time,
            *, upload_control: UploadControl | None = None, control_secret: str = "") -> type[BaseHTTPRequestHandler]:
    secret_bytes = secret.strip().encode()
    if len(secret_bytes) < 32:
        raise ValueError("MEDIA_HMAC_SECRET must contain at least 32 characters")
    if upload_control is not None and (len(control_secret.strip().encode()) < 32 or control_secret.strip() == secret.strip()):
        raise ValueError("upload control requires a separate secret of at least 32 bytes")

    class Handler(BaseHTTPRequestHandler):
        server_version = "self-deepsearch-media/1"

        def do_GET(self) -> None:  # noqa: N802
            if self.path == "/health/live":
                self._json(200, {"status": "ok", "service": "media-python"})
            else:
                self._json(404, {"error": "not_found"})

        def do_POST(self) -> None:  # noqa: N802
            if self.path in CONTROL_PATHS:
                handle_control(self, upload_control, control_secret, now)
                return
            if self.path not in {"/v1/delete", "/v1/reconcile"}:
                self._json(404, {"error": "not_found"})
                return
            try:
                if self.headers.get("Transfer-Encoding"):
                    self._json(400, {"error": "invalid_request"})
                    return
                length = int(self.headers.get("Content-Length", "-1"))
                maximum = MAX_RECONCILE_BODY_BYTES if self.path == "/v1/reconcile" else MAX_BODY_BYTES
                if length < 1 or length > maximum:
                    self._json(413, {"error": "body_too_large"})
                    return
                body = self.rfile.read(length)
                timestamp = self.headers.get("X-SD-Timestamp", "")
                event_id = self.headers.get("X-SD-Event-ID", "")
                supplied = self.headers.get("X-SD-Signature", "")
                match = SIGNATURE.fullmatch(supplied)
                if not match or not timestamp.isdigit() or abs(now() - int(timestamp)) > MAX_CLOCK_SKEW_SECONDS:
                    self._json(401, {"error": "invalid_signature"})
                    return
                expected = signature(secret_bytes, timestamp, event_id, body)
                if not hmac.compare_digest(expected, supplied):
                    self._json(401, {"error": "invalid_signature"})
                    return
                payload = json.loads(body)
                id_field = "run_id" if self.path == "/v1/reconcile" else "event_id"
                if not isinstance(payload, dict) or payload.get(id_field) != event_id:
                    raise ValueError("signed id does not match request header")
                if self.path == "/v1/reconcile":
                    self._json(200, reconcile_service.reconcile(payload))
                else:
                    self._json(200, service.delete(payload))
            except (UnicodeDecodeError, json.JSONDecodeError, ValueError):
                self._json(400, {"error": "invalid_request"})
            except OSError:
                self._json(503, {"error": "reconcile_unavailable" if self.path == "/v1/reconcile" else "delete_unavailable"})

        def log_message(self, format: str, *args: object) -> None:
            return

        def _json(self, status: int, value: object) -> None:
            body = json.dumps(value, separators=(",", ":")).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)

    return Handler


def serve(*, host: str, port: int, service: DeleteService, reconcile_service: ReconcileService, secret: str,
          upload_control: UploadControl | None = None, control_secret: str = "") -> None:
    server = ThreadingHTTPServer((host, port), handler(service, reconcile_service, secret, upload_control=upload_control, control_secret=control_secret))
    server.serve_forever()
