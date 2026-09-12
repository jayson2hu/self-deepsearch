"""One-shot signed usage checks against the configured Japan IP, no cache.

Only a complete, current, request-bound allow is usable. A small deadline-aware
socket reader bounds HTTP headers as well as the body (including slow peers).
"""
from __future__ import annotations

import hashlib
import hmac
import http.client
import io
import ipaddress
import json
import os
import re
import secrets
import socket
import ssl
import time
from collections.abc import Buffer
from dataclasses import dataclass
from datetime import datetime
from urllib.parse import urlsplit

PATH = "/v1/upload-admission"
MAX_WIRE_BYTES = 8192
TIMEOUT = 3.0


@dataclass(frozen=True)
class Decision:
    outcome: str
    nonce: str
    response_hash: str = ""


def request_signature(secret: bytes, timestamp: str, nonce: str) -> str:
    body = f"sd-upload-admission-request-v1\nPOST\n{PATH}\n{timestamp}\n{nonce}\n".encode() + b"{}"
    return "a1=" + hmac.new(secret, body, hashlib.sha256).hexdigest()


def response_signature(secret: bytes, timestamp: str, nonce: str, status: int, body: bytes) -> str:
    prefix = f"sd-upload-admission-response-v1\nPOST\n{PATH}\n{timestamp}\n{nonce}\n{hashlib.sha256(b'{}').hexdigest()}\n{status}\n".encode()
    return "a1=" + hmac.new(secret, prefix + body, hashlib.sha256).hexdigest()


class _DeadlineReader(io.RawIOBase):
    def __init__(self, connection: socket.socket, deadline: float) -> None:
        self.connection, self.deadline, self.size = connection, deadline, 0

    def readable(self) -> bool:
        return True

    def readinto(self, buffer: Buffer, /) -> int:
        remaining = self.deadline - time.monotonic()
        if remaining <= 0 or self.size >= MAX_WIRE_BYTES:
            raise OSError("admission response deadline or size exceeded")
        self.connection.settimeout(remaining)
        view = memoryview(buffer)
        count = self.connection.recv_into(view, min(view.nbytes, MAX_WIRE_BYTES - self.size))
        self.size += count
        return count


class _ResponseSocket:
    def __init__(self, connection: socket.socket, deadline: float) -> None:
        self.reader = _DeadlineReader(connection, deadline)

    def makefile(self, _mode: str) -> io.BufferedReader:
        return io.BufferedReader(self.reader)


