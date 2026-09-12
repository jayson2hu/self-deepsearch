from __future__ import annotations

import hashlib
import io
import json
import os
import re
import sys
import uuid
import warnings
from contextlib import AbstractContextManager
from datetime import UTC, datetime
from pathlib import Path
from typing import Protocol
from urllib.parse import quote, urlparse

from PIL import Image, ImageOps, UnidentifiedImageError

MAX_INPUT_BYTES = 10 * 1024 * 1024
MAX_PIXELS = 40_000_000
MAX_DIMENSION = 20_000
DEFAULT_STORAGE_LIMIT_BYTES = 10 * 1024 * 1024 * 1024
RENDITIONS = (320, 640, 960)
TOOL_VERSION = "media-python/1"
UUID_PATTERN = re.compile(r"^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$", re.I)


class MediaRejected(ValueError):
    pass


class ObjectStore(Protocol):
    def upload_guard(self) -> AbstractContextManager[None]: ...
    def usage_bytes(self) -> int: ...
    def put(self, key: str, body: bytes, *, content_type: str, public: bool, sha256: str) -> None: ...
    def verify(self, key: str, *, byte_size: int, sha256: str) -> None: ...
    def delete_and_verify(self, key: str) -> None: ...


def _decode(path: Path) -> Image.Image:
    try:
        size = path.stat().st_size
    except OSError as exc:
        raise MediaRejected("input image cannot be read") from exc
    if size < 1 or size > MAX_INPUT_BYTES:
        raise MediaRejected("input image size is outside the allowed range")
    Image.MAX_IMAGE_PIXELS = MAX_PIXELS
    try:
        with warnings.catch_warnings():
            warnings.simplefilter("error", Image.DecompressionBombWarning)
            with Image.open(path) as candidate:
                detected_format = candidate.format
                candidate.verify()
            if detected_format not in {"JPEG", "PNG", "WEBP"}:
                raise MediaRejected("only decoded JPEG, PNG and WebP images are accepted")
            with Image.open(path) as candidate:
                if getattr(candidate, "n_frames", 1) != 1 or getattr(candidate, "is_animated", False):
                    raise MediaRejected("animated images are not accepted")
                candidate.load()
                if candidate.width < 1 or candidate.height < 1:
                    raise MediaRejected("image dimensions are invalid")
                if candidate.width > MAX_DIMENSION or candidate.height > MAX_DIMENSION or candidate.width * candidate.height > MAX_PIXELS:
                    raise MediaRejected("decoded image dimensions exceed the safety limit")
                normalized = ImageOps.exif_transpose(candidate)
                if normalized.mode in {"RGBA", "LA"} or "transparency" in normalized.info:
                    rgba = normalized.convert("RGBA")
                    background = Image.new("RGB", rgba.size, "white")
                    background.paste(rgba, mask=rgba.getchannel("A"))
                    rgba.close()
                    return background
                return normalized.convert("RGB")
    except (Image.DecompressionBombError, Image.DecompressionBombWarning, UnidentifiedImageError, OSError, SyntaxError) as exc:
        raise MediaRejected("image decoding or integrity verification failed") from exc


def inspect_image(path: Path) -> dict[str, object]:
    image = _decode(path)
    try:
        return {"path": path.name, "input_bytes": path.stat().st_size, "width": image.width, "height": image.height, "status": "decoded"}
    finally:
        image.close()


def _webp(image: Image.Image, width: int, *, lossless: bool) -> tuple[bytes, int, int]:
    output = image.copy()
    try:
        if output.width > width:
            height = max(1, round(output.height * width / output.width))
            resized = output.resize((width, height), Image.Resampling.LANCZOS)
            output.close()
            output = resized
        buffer = io.BytesIO()
        output.save(buffer, format="WEBP", lossless=lossless, quality=84, method=6, exif=b"", icc_profile=None)
        return buffer.getvalue(), output.width, output.height
    finally:
        output.close()


def _safe_segment(value: str, field: str) -> str:
    value = value.strip()
    if not value or len(value) > 100 or not re.fullmatch(r"[A-Za-z0-9_-]+", value):
        raise MediaRejected(f"{field} is invalid")
    return value


