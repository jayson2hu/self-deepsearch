"""Durable, single-host upload control; independent of the uncertain PUT fence.

The bounded append-only ledger is the source of truth, not a cache. Missing,
corrupt or exhausted state never grants uploads. Only an explicit paused
bootstrap may create a new epoch. No command clears upload.pending.json.
"""
from __future__ import annotations

import json
import os
import re
import time
import uuid
from contextlib import contextmanager
from datetime import UTC, datetime
from pathlib import Path
from typing import Callable, Iterator

from .upload_admission import Decision, configured_decision
from .upload_guard import UploadGuardError, _open_regular, _sync_directory, locked_upload_file, validate_lock_path

MAX_LEDGER_BYTES = 1024 * 1024
MAX_GENERATION = 2**53 - 1
COMMAND_FIELDS = {"command_id", "expected_epoch", "expected_generation", "mode", "reason", "resume_confirmed"}
TIMED_COMMAND_FIELDS = COMMAND_FIELDS | {"issued_at", "expires_at"}


class UploadControlBlocked(UploadGuardError):
    pass


class UploadControlConflict(UploadGuardError):
    pass


class UploadControlExpired(UploadGuardError):
    pass


def _uuid(value: object) -> bool:
    return isinstance(value, str) and bool(re.fullmatch(r"[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}", value))


