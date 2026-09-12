"""Single-writer admission and a durable fence for uncertain S3 uploads.

Every Release A uploader must run on the same Beijing host and share this
private state volume. This is not a distributed/account billing quota service.
"""
from __future__ import annotations

import json
import os
import re
import stat
import sys
import uuid
from contextlib import contextmanager
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Iterator

if sys.platform == "win32":
    import msvcrt
else:
    import fcntl

MAX_JOURNAL_BYTES = 16 * 1024
MAX_REVIEW_LOG_BYTES = 1024 * 1024


class UploadGuardError(OSError):
    pass


class UploadBusy(UploadGuardError):
    pass


class UploadRecoveryRequired(UploadGuardError):
    pass


def validate_lock_path(path: Path | None) -> Path:
    if path is None or not path.is_absolute():
        raise UploadGuardError("an absolute shared MEDIA_UPLOAD_LOCK_FILE is required for uploads")
    if path.is_symlink():
        raise UploadGuardError("upload lock must not be a symbolic link")
    return path


def _pending_path(lock: Path) -> Path:
    return lock.with_name(lock.name + ".pending.json")


def _open_regular(path: Path, flags: int) -> int:
    if path.is_symlink():
        raise UploadGuardError("upload state must not be a symbolic link")
    fd = os.open(path, flags | getattr(os, "O_NOFOLLOW", 0) | getattr(os, "O_BINARY", 0), 0o600)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1:
            raise UploadGuardError("upload state must be a private regular file")
        os.set_inheritable(fd, False)
    except BaseException:
        os.close(fd)
        raise
    return fd


@contextmanager
def locked_upload_file(path: Path) -> Iterator[None]:
    path = validate_lock_path(path)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    fd = _open_regular(path, os.O_CREAT | os.O_RDWR)
    acquired = False
    try:
        # The permanent inode is never unlinked: deleting a lock file would
        # permit a second process to acquire a different lock at the same path.
        if os.fstat(fd).st_size == 0:
            os.write(fd, b"\x00")
        os.lseek(fd, 0, os.SEEK_SET)
        try:
            if sys.platform == "win32":
                msvcrt.locking(fd, msvcrt.LK_NBLCK, 1)
            else:
                fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except (BlockingIOError, PermissionError) as exc:
            raise UploadBusy("another media upload or recovery is running; retry after it finishes") from exc
        acquired = True
        yield
    finally:
        try:
            if acquired:
                os.lseek(fd, 0, os.SEEK_SET)
                if sys.platform == "win32":
                    msvcrt.locking(fd, msvcrt.LK_UNLCK, 1)
                else:
                    fcntl.flock(fd, fcntl.LOCK_UN)
        finally:
            os.close(fd)


def _sync_directory(path: Path) -> None:
    if sys.platform != "win32":
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)


def _atomic_json(path: Path, payload: object) -> None:
    body = (json.dumps(payload, ensure_ascii=True, separators=(",", ":")) + "\n").encode()
    if len(body) > MAX_JOURNAL_BYTES:
        raise UploadGuardError("upload journal exceeds its safety limit")
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        fd = _open_regular(temporary, os.O_CREAT | os.O_EXCL | os.O_WRONLY)
        with os.fdopen(fd, "wb") as stream:
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        _sync_directory(path.parent)
    finally:
        temporary.unlink(missing_ok=True)


@dataclass
class Attempt:
    storage_key: str
    byte_size: int
    sha256: str

    def validate(self) -> None:
        key = self.storage_key
        if not isinstance(key, str) or not 1 <= len(key) <= 1024 or not re.fullmatch(r"[A-Za-z0-9_./-]+", key):
            raise UploadRecoveryRequired("upload journal contains an invalid object key")
        if not key.startswith(("media-master/", "media-public/")) or any(part in {"", ".", ".."} for part in key.split("/")):
            raise UploadRecoveryRequired("upload journal contains an invalid object scope")
        if type(self.byte_size) is not int or not 1 <= self.byte_size <= 10 * 1024 * 1024 or not isinstance(self.sha256, str) or not re.fullmatch(r"[a-f0-9]{64}", self.sha256):
            raise UploadRecoveryRequired("upload journal contains invalid object evidence")


@dataclass
class PendingUpload:
    run_id: str
    started_at: str
    objects: list[Attempt] = field(default_factory=list)