def _unique(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate admission fields")
        result[key] = value
    return result


class AdmissionClient:
    def __init__(self, origin: str, secret: str, *, allow_http: bool = False) -> None:
        if any(character.isspace() for character in origin):
            raise ValueError("admission origin must not contain whitespace")
        parsed = urlsplit(origin)
        if parsed.scheme not in ({"https", "http"} if allow_http else {"https"}) or not parsed.hostname or parsed.username is not None or parsed.password is not None:
            raise ValueError("admission requires a configured HTTPS IP origin or explicit trusted HTTP")
        if parsed.path not in {"", "/"} or parsed.query or parsed.fragment or "?" in origin or "#" in origin or "%" in parsed.hostname:
            raise ValueError("admission origin must not contain a path, query or fragment")
        address = ipaddress.ip_address(parsed.hostname)
        if address.is_unspecified or address.is_multicast or parsed.port == 0 or not 32 <= len(secret.strip().encode()) <= 4096:
            raise ValueError("admission requires a fixed unicast IP and independent secret")
        self.ip, self.port, self.tls = str(address), parsed.port or (443 if parsed.scheme == "https" else 80), parsed.scheme == "https"
        self.secret = secret.strip().encode()
        self.family = socket.AF_INET6 if address.version == 6 else socket.AF_INET
        self.host = f"[{self.ip}]:{self.port}" if address.version == 6 else f"{self.ip}:{self.port}"

    def check(self) -> Decision:
        nonce, timestamp = secrets.token_hex(32), str(int(time.time()))
        deadline = time.monotonic() + TIMEOUT
        try:
            with socket.socket(self.family, socket.SOCK_STREAM) as raw:
                raw.settimeout(max(0.001, deadline - time.monotonic()))
                raw.connect((self.ip, self.port))
                if self.tls:
                    raw.settimeout(max(0.001, deadline - time.monotonic()))
                    with ssl.create_default_context().wrap_socket(raw, server_hostname=self.ip) as connection:
                        return self._exchange(connection, deadline, timestamp, nonce)
                return self._exchange(raw, deadline, timestamp, nonce)
        except (OSError, ValueError, UnicodeError, http.client.HTTPException, RecursionError):
            return Decision("unavailable", nonce)

    def _exchange(self, connection: socket.socket, deadline: float, timestamp: str, nonce: str) -> Decision:
        signature = request_signature(self.secret, timestamp, nonce)
        request = (f"POST {PATH} HTTP/1.1\r\nHost: {self.host}\r\nContent-Type: application/json\r\nContent-Length: 2\r\n"
                   f"X-SD-Admission-Timestamp: {timestamp}\r\nX-SD-Admission-Nonce: {nonce}\r\nX-SD-Admission-Signature: {signature}\r\n"
                   "Connection: close\r\n\r\n{}").encode()
        connection.settimeout(max(0.001, deadline - time.monotonic()))
        connection.sendall(request)
        adapter = _ResponseSocket(connection, deadline)
        with http.client.HTTPResponse(adapter, method="POST") as response:  # type: ignore[arg-type]
            response.begin()
            for name in ("Content-Type", "Content-Length", "X-SD-Admission-Response"):
                if len(response.headers.get_all(name, [])) != 1:
                    raise ValueError("missing or duplicate admission response headers")
            if response.headers["Content-Type"] != "application/json" or response.headers.get("Transfer-Encoding") or response.headers.get("Content-Encoding"):
                raise ValueError("admission response encoding rejected")
            length = response.headers["Content-Length"]
            if not re.fullmatch(r"[1-9][0-9]{0,3}", length) or int(length) > 1024:
                raise ValueError("admission response length rejected")
            body = response.read(int(length) + 1)
            if len(body) != int(length) or time.monotonic() > deadline:
                raise ValueError("admission response incomplete or late")
            expected = response_signature(self.secret, timestamp, nonce, response.status, body)
            if not hmac.compare_digest(expected, response.headers["X-SD-Admission-Response"]):
                raise ValueError("admission response signature rejected")
            data = json.loads(body.decode("utf-8"), object_pairs_hook=_unique)
            if not isinstance(data, dict) or set(data) != {"decision", "checked_at", "request_nonce"} or not isinstance(data["decision"], str) or data["decision"] not in {"allow", "deny"} or data["request_nonce"] != nonce:
                raise ValueError("admission response fields rejected")
            checked = data["checked_at"]
            if not isinstance(checked, str) or not re.fullmatch(r"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ", checked):
                raise ValueError("admission check time rejected")
            at = datetime.fromisoformat(checked).timestamp()
            if abs(time.time() - at) > 5 or at < int(timestamp) - 1 or response.status not in {200, 429}:
                raise ValueError("admission check is stale or unsuccessful")
            result = "allow" if response.status == 200 and data["decision"] == "allow" else "denied"
            return Decision(result, nonce, hashlib.sha256(body).hexdigest())


def configured_decision() -> Decision | None:
    mode = os.getenv("MEDIA_UPLOAD_ADMISSION_MODE", "off")
    if mode == "off":
        return None
    try:
        secret = os.getenv("MEDIA_UPLOAD_ADMISSION_SECRET", "").strip()
        if mode != "enforce" or os.getenv("MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "false") not in {"true", "false"}:
            raise ValueError("invalid upload admission configuration")
        if secret in {os.getenv("MEDIA_HMAC_SECRET", "").strip(), os.getenv("MEDIA_UPLOAD_CONTROL_SECRET", "").strip()}:
            raise ValueError("upload admission requires a separate secret")
        return AdmissionClient(os.getenv("MEDIA_UPLOAD_ADMISSION_URL", ""), secret,
                               allow_http=os.getenv("MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP") == "true").check()
    except (ValueError, OSError):
        # A broken admission configuration may never turn into a permit; it
        # must not prevent the service's separate necessary deletion path.
        return Decision("unavailable", secrets.token_hex(32))
