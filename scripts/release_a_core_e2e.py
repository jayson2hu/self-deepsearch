#!/usr/bin/env python3
"""Run real API/PostgreSQL/Mailpit contracts in a disposable loopback test setup."""
from __future__ import annotations

import argparse
import json
import os
import secrets
import socket
import subprocess
import sys
import tempfile
import time
import urllib.parse
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import release_a_catalog_e2e as catalog
import release_a_email_e2e as email

ROOT = Path(__file__).resolve().parents[1]
TEST_DATABASE = "self_deepsearch_core_e2e_test"
ORIGIN = "http://127.0.0.1:3001"
MAILPIT = "http://127.0.0.1:8025"


def validate_database(environment: dict[str, str]) -> str:
    if environment.get("CONFIRM_RELEASE_A_CORE_E2E") != "disposable-database":
        raise ValueError("disposable database confirmation is required")
    value = environment.get("CORE_E2E_DATABASE_URL", "")
    parsed = urllib.parse.urlsplit(value)
    if (
        parsed.scheme not in {"postgres", "postgresql"}
        or parsed.hostname not in {"127.0.0.1", "::1"}
        or parsed.username != "platform_api_login"
        or not parsed.password
        or parsed.path != f"/{TEST_DATABASE}"
        or parsed.query not in {"", "sslmode=disable"}
        or parsed.fragment
    ):
        raise ValueError("only the dedicated loopback database and restricted API login are accepted")
    # Force URL port validation before any process or network activity.
    if parsed.port is not None and not 1 <= parsed.port <= 65535:
        raise ValueError("invalid database port")
    return value


def runtime_environment(database_url: str, port: int, version: str) -> dict[str, str]:
    # Do not hand unrelated production/provider credentials to child processes.
    inherited = {"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG", "LC_ALL"}
    result = {key: value for key, value in os.environ.items() if key.upper() in inherited}
    result.update({
        "APP_ENV": "development",
        "BUILD_VERSION": version,
        "DATABASE_URL": database_url,
        "API_ADDR": f"127.0.0.1:{port}",
        "ALLOWED_ORIGINS": ORIGIN,
        "AUTH_HMAC_SECRET": secrets.token_urlsafe(48),
        "SMTP_URL": "smtp://127.0.0.1:1025?from=no-reply@example.test&insecure=true",
        "TURNSTILE_BYPASS": "true",
        "TRUST_PROXY_HEADERS": "false",
        "COLLECTION_ENABLED": "false",
        "EMAIL_DAILY_LIMIT": "100",
        "S3_PUBLIC_BASE_URL": "http://127.0.0.1:8090/media",
    })
    return result


def available_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def wait_for_api(process: subprocess.Popen[bytes], client: email.ReleaseAClient, version: str, timeout: float) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError("API exited before readiness")
        try:
            health = client.request("GET", "/healthz")
            ready = client.request("GET", "/readyz")
            if health.status == 200 and ready.status == 200:
                if not isinstance(health.body, dict) or health.body.get("version") != version:
                    raise RuntimeError("unexpected API process owns the port")
                client.message_ids()  # Mailpit must be accessible before any signup.
                return
        except email.E2EFailure:
            pass
        time.sleep(0.25)
    raise TimeoutError("API/database or Mailpit readiness timed out")


def stop_process(process: subprocess.Popen[bytes]) -> None:
    if process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


