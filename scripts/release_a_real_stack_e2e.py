#!/usr/bin/env python3
"""Exercise production browsers against an isolated real API, Worker, PostgreSQL and Mailpit."""
from __future__ import annotations

import argparse
import json
import os
import secrets
import signal
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import release_a_core_e2e as core

ROOT = Path(__file__).resolve().parents[1]
CONFIRMATION = "CONFIRM_RELEASE_A_REAL_STACK_E2E"
BROWSER_CHECKS = {
    "real_services_ready", "production_frontends_ready", "owner_browser_login",
    "owner_ui_invitation_mailpit", "editor_browser_accept_invitation", "editor_ui_create_with_sources",
    "author_review_separation", "owner_ui_claim_approve", "owner_ui_publish",
    "worker_publication_cache_invalidation", "anonymous_desktop_mobile_search_detail",
    "owner_ui_hide", "worker_hidden_cache_invalidation", "anonymous_desktop_mobile_hidden", "browser_loopback_only",
}
BROWSER_STAGES = BROWSER_CHECKS | {
    "configuration", "complete", "browser_startup", "warm_unpublished_public_cache", "warm_published_public_cache",
    "owner_invitation_page_load", "owner_invitation_prepare_form", "owner_invitation_submit",
    "owner_invitation_mailpit_delivery", "editor_work_prepare_form", "editor_work_submit", "editor_work_success_feedback",
}
BROWSER_FAILURE_CODES = {
    "invitation_create_http_status", "new_work_create_http_status", "new_work_form_error_after_success",
}


def merge_browser_report(code: int, details: Any, report: dict[str, Any]) -> None:
    """Retain fixed contract names and bounded metrics, never arbitrary child output."""
    if not isinstance(details, dict):
        raise RuntimeError("invalid browser evidence")
    stage, failure = details.get("stage"), details.get("failure_code")
    if isinstance(stage, str) and stage in BROWSER_STAGES:
        report["browser_stage"] = stage
    if isinstance(failure, str) and failure in BROWSER_FAILURE_CODES:
        report["browser_failure_code"] = failure
    if code != 0 or details.get("status") != "passed":
        raise RuntimeError("real browser contract failed")
    checks, cache = details.get("checks"), details.get("cache")
    if not isinstance(checks, dict) or set(checks) != BROWSER_CHECKS or any(value != "passed" for value in checks.values()):
        raise RuntimeError("incomplete browser check evidence")
    if not isinstance(cache, dict) or any(type(cache.get(key)) is not int or cache[key] != value for key, value in {
        "ttl_seconds": 300, "observation_timeout_seconds": 60, "manual_revalidation_requests": 0,
    }.items()):
        raise RuntimeError("invalid cache contract evidence")
    sanitized = {key: cache[key] for key in ("ttl_seconds", "observation_timeout_seconds", "manual_revalidation_requests")}
    for key, expected in {
        "cached_surfaces": ["home", "detail", "sitemap"], "uncached_visibility_surfaces": ["search"],
    }.items():
        if cache.get(key) != expected:
            raise RuntimeError("invalid cached versus uncached evidence scope")
        sanitized[key] = list(expected)
    for event in ("publication", "hidden"):
        elapsed, surfaces = cache.get(f"{event}_elapsed_ms"), cache.get(f"{event}_surfaces")
        if type(elapsed) is not int or not 0 <= elapsed < 60000 or surfaces != ["home", "search", "detail", "sitemap"]:
            raise RuntimeError("cache observation exceeded the verified contract")
        sanitized[f"{event}_elapsed_ms"] = elapsed
        sanitized[f"{event}_surfaces"] = list(surfaces)
    report["cache"] = sanitized
    report["browser_profiles"] = {"owner": "desktop", "editor": "mobile", "anonymous": ["desktop", "mobile"]}
    report["checks"].update({f"browser_{name}": "passed" for name in sorted(BROWSER_CHECKS)})
    report["checks"]["real_browser_flow"] = "passed"


