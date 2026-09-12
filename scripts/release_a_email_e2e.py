#!/usr/bin/env python3
from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import re
import secrets
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

CODE_PATTERN = re.compile(r"验证码\s*[：:]\s*(\d{6})")


class E2EFailure(RuntimeError):
    pass


@dataclass(frozen=True)
class HTTPResult:
    status: int
    body: Any


def nested_strings(value: Any) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        return [item for child in value.values() for item in nested_strings(child)]
    if isinstance(value, list):
        return [item for child in value for item in nested_strings(child)]
    return []


def extract_code(value: Any) -> str | None:
    for text in nested_strings(value):
        match = CODE_PATTERN.search(text)
        if match:
            return match.group(1)
    return None


def contains_text(value: Any, expected: str) -> bool:
    expected = expected.casefold()
    return any(expected in text.casefold() for text in nested_strings(value))


class ReleaseAClient:
    def __init__(self, api_base: str, mailpit_base: str, origin: str, timeout: float) -> None:
        self.api_base = api_base.rstrip("/")
        self.mailpit_base = mailpit_base.rstrip("/")
        self.origin = origin
        self.timeout = timeout
        self.cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))

    def request(self, method: str, path: str, payload: Any | None = None) -> HTTPResult:
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        headers = {"Accept": "application/json", "Origin": self.origin}
        if data is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.api_base + path, data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                return HTTPResult(response.status, decode_body(response.read()))
        except urllib.error.HTTPError as error:
            return HTTPResult(error.code, decode_body(error.read()))
        except OSError as error:
            raise E2EFailure(f"cannot reach API {self.api_base}: {error}") from error

    def mailpit_json(self, path: str) -> Any:
        request = urllib.request.Request(self.mailpit_base + path, headers={"Accept": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=self.timeout) as response:
                return json.loads(response.read().decode("utf-8"))
        except (OSError, json.JSONDecodeError) as error:
            raise E2EFailure(f"cannot read Mailpit {self.mailpit_base}: {error}") from error

    def message_ids(self) -> set[str]:
        return {message_id(item) for item in mailpit_messages(self.mailpit_json("/api/v1/messages")) if message_id(item)}

    def wait_for_code(self, email: str, subject: str, previous_ids: set[str]) -> str:
        deadline = time.monotonic() + self.timeout
        while time.monotonic() < deadline:
            listing = self.mailpit_json("/api/v1/messages")
            for summary in mailpit_messages(listing):
                identifier = message_id(summary)
                if not identifier or identifier in previous_ids:
                    continue
                if not contains_text(summary, email) or not contains_text(summary, subject):
                    continue
                detail = self.mailpit_json(f"/api/v1/message/{identifier}")
                code = extract_code(detail)
                if code:
                    return code
            time.sleep(0.25)
        raise E2EFailure(f"Mailpit did not receive {subject} for {email} within {self.timeout:.0f}s")


def decode_body(raw: bytes) -> Any:
    if not raw:
        return None
    text = raw.decode("utf-8", errors="replace")
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def mailpit_messages(value: Any) -> list[dict[str, Any]]:
    if isinstance(value, list):
        return [item for item in value if isinstance(item, dict)]
    if isinstance(value, dict):
        for key in ("messages", "Messages"):
            items = value.get(key)
            if isinstance(items, list):
                return [item for item in items if isinstance(item, dict)]
    raise E2EFailure("Mailpit messages response has an unsupported shape")


def message_id(value: dict[str, Any]) -> str:
    for key in ("ID", "Id", "id"):
        identifier = value.get(key)
        if isinstance(identifier, str):
            return identifier
    return ""


def expect(result: HTTPResult, status: int, step: str) -> None:
    if result.status != status:
        raise E2EFailure(f"{step}: expected HTTP {status}, got {result.status}: {result.body!r}")


def run(
    client: ReleaseAClient,
    email: str,
    password: str,
    new_password: str,
    *,
    owner_email: str | None = None,
    owner_password: str | None = None,
    invited_email: str | None = None,
    invited_password: str | None = None,
) -> dict[str, str]:
    expect(client.request("GET", "/healthz"), 200, "API health")
    expect(client.request("GET", "/readyz"), 200, "API readiness")

    existing = client.message_ids()
    expect(client.request("POST", "/api/v1/auth/signup/code", {"email": email, "turnstile_token": "test-pass"}), 202, "request signup code")
    signup_code = client.wait_for_code(email, "幕鉴注册验证码", existing)
    expect(client.request("POST", "/api/v1/auth/signup/verify", {"email": email, "code": signup_code, "password": password}), 201, "verify signup")
    expect(client.request("GET", "/api/v1/me"), 200, "authenticated session after signup")

    existing = client.message_ids()
    expect(client.request("POST", "/api/v1/auth/password/code", {"email": email, "turnstile_token": "test-pass"}), 202, "request password reset code")
    reset_code = client.wait_for_code(email, "幕鉴密码重置验证码", existing)
    expect(client.request("POST", "/api/v1/auth/password/reset", {"email": email, "code": reset_code, "password": new_password}), 200, "reset password")
    expect(client.request("GET", "/api/v1/me"), 401, "old session revoked after password reset")
    expect(client.request("POST", "/api/v1/auth/login", {"email": email, "password": password}), 401, "old password rejected")
    expect(client.request("POST", "/api/v1/auth/login", {"email": email, "password": new_password}), 200, "new password accepted")

    existing = client.message_ids()
    expect(client.request("POST", "/api/v1/account/close/code"), 202, "request account close code")
    close_code = client.wait_for_code(email, "幕鉴关闭账号验证码", existing)
    expect(client.request("POST", "/api/v1/account/close", {"code": close_code, "notice_version": "2026-08-07"}), 200, "close account")
    expect(client.request("GET", "/api/v1/me"), 401, "session revoked after account close")
    expect(client.request("POST", "/api/v1/auth/login", {"email": email, "password": new_password}), 401, "closed account cannot log in")

    result = {
        "email": email,
        "signup": "passed",
        "password_reset": "passed",
        "account_close": "passed",
        "session_revocation": "passed",
    }
    invitation_values = (owner_email, owner_password, invited_email, invited_password)
    if any(value is not None for value in invitation_values):
        if not all(isinstance(value, str) and value.strip() for value in invitation_values):
            raise E2EFailure("owner and invited account arguments must be provided together")
        assert owner_email is not None and owner_password is not None and invited_email is not None and invited_password is not None
        run_invitation(
            client,
            owner_email=owner_email.strip(),
            owner_password=owner_password,
            invited_email=invited_email.casefold().strip(),
            invited_password=invited_password,
        )
        result["invitation"] = "passed"
    return result


def run_invitation(
    client: ReleaseAClient,
    *,
    owner_email: str,
    owner_password: str,
    invited_email: str,
    invited_password: str,
    invited_role: str = "user",
) -> None:
    if invited_role not in {"user", "editor", "admin"}:
        raise E2EFailure("unsupported invitation role")
    owner = ReleaseAClient(client.api_base, client.mailpit_base, client.origin, client.timeout)
    expect(owner.request("POST", "/api/v1/auth/login", {"email": owner_email, "password": owner_password}), 200, "owner login")
    expect(
        owner.request("POST", "/api/v1/auth/reauth", {"password": owner_password}),
        200,
        "owner recent password confirmation",
    )
    existing = owner.message_ids()
    invitation = owner.request(
        "POST",
        "/admin/v1/invitations",
        {"email": invited_email, "role": invited_role, "reason": "Release A invitation E2E"},
    )
    expect(invitation, 201, "create invitation")
    if not isinstance(invitation.body, dict) or not isinstance(invitation.body.get("id"), str):
        raise E2EFailure(f"create invitation: response did not contain an invitation id: {invitation.body!r}")
    code = owner.wait_for_code(invited_email, "幕鉴后台邀请验证码", existing)

    invited = ReleaseAClient(client.api_base, client.mailpit_base, client.origin, client.timeout)
    accepted = invited.request(
        "POST",
        "/api/v1/auth/invitations/accept",
        {"email": invited_email, "code": code, "password": invited_password},
    )
    expect(accepted, 201, "accept invitation")
    session = invited.request("GET", "/api/v1/me")
    expect(session, 200, "invited session")
    if not isinstance(session.body, dict) or not isinstance(session.body.get("user"), dict) or session.body["user"].get("role") != invited_role:
        raise E2EFailure(f"invited session: expected {invited_role} role, got {session.body!r}")


def required_secret(env_name: str) -> str:
    value = os.environ.get(env_name, "")
    if not value:
        raise E2EFailure(f"required password environment variable is not set: {env_name}")
    return value


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run Release A email account flows against API and Mailpit.",
        allow_abbrev=False,
    )
    parser.add_argument("--api-base", default="http://127.0.0.1:8080")
    parser.add_argument("--mailpit-base", default="http://127.0.0.1:8025")
    parser.add_argument("--origin", default="http://127.0.0.1:3000")
    parser.add_argument("--timeout", type=float, default=20.0)
    parser.add_argument("--email", default=f"release-a-e2e-{int(time.time())}@example.test")
    parser.add_argument("--password", default=f"Release-A-old-{secrets.token_urlsafe(18)}")
    parser.add_argument("--new-password", default=f"Release-A-new-{secrets.token_urlsafe(18)}")
    parser.add_argument("--owner-email", default=None, help="Run the optional owner invitation flow")
    parser.add_argument("--invited-email", default=None)
    parser.add_argument("--owner-password-env", default="RELEASE_A_OWNER_PASSWORD")
    parser.add_argument("--invited-password-env", default="RELEASE_A_INVITED_PASSWORD")
    return parser.parse_args(argv)


def main() -> int:
    arguments = parse_args()
    client = ReleaseAClient(arguments.api_base, arguments.mailpit_base, arguments.origin, arguments.timeout)
    try:
        owner_password = None
        invited_password = None
        if arguments.owner_email is not None or arguments.invited_email is not None:
            if not arguments.owner_email or not arguments.invited_email:
                raise E2EFailure("owner and invited email arguments must be provided together")
            owner_password = required_secret(arguments.owner_password_env)
            invited_password = required_secret(arguments.invited_password_env)
        result = run(
            client,
            arguments.email.casefold(),
            arguments.password,
            arguments.new_password,
            owner_email=arguments.owner_email,
            owner_password=owner_password,
            invited_email=arguments.invited_email,
            invited_password=invited_password,
        )
    except E2EFailure as error:
        print(json.dumps({"status": "failed", "error": str(error)}, ensure_ascii=False), file=sys.stderr)
        return 1
    print(json.dumps({"status": "passed", **result}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