def validate_command(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or set(value) not in (COMMAND_FIELDS, TIMED_COMMAND_FIELDS) or not _uuid(value["command_id"]):
        raise ValueError("invalid upload control command")
    generation, epoch = value["expected_generation"], value["expected_epoch"]
    if type(generation) is not int or not 0 <= generation < MAX_GENERATION:
        raise ValueError("invalid expected generation")
    if (generation == 0 and epoch != "") or (generation != 0 and not _uuid(epoch)):
        raise ValueError("invalid expected epoch")
    if value["mode"] not in ("paused", "enabled") or type(value["resume_confirmed"]) is not bool:
        raise ValueError("invalid upload mode")
    if (value["mode"] == "enabled") != value["resume_confirmed"] or (generation == 0 and value["mode"] != "paused"):
        raise ValueError("resume requires explicit confirmation and an initialized paused epoch")
    reason = value["reason"]
    if not isinstance(reason, str) or reason != reason.strip() or not 2 <= len(reason) <= 1000 or any(ord(c) < 32 for c in reason):
        raise ValueError("invalid control reason")
    if set(value) == TIMED_COMMAND_FIELDS:
        if not _time_valid(value["issued_at"]) or not _time_valid(value["expires_at"]):
            raise ValueError("invalid command validity window")
        duration = (datetime.fromisoformat(value["expires_at"]) - datetime.fromisoformat(value["issued_at"])).total_seconds()
        if not 0 < duration <= 300:
            raise ValueError("command validity must be at most five minutes")
    return value


def unique_fields(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON fields")
        result[key] = value
    return result


def _time_valid(value: object) -> bool:
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", value):
        return False
    try:
        datetime.fromisoformat(value)
        return True
    except ValueError:
        return False


class UploadControl:
    def __init__(self, upload_lock: Path) -> None:
        self.upload_lock = validate_lock_path(upload_lock)
        self.lock = upload_lock.with_name(upload_lock.name + ".control.lock")
        self.ledger = upload_lock.with_name(upload_lock.name + ".control.jsonl")

    def _read(self) -> list[dict[str, object]]:
        try:
            fd = _open_regular(self.ledger, os.O_RDONLY)
        except FileNotFoundError:
            return []
        with os.fdopen(fd, "rb") as stream:
            body = stream.read(MAX_LEDGER_BYTES + 1)
        if not body or len(body) >= MAX_LEDGER_BYTES or not body.endswith(b"\n"):
            raise UploadControlBlocked("upload control ledger is incomplete or exhausted; private recovery required")
        records: list[dict[str, object]] = []
        seen: set[str] = set()
        epoch, previous_time = "", ""
        try:
            for line in body.splitlines():
                record = json.loads(line.decode("utf-8"), object_pairs_hook=unique_fields)
                base_fields = {"version", "epoch", "generation", "command", "applied_at"}
                if not isinstance(record, dict) or set(record) not in (base_fields, base_fields | {"source", "decision"}):
                    raise ValueError("invalid ledger record")
                command = validate_command(record["command"])
                command_id = str(command["command_id"])
                if type(record["version"]) is not int or type(record["generation"]) is not int:
                    raise ValueError("invalid ledger version")
                if record["version"] == 1:
                    if set(record) != base_fields:
                        raise ValueError("invalid operator ledger fields")
                elif record["version"] == 2:
                    evidence = record.get("decision")
                    if record.get("source") != "usage_admission" or command["mode"] != "paused" or set(command) != COMMAND_FIELDS:
                        raise ValueError("invalid automatic pause origin")
                    if not isinstance(evidence, dict) or set(evidence) != {"outcome", "nonce", "response_hash"}:
                        raise ValueError("invalid automatic pause evidence")
                    if evidence["outcome"] not in ("denied", "unavailable") or not isinstance(evidence["nonce"], str) or not re.fullmatch(r"[a-f0-9]{64}", evidence["nonce"]):
                        raise ValueError("invalid automatic pause decision")
                    if not isinstance(evidence["response_hash"], str) or not re.fullmatch(r"[a-f0-9]{64}|", evidence["response_hash"]):
                        raise ValueError("invalid automatic pause digest")
                    if evidence["outcome"] == "denied" and not evidence["response_hash"]:
                        raise ValueError("denied decision needs authenticated response evidence")
                    if command["reason"] != "Automatic pause: upload usage admission " + evidence["outcome"]:
                        raise ValueError("invalid automatic pause reason")
                else:
                    raise ValueError("unsupported ledger version")
                if command["expected_generation"] != len(records) or command["expected_epoch"] != epoch or command_id in seen:
                    raise ValueError("invalid ledger chain")
                if not records:
                    epoch = command_id
                if record["generation"] != len(records) + 1 or record["epoch"] != epoch:
                    raise ValueError("invalid ledger generation")
                applied = record["applied_at"]
                if not _time_valid(applied) or str(applied) < previous_time:
                    raise ValueError("invalid ledger time")
                previous_time = str(applied)
                seen.add(command_id)
                records.append(record)
        except (ValueError, UnicodeError, TypeError, KeyError, RecursionError) as exc:
            raise UploadControlBlocked("upload control ledger is invalid; private recovery required") from exc
        return records

    @staticmethod
    def _receipt(records: list[dict[str, object]], status: str) -> dict[str, object]:
        if not records:
            return {"status": status, "epoch": "", "generation": 0, "mode": "paused", "command_id": "", "applied_at": ""}
        last = records[-1]
        command = validate_command(last["command"])
        return {"status": status, "epoch": last["epoch"], "generation": last["generation"], "mode": command["mode"],
                "command_id": command["command_id"], "applied_at": last["applied_at"]}

    def status(self) -> dict[str, object]:
        with locked_upload_file(self.lock):
            return self._receipt(self._read(), "observed")

    @contextmanager
    def operation(self) -> Iterator[None]:
        # Serialize an actual SDK dispatch with control application. A pause
        # during a PUT returns busy, never an early "applied" acknowledgement.
        # Timed-out remote PUTs remain uncertain in the separate upload journal.
        with locked_upload_file(self.lock):
            records = self._read()
            if os.getenv("MEDIA_DELIVERY_MODE", "normal") != "normal" or self._receipt(records, "observed")["mode"] != "enabled":
                raise UploadControlBlocked("new media uploads are paused or not initialized")
            self._require_admission(records)
            yield

    def require_enabled(self) -> None:
        with self.operation():
            pass

    def apply(self, payload: object, *, now: float | Callable[[], float] = time.time) -> dict[str, object]:
        command = validate_command(payload)
        with locked_upload_file(self.lock):
            records = self._read()
            current = self._receipt(records, "observed")
            if records and records[-1]["command"] == command:
                # Retry after a lost receipt: ensure the existing ledger is
                # durable even when the previous process failed during fsync.
                fd = _open_regular(self.ledger, os.O_RDWR)
                try:
                    os.fsync(fd)
                finally:
                    os.close(fd)
                _sync_directory(self.ledger.parent)
                return self._receipt(records, "replayed")
            if command["expected_epoch"] != current["epoch"] or command["expected_generation"] != current["generation"]:
                raise UploadControlConflict("upload control snapshot changed; inspect status before issuing another command")
            if any(validate_command(record["command"])["command_id"] == command["command_id"] for record in records):
                raise UploadControlConflict("upload control command id was already used")
            # Read the clock AFTER the file read/CAS, immediately before the
            # append linearization point. A delayed queued resume cannot use
            # the HTTP request's earlier authentication time as a new permit.
            at = datetime.fromtimestamp(now() if callable(now) else now, UTC)
            if "issued_at" in command and not datetime.fromisoformat(str(command["issued_at"])) <= at < datetime.fromisoformat(str(command["expires_at"])):
                raise UploadControlExpired("upload control request is outside its validity window")
            if command["mode"] == "enabled":
                self._require_admission(records)
                # The reverse check consumes time. Revalidate the original
                # command's deadline immediately before durable application.
                at = datetime.fromtimestamp(now() if callable(now) else now, UTC)
                if "expires_at" in command and not datetime.fromisoformat(str(command["issued_at"])) <= at < datetime.fromisoformat(str(command["expires_at"])):
                    raise UploadControlExpired("upload control request expired during admission check")
            self._append(records, command, at)
            return self._receipt(records, "applied")

    def _require_admission(self, records: list[dict[str, object]]) -> None:
        decision = configured_decision()
        if decision is None or decision.outcome == "allow":
            return
        current = self._receipt(records, "observed")
        if current["mode"] == "enabled":
            # This is a system event, not an administrator request. Advance
            # the SAME CAS chain before returning a denied SDK admission so
            # stale queued resumes cannot later undo the automatic pause.
            command = {"command_id": str(uuid.uuid4()), "expected_epoch": current["epoch"],
                       "expected_generation": current["generation"], "mode": "paused",
                       "reason": "Automatic pause: upload usage admission " + decision.outcome, "resume_confirmed": False}
            self._append(records, validate_command(command), datetime.now(UTC), decision)
        raise UploadControlBlocked("upload usage admission denied or unavailable; uploads remain paused pending explicit review and resume")

    def _append(self, records: list[dict[str, object]], command: dict[str, object], at: datetime, decision: Decision | None = None) -> None:
        current = self._receipt(records, "observed")
        record = {"version": 2 if decision else 1, "epoch": current["epoch"] or command["command_id"],
                  "generation": int(str(current["generation"])) + 1, "command": command,
                  "applied_at": max(str(current["applied_at"]), at.strftime("%Y-%m-%dT%H:%M:%SZ"))}
        if decision:
            record.update(source="usage_admission", decision={"outcome": decision.outcome, "nonce": decision.nonce, "response_hash": decision.response_hash})
        body = (json.dumps(record, ensure_ascii=True, separators=(",", ":")) + "\n").encode()
        fd = _open_regular(self.ledger, os.O_CREAT | os.O_APPEND | os.O_WRONLY)
        with os.fdopen(fd, "ab") as stream:
            limit = MAX_LEDGER_BYTES - 16 * 1024 if command["mode"] == "enabled" else MAX_LEDGER_BYTES
            if os.fstat(stream.fileno()).st_size + len(body) >= limit:
                raise UploadControlBlocked("archive the private control ledger in a stopped maintenance window")
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        _sync_directory(self.ledger.parent)
        records.append(record)


def configured_control(upload_lock: Path | None) -> UploadControl | None:
    mode = os.getenv("MEDIA_UPLOAD_CONTROL_MODE", "off")
    if mode not in {"off", "enforce"}:
        raise UploadControlBlocked("MEDIA_UPLOAD_CONTROL_MODE must be off or enforce")
    admission_mode = os.getenv("MEDIA_UPLOAD_ADMISSION_MODE", "off")
    if mode == "off" and upload_lock is None and admission_mode == "off":
        return None
    control = UploadControl(validate_lock_path(upload_lock))
    # Once initialized, reverting the flag to off cannot silently reopen
    # uploads. Never remove/replace the permanent lock or private ledger live.
    if mode == "enforce" or admission_mode != "off" or control.lock.exists() or control.lock.is_symlink() or control.ledger.exists() or control.ledger.is_symlink():
        return control
    return None
