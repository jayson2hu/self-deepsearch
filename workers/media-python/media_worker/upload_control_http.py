"""Versioned, path-bound request AND response authentication for controls."""
from __future__ import annotations

import hashlib
import hmac
import json
import re
from http.server import BaseHTTPRequestHandler
from typing import Callable

from .upload_control import UploadControl, UploadControlConflict, UploadControlExpired, unique_fields
from .upload_guard import UploadBusy

PATHS = {"/v1/upload-control/status", "/v1/upload-control/apply"}
MAX_BODY = 16 * 1024


def request_signature(secret: bytes, path: str, timestamp: str, nonce: str, body: bytes) -> str:
    prefix = f"sd-upload-control-request-v1\nPOST\n{path}\n{timestamp}\n{nonce}\n".encode()
    return "u1=" + hmac.new(secret, prefix + body, hashlib.sha256).hexdigest()


def response_signature(secret: bytes, path: str, timestamp: str, nonce: str, request_body: bytes, status: int, body: bytes) -> str:
    digest = hashlib.sha256(request_body).hexdigest()
    prefix = f"sd-upload-control-response-v1\nPOST\n{path}\n{timestamp}\n{nonce}\n{digest}\n{status}\n".encode()
    return "u1=" + hmac.new(secret, prefix + body, hashlib.sha256).hexdigest()


def handle_control(request: BaseHTTPRequestHandler, control: UploadControl | None, secret: str, now: Callable[[], float]) -> None:
    authenticated = False
    timestamp, nonce, body = "", "", b""
    secret_bytes = secret.strip().encode()

    def respond(status: int, value: object) -> None:
        response = json.dumps(value, separators=(",", ":")).encode()
        request.send_response(status)
        request.send_header("Content-Type", "application/json")
        request.send_header("Content-Length", str(len(response)))
        request.send_header("Cache-Control", "no-store")
        request.send_header("Connection", "close")
        if authenticated:
            request.send_header("X-SD-Control-Response", response_signature(secret_bytes, request.path, timestamp, nonce, body, status, response))
        request.end_headers()
        request.wfile.write(response)
        request.close_connection = True

    if control is None or len(secret_bytes) < 32:
        respond(404, {"error": "control_disabled"})
        return
    try:
        request.connection.settimeout(5)
        headers = request.headers
        for name in ("Content-Length", "Content-Type", "X-SD-Control-Timestamp", "X-SD-Control-Nonce", "X-SD-Control-Signature"):
            if len(headers.get_all(name, [])) != 1:
                raise ValueError("missing or duplicate headers")
        if headers.get("Transfer-Encoding") or headers.get("Content-Encoding") or headers["Content-Type"] != "application/json":
            raise ValueError("invalid encoding")
        length_text = headers["Content-Length"]
        if not re.fullmatch(r"[1-9][0-9]{0,4}", length_text) or not 1 <= int(length_text) <= MAX_BODY:
            raise ValueError("invalid length")
        timestamp, nonce = headers["X-SD-Control-Timestamp"], headers["X-SD-Control-Nonce"]
        supplied = headers["X-SD-Control-Signature"]
        if not re.fullmatch(r"[0-9]{1,12}", timestamp) or abs(now() - int(timestamp)) > 300 or not re.fullmatch(r"[a-f0-9]{64}", nonce):
            respond(401, {"error": "invalid_signature"})
            return
        body = request.rfile.read(int(length_text))
        if len(body) != int(length_text):
            raise ValueError("incomplete body")
        if not hmac.compare_digest(supplied, request_signature(secret_bytes, request.path, timestamp, nonce, body)):
            respond(401, {"error": "invalid_signature"})
            return
        authenticated = True
        payload = json.loads(body.decode("utf-8"), object_pairs_hook=unique_fields)
        if request.path == "/v1/upload-control/status":
            if payload != {}:
                raise ValueError("status takes an empty object")
            respond(200, control.status())
        else:
            respond(200, control.apply(payload, now=now))
    except UploadControlExpired:
        respond(409, {"error": "control_expired"})
    except UploadControlConflict:
        respond(409, {"error": "control_conflict"})
    except UploadBusy:
        respond(503, {"error": "control_busy"})
    except (ValueError, UnicodeError, RecursionError):
        respond(400, {"error": "invalid_control_request"})
    except OSError:
        respond(503, {"error": "control_unavailable"})