def execute(args: argparse.Namespace, report: dict[str, Any]) -> None:
    database_url = validate_database(dict(os.environ))
    api_binary, owner_binary = args.api_binary.resolve(), args.owner_binary.resolve()
    if not api_binary.is_file() or not owner_binary.is_file():
        raise ValueError("build API and create-owner binaries before running")

    # This dedicated process only calls loopback endpoints; inherited proxies
    # must not receive synthetic passwords, cookies, or database credentials.
    os.environ["NO_PROXY"] = "*"
    os.environ["no_proxy"] = "*"
    run_id = secrets.token_hex(8)
    version = f"core-e2e-{run_id}"
    port = available_port()
    api_base = f"http://127.0.0.1:{port}"
    environment = runtime_environment(database_url, port, version)
    owner_email, editor_email = f"owner-{run_id}@example.test", f"editor-{run_id}@example.test"
    owner_password, editor_password = secrets.token_urlsafe(32), secrets.token_urlsafe(32)
    client = email.ReleaseAClient(api_base, MAILPIT, ORIGIN, args.timeout)
    with tempfile.TemporaryDirectory(prefix="release-a-core-e2e-") as directory:
        # Raw subprocess output is local and ephemeral, never a CI artifact.
        with (Path(directory) / "process.log").open("wb") as log:
            report["stage"] = "owner_bootstrap"
            subprocess.run(
                [str(owner_binary)], cwd=ROOT,
                env={**environment, "BOOTSTRAP_OWNER_EMAIL": owner_email, "BOOTSTRAP_OWNER_PASSWORD": owner_password},
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, timeout=30, check=True,
            )
            report["stage"] = "api_startup"
            process = subprocess.Popen(
                [str(api_binary)], cwd=ROOT, env=environment,
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
            )
            try:
                readiness_client = email.ReleaseAClient(api_base, MAILPIT, ORIGIN, 2)
                wait_for_api(process, readiness_client, version, args.timeout)
                report["checks"]["database_api_readiness"] = "passed"
                report["stage"] = "email_account_flows"
                result = email.run(
                    client, f"account-{run_id}@example.test", secrets.token_urlsafe(32), secrets.token_urlsafe(32),
                    owner_email=owner_email, owner_password=owner_password,
                    invited_email=f"invited-{run_id}@example.test", invited_password=secrets.token_urlsafe(32),
                )
                for check in ("signup", "password_reset", "account_close", "session_revocation", "invitation"):
                    if result.get(check) != "passed":
                        raise RuntimeError("account flow did not produce passing evidence")
                    report["checks"][check] = "passed"
                report["stage"] = "editor_invitation"
                email.run_invitation(
                    client, owner_email=owner_email, owner_password=owner_password,
                    invited_email=editor_email, invited_password=editor_password, invited_role="editor",
                )
                report["checks"]["editor_invitation"] = "passed"
                report["stage"] = "catalog_publication"
                code, slug, checked_at = catalog.default_identity()
                result = catalog.run_catalog_flow(
                    catalog.ReleaseAClient(api_base, ORIGIN, args.timeout),
                    catalog.ReleaseAClient(api_base, ORIGIN, args.timeout),
                    editor_email=editor_email, editor_password=editor_password,
                    reviewer_email=owner_email, reviewer_password=owner_password,
                    code=code, slug=slug, checked_at=checked_at,
                )
                if result.get("status") != "passed" or result.get("final_state") != "hidden":
                    raise RuntimeError("catalog flow did not finish with a hidden fixture")
                report["checks"]["create_review_publish_search_sitemap_hide"] = "passed"
            finally:
                stop_process(process)
    report["stage"] = "completed"
    report["status"] = "passed"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    suffix = ".exe" if sys.platform == "win32" else ""
    parser.add_argument("--api-binary", type=Path, default=ROOT / f".cache/core-e2e/platform-api{suffix}")
    parser.add_argument("--owner-binary", type=Path, default=ROOT / f".cache/core-e2e/create-owner{suffix}")
    parser.add_argument("--output", type=Path, default=ROOT / "docs/evidence/release-a-core-e2e-ci.json")
    parser.add_argument("--timeout", type=float, default=30.0)
    args = parser.parse_args(argv)
    if not 1 <= args.timeout <= 60:
        parser.error("timeout must be between 1 and 60 seconds")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report: dict[str, Any] = {
        "status": "failed", "stage": "configuration", "checks": {},
        "scope": "real-go-api-postgresql-mailpit",
        "started_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "not_covered": ["worker", "frontend", "turnstile", "s3", "cloudflare", "target_servers"],
    }
    try:
        execute(args, report)
    except Exception as error:
        # Existing E2E errors can contain raw response bodies. Never serialize
        # them, process output, connection strings, or generated credentials.
        report["error_type"] = type(error).__name__
    report["finished_at"] = datetime.now(timezone.utc).isoformat(timespec="seconds")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