def validate_environment(environment: dict[str, str]) -> tuple[str, str]:
    if environment.get(CONFIRMATION) != "disposable-database":
        raise ValueError("real stack requires disposable database confirmation")
    api_url = core.validate_database({
        "CONFIRM_RELEASE_A_CORE_E2E": "disposable-database",
        "CORE_E2E_DATABASE_URL": environment.get("CORE_E2E_DATABASE_URL", ""),
    })
    worker_url = environment.get("REAL_STACK_WORKER_DATABASE_URL", "")
    api, worker = urllib.parse.urlsplit(api_url), urllib.parse.urlsplit(worker_url)
    if (
        worker.scheme not in {"postgres", "postgresql"}
        or worker.username != "platform_worker_login"
        or not worker.password
        or worker.hostname != api.hostname
        or worker.port != api.port
        or worker.path != api.path
        or worker.query not in {"", "sslmode=disable"}
        or worker.fragment
        or "#" in worker_url
    ):
        raise ValueError("Worker must use its restricted login in the same dedicated loopback database")
    return api_url, worker_url


def inherited_environment() -> dict[str, str]:
    allowed = {"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "HOME", "LANG", "LC_ALL", "PLAYWRIGHT_BROWSERS_PATH"}
    result = {key: value for key, value in os.environ.items() if key.upper() in allowed}
    result.update({"NO_PROXY": "*", "no_proxy": "*"})
    return result


def runtime_environments(
    api_url: str, worker_url: str, ports: tuple[int, int, int, int], version: str,
    run_id: str, owner_email: str, owner_password: str,
) -> tuple[dict[str, str], dict[str, str], dict[str, str]]:
    api_port, worker_port, display_port, ops_port = ports
    api_origin, worker_origin = f"http://127.0.0.1:{api_port}", f"http://127.0.0.1:{worker_port}"
    display_origin, ops_origin = f"http://127.0.0.1:{display_port}", f"http://127.0.0.1:{ops_port}"
    cache_secret = secrets.token_urlsafe(48)
    api_environment = core.runtime_environment(api_url, api_port, version)
    api_environment.update({"ALLOWED_ORIGINS": f"{display_origin},{ops_origin}", "NO_PROXY": "*", "no_proxy": "*"})
    worker_environment = {
        **inherited_environment(), "BUILD_VERSION": version, "DATABASE_URL": worker_url,
        "WORKER_ADDR": f"127.0.0.1:{worker_port}", "WORKER_REGION": "japan", "WORKER_ID": f"real-stack-{run_id}",
        "WORKER_POLL_INTERVAL": "250ms", "WORKER_LEASE_DURATION": "30s", "COLLECTION_ENABLED": "false",
        "DISPLAY_REVALIDATE_URL": f"{display_origin}/api/internal/revalidate", "CACHE_HMAC_SECRET": cache_secret,
        # This suite never creates media objects. Unexpected media work must
        # fail locally rather than reaching a configured provider or deleting files.
        "MEDIA_DELETE_URL": "http://127.0.0.1:9/internal/media/delete",
        "MEDIA_HMAC_SECRET": secrets.token_urlsafe(48),
        "MEDIA_RECONCILE_ENABLED": "false", "MEDIA_INSPECT_ENABLED": "false",
        "MEDIA_USAGE_MONITOR_MODE": "off", "MEDIA_UPLOAD_QUEUE_MODE": "off",
        "MEDIA_UPLOAD_ADMISSION_MODE": "off", "HISTORY_ARCHIVE_ENABLED": "false",
    }
    browser_environment = {
        **inherited_environment(), "CONFIRM_RELEASE_A_BROWSER_E2E": "disposable-database",
        "REAL_BROWSER_API_URL": api_origin, "REAL_BROWSER_WORKER_URL": worker_origin,
        "REAL_BROWSER_DISPLAY_URL": display_origin, "REAL_BROWSER_OPS_URL": ops_origin,
        "REAL_BROWSER_MAILPIT_URL": core.MAILPIT,
        "REAL_BROWSER_OWNER_EMAIL": owner_email, "REAL_BROWSER_OWNER_PASSWORD": owner_password,
        "REAL_BROWSER_RUN_ID": run_id, "REAL_BROWSER_BUILD_VERSION": version, "CACHE_HMAC_SECRET": cache_secret,
    }
    return api_environment, worker_environment, browser_environment


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req: Any, fp: Any, code: int, msg: str, headers: Any, newurl: str) -> None:
        return None