def _public_url(base: str, key: str) -> str:
    parsed = urlparse(base.strip())
    if parsed.scheme != "https" or not parsed.netloc or parsed.query or parsed.fragment or parsed.username:
        raise MediaRejected("public base URL must be an HTTPS origin or path without credentials, query or fragment")
    encoded_key = "/".join(quote(segment, safe="-_.~") for segment in key.split("/"))
    return base.rstrip("/") + "/" + encoded_key


def _write_backup(root: Path, key: str, body: bytes) -> Path:
    root = root.resolve()
    destination = (root / Path(*key.split("/"))).resolve()
    if root != destination and root not in destination.parents:
        raise MediaRejected("generated backup path escapes the configured root")
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.exists():
        raise MediaRejected("generated backup path already exists; object keys are immutable")
    temporary = destination.with_suffix(destination.suffix + f".{uuid.uuid4()}.tmp")
    try:
        with temporary.open("xb") as stream:
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.link(temporary, destination)
    except FileExistsError as exc:
        raise MediaRejected("generated backup path already exists; object keys are immutable") from exc
    finally:
        temporary.unlink(missing_ok=True)
    return destination


def prepare_manifest(
    *, input_path: Path, entity_type: str, entity_id: str, purpose: str, source_type: str,
    source_url: str | None, reason: str, position: int, is_primary: bool, confidence: float,
    asset_id: str | None, site_id: str, backup_root: Path, public_base_url: str,
    object_store: ObjectStore, storage_limit_bytes: int = DEFAULT_STORAGE_LIMIT_BYTES,
    manifest_path: Path | None = None,
) -> dict[str, object]:
    # Hold one shared admission from before decoding/capacity inspection until
    # every remote write is confirmed (or fenced for manual reconciliation).
    with object_store.upload_guard():
        if manifest_path is not None and manifest_path.exists():
            raise MediaRejected("output manifest already exists")
        result = _prepare_manifest(
            input_path=input_path, entity_type=entity_type, entity_id=entity_id, purpose=purpose,
            source_type=source_type, source_url=source_url, reason=reason, position=position,
            is_primary=is_primary, confidence=confidence, asset_id=asset_id, site_id=site_id,
            backup_root=backup_root, public_base_url=public_base_url, object_store=object_store,
            storage_limit_bytes=storage_limit_bytes,
        )
        # The CLI must durably preserve the handoff manifest before clearing
        # upload admission. A failed local handoff retains remote/backups and
        # the pending journal for operator reconciliation instead of reupload.
        if manifest_path is not None:
            _write_manifest(manifest_path, result)
        return result