def _unique_fields(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise UploadRecoveryRequired("upload journal contains duplicate fields")
        result[key] = value
    return result


def _read_pending(lock: Path) -> PendingUpload | None:
    try:
        fd = _open_regular(_pending_path(lock), os.O_RDONLY)
    except FileNotFoundError:
        return None
    with os.fdopen(fd, "rb") as stream:
        body = stream.read(MAX_JOURNAL_BYTES + 1)
    if len(body) > MAX_JOURNAL_BYTES:
        raise UploadRecoveryRequired("upload journal is too large; manual recovery required")
    try:
        data = json.loads(body.decode("utf-8"), object_pairs_hook=_unique_fields)
        if not isinstance(data, dict) or set(data) != {"run_id", "started_at", "objects"}:
            raise ValueError("invalid fields")
        if not isinstance(data["run_id"], str) or str(uuid.UUID(data["run_id"])) != data["run_id"]:
            raise ValueError("invalid run id")
        if not isinstance(data["started_at"], str) or not data["started_at"].endswith("Z"):
            raise ValueError("invalid time")
        datetime.fromisoformat(data["started_at"])
        if not isinstance(data["objects"], list) or not 1 <= len(data["objects"]) <= 4:
            raise ValueError("invalid object count")
        attempts: list[Attempt] = []
        for item in data["objects"]:
            if not isinstance(item, dict) or set(item) != {"storage_key", "byte_size", "sha256"}:
                raise ValueError("invalid object fields")
            attempt = Attempt(**item)
            attempt.validate()
            attempts.append(attempt)
        if len({item.storage_key for item in attempts}) != len(attempts):
            raise ValueError("duplicate objects")
        return PendingUpload(data["run_id"], data["started_at"], attempts)
    except (UnicodeError, ValueError, TypeError, KeyError) as exc:
        raise UploadRecoveryRequired("upload journal is invalid; manual recovery required") from exc


class UploadJournal:
    def __init__(self, path: Path) -> None:
        self.path = path
        self.pending = PendingUpload(str(uuid.uuid4()), datetime.now(UTC).isoformat().replace("+00:00", "Z"))

    def before_put(self, key: str, byte_size: int, sha256: str) -> None:
        attempt = Attempt(key, byte_size, sha256)
        attempt.validate()
        if len(self.pending.objects) >= 4 or any(item.storage_key == key for item in self.pending.objects):
            raise UploadGuardError("upload batch contains too many or repeated objects")
        self.pending.objects.append(attempt)
        # Persist BEFORE dispatch: a lost SDK reply or killed process cannot
        # silently reopen admission while a remote write may still finish.
        _atomic_json(_pending_path(self.path), asdict(self.pending))

    def complete(self) -> None:
        if not self.pending.objects:
            return
        if _read_pending(self.path) != self.pending:
            raise UploadRecoveryRequired("upload journal changed before completion")
        _pending_path(self.path).unlink()
        _sync_directory(self.path.parent)


@contextmanager
def upload_admission(path: Path) -> Iterator[UploadJournal]:
    with locked_upload_file(path):
        pending = _read_pending(path)
        if pending is not None:
            raise UploadRecoveryRequired("an earlier upload is unconfirmed; inspect and acknowledge its run id after storage review")
        journal = UploadJournal(path)
        yield journal
        # Exceptions deliberately leave the fence, even if best-effort rollback
        # appeared successful. DELETE/HEAD cannot prove a timed-out PUT ended.
        journal.complete()


def upload_status(path: Path) -> dict[str, object]:
    with locked_upload_file(path):
        pending = _read_pending(path)
        return {"status": "review_required" if pending else "idle", "pending": asdict(pending) if pending else None}


def acknowledge_upload(path: Path, *, expected_run_id: str, reason: str, confirmed: bool) -> dict[str, object]:
    if not confirmed or not isinstance(reason, str) or not 2 <= len(reason.strip()) <= 1000:
        raise UploadGuardError("explicit storage reconciliation confirmation and a 2-1000 character reason are required")
    with locked_upload_file(path):
        pending = _read_pending(path)
        if pending is None or pending.run_id != expected_run_id:
            raise UploadRecoveryRequired("the pending upload does not match the expected run id")
        event = {"status": "manual_review_acknowledged", "reviewed_at": datetime.now(UTC).isoformat().replace("+00:00", "Z"),
                 "reason": reason.strip(), "pending": asdict(pending)}
        body = (json.dumps(event, ensure_ascii=True, separators=(",", ":")) + "\n").encode()
        review_file = path.with_name(path.name + ".reviews.jsonl")
        fd = _open_regular(review_file, os.O_CREAT | os.O_APPEND | os.O_WRONLY)
        with os.fdopen(fd, "ab") as stream:
            if os.fstat(stream.fileno()).st_size + len(body) > MAX_REVIEW_LOG_BYTES:
                raise UploadGuardError("archive the private upload review log before acknowledging more runs")
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        _sync_directory(path.parent)
        _pending_path(path).unlink()
        _sync_directory(path.parent)
        return {"status": "manual_review_acknowledged", "run_id": pending.run_id, "fresh_capacity_check_required": True}