def wait_service(process: subprocess.Popen[bytes], origin: str, version: str, timeout: float) -> None:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError("owned service exited before readiness")
        try:
            for path in ("/healthz", "/readyz"):
                with opener.open(origin + path, timeout=2) as response:
                    body = response.read(32769)
                    if len(body) > 32768:
                        raise RuntimeError("unexpected service health response")
                    payload = json.loads(body)
                    if response.status != 200 or not isinstance(payload, dict):
                        raise RuntimeError("unexpected service health response")
                    if path == "/healthz" and payload.get("version") != version:
                        raise RuntimeError("another service owns the selected port")
            return
        except (urllib.error.URLError, TimeoutError, OSError):
            time.sleep(0.25)
    raise TimeoutError("owned service did not become ready")


def process_start_ticks(pid: int) -> str | None:
    """Linux start identity prevents a reused PID from becoming a cleanup target."""
    if sys.platform != "linux" or type(pid) is not int or pid < 2:
        return None
    try:
        fields = Path(f"/proc/{pid}/stat").read_text(encoding="utf-8").rsplit(") ", 1)[1].split()
        return fields[19] if fields[19].isascii() and fields[19].isdigit() else None
    except (OSError, IndexError):
        return None


def registered_browser_group(registry: Path | None, node_pid: int, node_start_ticks: str | None) -> tuple[int, str] | None:
    if registry is None or node_start_ticks is None or not registry.exists():
        return None
    try:
        # Only the owned Node process can write this private temporary directory.
        # Never trust a path, command, extra PID or arbitrary process group from it.
        if registry.is_symlink() or not registry.is_file() or registry.stat().st_size > 4096:
            raise ValueError
        record = json.loads(registry.read_text(encoding="utf-8"))
        if not isinstance(record, dict) or set(record) != {"node_pid", "node_start_ticks", "browser_pid", "browser_start_ticks"}:
            raise ValueError
        if type(record["node_pid"]) is not int or record["node_pid"] != node_pid or record["node_start_ticks"] != node_start_ticks:
            raise ValueError
        pid, ticks = record["browser_pid"], record["browser_start_ticks"]
        if type(pid) is not int or pid < 2 or pid in {node_pid, os.getpid()} or not isinstance(ticks, str) or not ticks.isascii() or not ticks.isdigit():
            raise ValueError
        current = process_start_ticks(pid)
        if current is not None and (current != ticks or os.getpgid(pid) != pid):
            raise ValueError
        # An absent Chromium leader does not imply that its renderer group is
        # gone. The private registration already bound this group to our Node.
        return pid, ticks
    except (OSError, ValueError, TypeError) as error:
        raise RuntimeError("invalid owned browser process registration") from error


def stop_browser(
    process: subprocess.Popen[bytes], *, registry: Path | None = None,
    node_start_ticks: str | None = None, grace_seconds: float = 10,
) -> None:
    if os.name != "posix":
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=grace_seconds)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
        return
    groups = [(process.pid, node_start_ticks)]
    registration_error = None
    try:
        registered = registered_browser_group(registry, process.pid, node_start_ticks)
        if registered is not None:
            groups.append(registered)
    except RuntimeError as error:
        registration_error = error

    def signal_group(pid: int, ticks: str | None, value: signal.Signals | int) -> bool:
        current = process_start_ticks(pid)
        if ticks is not None and current is not None and current != ticks:
            return False
        try:
            # Node gets start_new_session; Chromium gets its separately verified
            # process group. An exited leader can still have live group members.
            os.killpg(pid, value)
            return True
        except ProcessLookupError:
            return False

    active = [(pid, ticks) for pid, ticks in groups if signal_group(pid, ticks, signal.SIGTERM)]
    deadline = time.monotonic() + grace_seconds
    while active and time.monotonic() < deadline:
        process.poll()  # Reap the owned Node leader without mistaking it for the tree.
        active = [(pid, ticks) for pid, ticks in active if signal_group(pid, ticks, 0)]
        if active:
            time.sleep(0.05)
    for pid, ticks in active:
        signal_group(pid, ticks, signal.SIGKILL)
    process.wait(timeout=5)
    if registration_error is not None:
        raise registration_error