def _write_manifest(path: Path, manifest: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + f".{uuid.uuid4()}.tmp")
    try:
        with temporary.open("x", encoding="utf-8") as stream:
            stream.write(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        try:
            os.link(temporary, path)
        except FileExistsError as exc:
            raise MediaRejected("output manifest already exists") from exc
        if sys.platform != "win32":
            fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
    finally:
        temporary.unlink(missing_ok=True)


def _prepare_manifest(
    *, input_path: Path, entity_type: str, entity_id: str, purpose: str, source_type: str,
    source_url: str | None, reason: str, position: int, is_primary: bool, confidence: float,
    asset_id: str | None, site_id: str, backup_root: Path, public_base_url: str,
    object_store: ObjectStore, storage_limit_bytes: int,
) -> dict[str, object]:
    if entity_type not in {"work", "performer"} or not UUID_PATTERN.fullmatch(entity_id):
        raise MediaRejected("entity identity is invalid")
    valid_association = (
        entity_type == "work" and purpose == "cover" and is_primary and position == 0
    ) or (
        entity_type == "work" and purpose == "gallery" and not is_primary and 1 <= position <= 3
    ) or (
        entity_type == "performer" and purpose == "avatar" and is_primary and position == 0
    )
    if not valid_association:
        raise MediaRejected("purpose, primary flag and position do not form a valid display slot")
    if not 0 <= confidence <= 1:
        raise MediaRejected("confidence is outside the allowed range")
    if storage_limit_bytes < 1:
        raise MediaRejected("media storage limit must be positive")
    if len(reason.strip()) < 2 or len(reason.strip()) > 1000:
        raise MediaRejected("reason must contain 2 to 1000 characters")
    if source_url:
        parsed_source = urlparse(source_url.strip())
        if parsed_source.scheme not in {"http", "https"} or not parsed_source.netloc:
            raise MediaRejected("source URL must be HTTP(S)")
    source_type = _safe_segment(source_type, "source type")
    site_id = _safe_segment(site_id, "site id")
    try:
        asset_id = str(uuid.UUID(asset_id)) if asset_id else str(uuid.uuid4())
    except ValueError as exc:
        raise MediaRejected("asset id must be a UUID") from exc

    image = _decode(input_path)
    uploaded: list[str] = []
    backups: dict[str, Path] = {}
    objects: list[dict[str, object]] = []
    try:
        variants: list[tuple[str, str, bytes, int, int, bool]] = []
        master_body, master_width, master_height = _webp(image, image.width, lossless=True)
        variants.append(("master", f"media-master/{asset_id}/v1/master.webp", master_body, master_width, master_height, False))
        plural = "works" if entity_type == "work" else "performers"
        for target_width in RENDITIONS:
            rendition = f"w{target_width}"
            body, width, height = _webp(image, target_width, lossless=False)
            key = f"media-public/{site_id}/{plural}/{entity_id}/{asset_id}/v1/{purpose}-{rendition}.webp"
            variants.append((rendition, key, body, width, height, True))
        for rendition, _, body, _, _, _ in variants:
            if not 1 <= len(body) <= MAX_INPUT_BYTES:
                raise MediaRejected(f"generated {rendition} object is outside the allowed size")
        current_usage = object_store.usage_bytes()
        if type(current_usage) is not int or current_usage < 0:
            raise MediaRejected("media storage usage must be a nonnegative integer")
        batch_bytes = sum(len(body) for _, _, body, _, _, _ in variants)
        if current_usage > storage_limit_bytes or batch_bytes > storage_limit_bytes - current_usage:
            raise MediaRejected(
                f"media storage limit would be exceeded ({current_usage} + {batch_bytes} > {storage_limit_bytes} bytes); no objects were uploaded"
            )
        for rendition, key, body, width, height, public in variants:
            if len(body) < 1 or len(body) > MAX_INPUT_BYTES:
                raise MediaRejected(f"generated {rendition} object is outside the allowed size")
            digest = hashlib.sha256(body).hexdigest()
            backup = _write_backup(backup_root, key, body)
            backups[key] = backup
            object_store.put(key, body, content_type="image/webp", public=public, sha256=digest)
            uploaded.append(key)
            object_store.verify(key, byte_size=len(body), sha256=digest)
            objects.append({
                "rendition": rendition, "storage_key": key,
                "storage_scope": "public" if public else "private",
                "backup_path": backup.relative_to(backup_root.resolve()).as_posix(),
                "public_url": _public_url(public_base_url, key) if public else None,
                "sha256": digest, "mime_type": "image/webp", "width": width,
                "height": height, "byte_size": len(body),
            })
    except Exception:
        for key in reversed(uploaded):
            try:
                object_store.delete_and_verify(key)
                backups[key].unlink(missing_ok=True)
            except Exception:
                # Retain the matching Beijing copy when deletion is not
                # confirmed. It is needed to inspect/recover the remote object.
                pass
        # A PUT without a success receipt might have committed. Keep that
        # object's backup too; do not infer absence from an SDK exception.
        raise
    finally:
        image.close()
    return {
        "asset_id": asset_id, "entity_type": entity_type, "entity_id": entity_id,
        "asset_type": "work_image" if entity_type == "work" else "performer_avatar",
        "source_type": source_type, "source_url": source_url.strip() if source_url else None,
        "checked_at": datetime.now(UTC).isoformat().replace("+00:00", "Z"),
        "confidence": confidence, "purpose": purpose, "position": position,
        "is_primary": is_primary, "reason": reason.strip(), "tool_version": TOOL_VERSION,
        "objects": objects,
    }