def execute(args: argparse.Namespace, report: dict[str, Any]) -> None:
    api_url, worker_url = validate_environment(dict(os.environ))
    binaries = (args.api_binary.resolve(), args.worker_binary.resolve(), args.owner_binary.resolve())
    if any(not binary.is_file() for binary in binaries) or not args.browser_script.resolve().is_file():
        raise ValueError("build all three Go binaries and provide the real browser script first")
    ports: list[int] = []
    while len(ports) < 4:
        port = core.available_port()
        if port not in ports:
            ports.append(port)
    run_id = secrets.token_hex(8)
    version = f"real-stack-{run_id}"
    owner_email, owner_password = f"owner-{run_id}@example.test", secrets.token_urlsafe(32)
    environments = runtime_environments(
        api_url, worker_url, (ports[0], ports[1], ports[2], ports[3]), version, run_id, owner_email, owner_password,
    )
    api_environment, worker_environment, browser_environment = environments
    processes: list[subprocess.Popen[bytes]] = []
    with tempfile.TemporaryDirectory(prefix="release-a-real-stack-") as directory:
        temporary = Path(directory)
        with (temporary / "process.log").open("wb") as log:
            report["stage"] = "owner_bootstrap"
            subprocess.run(
                [str(binaries[2])], cwd=ROOT,
                env={**api_environment, "BOOTSTRAP_OWNER_EMAIL": owner_email, "BOOTSTRAP_OWNER_PASSWORD": owner_password},
                stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, timeout=30, check=True,
            )
            try:
                for name, binary, environment, origin in (
                    ("api", binaries[0], api_environment, browser_environment["REAL_BROWSER_API_URL"]),
                    ("worker", binaries[1], worker_environment, browser_environment["REAL_BROWSER_WORKER_URL"]),
                ):
                    report["stage"] = f"{name}_startup"
                    process = subprocess.Popen(
                        [str(binary)], cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                        stdout=log, stderr=subprocess.STDOUT, creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
                    )
                    processes.append(process)
                    wait_service(process, origin, version, args.timeout)
                    report["checks"][f"{name}_readiness"] = "passed"
                report["stage"] = "real_browser_flows"
                browser_report = temporary / "browser.json"
                browser_registry = temporary / "browser-process.json"
                browser_environment["REAL_BROWSER_PROCESS_REGISTRY"] = str(browser_registry)
                browser = subprocess.Popen(
                    [args.node, str(args.browser_script.resolve()), "--output", str(browser_report)],
                    cwd=ROOT, env=browser_environment, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                    start_new_session=os.name == "posix", creationflags=getattr(subprocess, "CREATE_NO_WINDOW", 0),
                )
                browser_start_ticks = process_start_ticks(browser.pid)
                try:
                    code = browser.wait(timeout=args.browser_timeout)
                finally:
                    stop_browser(browser, registry=browser_registry, node_start_ticks=browser_start_ticks)
                if not browser_report.is_file() or browser_report.stat().st_size > 65536:
                    raise RuntimeError("browser evidence missing or too large")
                details = json.loads(browser_report.read_text(encoding="utf-8"))
                merge_browser_report(code, details, report)
            finally:
                for process in reversed(processes):
                    core.stop_process(process)
    report["status"], report["stage"] = "passed", "completed"


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__, allow_abbrev=False)
    suffix = ".exe" if sys.platform == "win32" else ""
    parser.add_argument("--api-binary", type=Path, default=ROOT / f".cache/core-e2e/platform-api{suffix}")
    parser.add_argument("--worker-binary", type=Path, default=ROOT / f".cache/core-e2e/platform-worker{suffix}")
    parser.add_argument("--owner-binary", type=Path, default=ROOT / f".cache/core-e2e/create-owner{suffix}")
    parser.add_argument("--browser-script", type=Path, default=ROOT / "scripts/release_a_real_browser_e2e.mjs")
    parser.add_argument("--node", default="node")
    parser.add_argument("--output", type=Path, default=ROOT / "docs/evidence/release-a-real-stack-ci.json")
    parser.add_argument("--timeout", type=float, default=30)
    parser.add_argument("--browser-timeout", type=float, default=480)
    args = parser.parse_args(argv)
    if not 1 <= args.timeout <= 60 or not 60 <= args.browser_timeout <= 900:
        parser.error("service timeout must be 1..60 seconds and browser timeout 60..900 seconds")
    return args


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report: dict[str, Any] = {
        "status": "failed", "stage": "configuration", "checks": {},
        "scope": "real-go-api-worker-postgresql-mailpit-production-next-playwright",
        "started_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "not_covered": ["real_content", "turnstile", "external_smtp", "s3", "cloudflare", "target_servers", "production_load"],
    }
    try:
        execute(args, report)
    except Exception as error:
        report["error_type"] = type(error).__name__
    report["finished_at"] = datetime.now(timezone.utc).isoformat(timespec="seconds")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
