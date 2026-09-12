from __future__ import annotations

import argparse
import ipaddress
import json
import os
import sys
import time
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Callable
from urllib.error import HTTPError, URLError
from urllib.parse import SplitResult, urljoin, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

MAX_RESPONSE_BYTES = 2 * 1024 * 1024
FORBIDDEN_AD_SIGNALS = (
    "pagead2.googlesyndication.com",
    "googleads.g.doubleclick.net",
    "adsbygoogle",
    'data-ad-state="active"',
    'data-ad-behavior="floating"',
    "data-ad-close-countdown",
    "popunder",
)


@dataclass(frozen=True)
class ProbeConfig:
    display_url: str
    ops_url: str
    api_url: str
    worker_url: str
    expected_version: str
    expected_worker_region: str
    ops_access_client_id: str | None = None
    ops_access_client_secret: str | None = None
    metrics_url: str | None = None
    metrics_token: str | None = None
    timeout: float = 5.0


@dataclass(frozen=True)
class ProbeResponse:
    status: int
    headers: dict[str, str]
    body: str
    elapsed_ms: int
    final_url: str


@dataclass(frozen=True)
class CheckResult:
    name: str
    status: str
    url: str
    status_code: int | None
    elapsed_ms: int | None
    failures: list[str]


Fetcher = Callable[[str, dict[str, str], float], ProbeResponse]


class NoRedirectHandler(HTTPRedirectHandler):
    def redirect_request(self, _request: object, _file_pointer: object, _code: int, _message: str, _headers: object, _new_url: str) -> None:
        return None


HTTP_OPENER = build_opener(NoRedirectHandler())


def normalized_base_url(value: str, *, public: bool) -> str:
    parsed = urlsplit(value.strip())
    try:
        parsed.port
    except ValueError as exc:
        raise ValueError("URL contains an invalid port") from exc
    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        raise ValueError("URL must be an absolute HTTP(S) origin")
    if parsed.path not in ("", "/") or parsed.query or parsed.fragment or parsed.username or parsed.password:
        raise ValueError("URL must be an origin without path, credentials, query, or fragment")
    if public and parsed.scheme != "https" and not is_local_or_private_host(parsed.hostname):
        raise ValueError("public URL must use HTTPS outside localhost or a private network")
    return origin(parsed)


def is_local_or_private_host(host: str) -> bool:
    if host.lower() == "localhost":
        return True
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        return False
    return address.is_loopback or address.is_private


def origin(parsed: SplitResult) -> str:
    host = parsed.hostname or ""
    if ":" in host:
        host = f"[{host}]"
    default_port = (parsed.scheme == "https" and parsed.port == 443) or (parsed.scheme == "http" and parsed.port == 80)
    port = "" if parsed.port is None or default_port else f":{parsed.port}"
    return f"{parsed.scheme}://{host}{port}"


def fetch_url(url: str, headers: dict[str, str], timeout: float) -> ProbeResponse:
    request_headers = {
        "Accept": headers.get("Accept", "application/json, text/html;q=0.9, text/plain;q=0.8"),
        "User-Agent": "self-deepsearch-release-a-probe/1",
        **headers,
    }
    request = Request(url, headers=request_headers, method="GET")
    started = time.perf_counter()
    try:
        response = HTTP_OPENER.open(request, timeout=timeout)
    except HTTPError as error:
        response = error
    try:
        body = response.read(MAX_RESPONSE_BYTES + 1)
        if len(body) > MAX_RESPONSE_BYTES:
            raise ValueError("response exceeds 2 MiB probe limit")
        return ProbeResponse(
            status=response.status,
            headers={key.lower(): value for key, value in response.headers.items()},
            body=body.decode("utf-8", errors="replace"),
            elapsed_ms=round((time.perf_counter() - started) * 1000),
            final_url=response.geturl(),
        )
    finally:
        response.close()


def security_header_failures(response: ProbeResponse, *, require_hsts: bool) -> list[str]:
    failures: list[str] = []
    csp = response.headers.get("content-security-policy", "")
    for directive in ("default-src", "object-src 'none'", "frame-ancestors 'none'"):
        if directive not in csp:
            failures.append(f"CSP missing {directive}")
    if response.headers.get("x-content-type-options", "").lower() != "nosniff":
        failures.append("X-Content-Type-Options must be nosniff")
    if response.headers.get("x-frame-options", "").upper() != "DENY":
        failures.append("X-Frame-Options must be DENY")
    if not response.headers.get("referrer-policy"):
        failures.append("Referrer-Policy is missing")
    if not response.headers.get("permissions-policy"):
        failures.append("Permissions-Policy is missing")
    if require_hsts and not response.headers.get("strict-transport-security"):
        failures.append("Strict-Transport-Security is missing")
    return failures


def json_body(response: ProbeResponse) -> tuple[dict[str, object] | None, list[str]]:
    try:
        value = json.loads(response.body)
    except json.JSONDecodeError:
        return None, ["response is not valid JSON"]
    if not isinstance(value, dict):
        return None, ["JSON response must be an object"]
    return value, []


def same_origin_failure(request_url: str, response: ProbeResponse) -> list[str]:
    if origin(urlsplit(request_url)) != origin(urlsplit(response.final_url)):
        return ["request redirected to a different origin"]
    return []


def run_probe(configuration: ProbeConfig, fetcher: Fetcher = fetch_url) -> list[CheckResult]:
    checks: list[CheckResult] = []

    def execute(
        name: str,
        url: str,
        validator: Callable[[ProbeResponse], list[str]],
        headers: dict[str, str] | None = None,
    ) -> None:
        try:
            response = fetcher(url, headers or {}, configuration.timeout)
            failures = same_origin_failure(url, response) + validator(response)
            checks.append(
                CheckResult(
                    name=name,
                    status="passed" if not failures else "failed",
                    url=url,
                    status_code=response.status,
                    elapsed_ms=response.elapsed_ms,
                    failures=failures,
                )
            )
        except (OSError, URLError, ValueError, TimeoutError) as exc:
            checks.append(
                CheckResult(
                    name=name,
                    status="failed",
                    url=url,
                    status_code=None,
                    elapsed_ms=None,
                    failures=[f"request failed ({exc.__class__.__name__})"],
                )
            )

    display_root = urljoin(configuration.display_url + "/", "/")
    ops_login = urljoin(configuration.ops_url + "/", "login")
    api_health = urljoin(configuration.api_url + "/", "healthz")
    api_ready = urljoin(configuration.api_url + "/", "readyz")
    worker_ready = urljoin(configuration.worker_url + "/", "readyz")

    def validate_display(response: ProbeResponse) -> list[str]:
        failures = [] if response.status == 200 else [f"expected HTTP 200, got {response.status}"]
        for signal in (
            "资料和图片来自经审核的公开来源",
            "本站不提供视频、音频、下载、磁力、网盘或资源跳转",
            "仅限 18 岁以上用户",
            'data-ad-state="placeholder"',
            "测试占位，不加载第三方脚本",
        ):
            if signal not in response.body:
                failures.append(f"page is missing required signal: {signal}")
        lowered = response.body.lower()
        for signal in FORBIDDEN_AD_SIGNALS:
            if signal in lowered:
                failures.append(f"Release A contains forbidden ad signal: {signal}")
        cache_control = response.headers.get("cache-control", "").lower()
        if "public" not in cache_control or "no-store" in cache_control:
            failures.append("public homepage must use a public cache policy")
        failures.extend(
            security_header_failures(response, require_hsts=urlsplit(configuration.display_url).scheme == "https")
        )
        return failures

    def validate_ops(response: ProbeResponse) -> list[str]:
        failures = [] if response.status == 200 else [f"expected HTTP 200, got {response.status}"]
        for signal in ("运营后台", "后台登录"):
            if signal not in response.body:
                failures.append(f"page is missing required signal: {signal}")
        if "no-store" not in response.headers.get("cache-control", "").lower():
            failures.append("ops login must use Cache-Control: no-store")
        failures.extend(security_header_failures(response, require_hsts=urlsplit(configuration.ops_url).scheme == "https"))
        return failures

    def validate_api(response: ProbeResponse, expected_status: str) -> list[str]:
        failures = [] if response.status == 200 else [f"expected HTTP 200, got {response.status}"]
        body, body_failures = json_body(response)
        failures.extend(body_failures)
        if body:
            expected = {
                "status": expected_status,
                "service": "platform-api",
                "version": configuration.expected_version,
            }
            for key, value in expected.items():
                if body.get(key) != value:
                    failures.append(f"JSON field {key} does not match the expected release")
            if expected_status == "ready":
                dependencies = body.get("dependencies")
                database_ready = isinstance(dependencies, list) and any(
                    isinstance(item, dict) and item.get("name") == "database" and item.get("status") == "ok"
                    for item in dependencies
                )
                if not database_ready:
                    failures.append("database dependency is not ready")
        if "no-store" not in response.headers.get("cache-control", "").lower():
            failures.append("API health surface must use Cache-Control: no-store")
        failures.extend(security_header_failures(response, require_hsts=urlsplit(configuration.api_url).scheme == "https"))
        return failures

    def validate_worker(response: ProbeResponse) -> list[str]:
        failures = [] if response.status == 200 else [f"expected HTTP 200, got {response.status}"]
        body, body_failures = json_body(response)
        failures.extend(body_failures)
        if body:
            expected = {
                "status": "ready",
                "service": "platform-worker",
                "version": configuration.expected_version,
                "region": configuration.expected_worker_region,
            }
            for key, value in expected.items():
                if body.get(key) != value:
                    failures.append(f"JSON field {key} does not match the expected release")
            capabilities = body.get("capabilities")
            if not isinstance(capabilities, list) or not capabilities:
                failures.append("worker capabilities are missing")
        return failures

    ops_headers = {"Accept": "text/html"}
    if configuration.ops_access_client_id and configuration.ops_access_client_secret:
        ops_headers["CF-Access-Client-Id"] = configuration.ops_access_client_id
        ops_headers["CF-Access-Client-Secret"] = configuration.ops_access_client_secret

    execute("display-home", display_root, validate_display, {"Accept": "text/html"})
    execute("ops-login", ops_login, validate_ops, ops_headers)
    execute("api-health", api_health, lambda response: validate_api(response, "ok"))
    execute("api-ready", api_ready, lambda response: validate_api(response, "ready"))
    execute("worker-ready", worker_ready, validate_worker)

    if configuration.metrics_url:
        metrics_url = urljoin(configuration.metrics_url + "/", "metrics")
        if not configuration.metrics_token:
            checks.append(
                CheckResult(
                    name="metrics",
                    status="failed",
                    url=metrics_url,
                    status_code=None,
                    elapsed_ms=None,
                    failures=["metrics token environment variable is empty"],
                )
            )
        else:

            def validate_metrics(response: ProbeResponse) -> list[str]:
                failures = [] if response.status == 200 else [f"expected HTTP 200, got {response.status}"]
                if "no-store" not in response.headers.get("cache-control", "").lower():
                    failures.append("metrics must use Cache-Control: no-store")
                if not any(
                    signal in response.body for signal in ("self_deepsearch_build_info", "self_deepsearch_worker_info")
                ):
                    failures.append("metrics are missing API or Worker build information")
                if configuration.expected_version not in response.body:
                    failures.append(f"metrics are missing required version: {configuration.expected_version}")
                return failures

            execute(
                "metrics",
                metrics_url,
                validate_metrics,
                {"Accept": "text/plain", "Authorization": f"Bearer {configuration.metrics_token}"},
            )
    return checks


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run read-only Release A checks against a deployed environment")
    parser.add_argument("--display-url", required=True)
    parser.add_argument("--ops-url", required=True)
    parser.add_argument("--api-url", required=True)
    parser.add_argument("--worker-url", required=True)
    parser.add_argument("--expected-version", required=True)
    parser.add_argument("--expected-worker-region", default="japan")
    parser.add_argument("--ops-access-client-id-env", default="CF_ACCESS_CLIENT_ID")
    parser.add_argument("--ops-access-client-secret-env", default="CF_ACCESS_CLIENT_SECRET")
    parser.add_argument("--metrics-url")
    parser.add_argument("--metrics-token-env", default="METRICS_TOKEN")
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--output", type=Path)
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    if args.timeout <= 0 or args.timeout > 30:
        print("ERROR --timeout must be greater than 0 and no more than 30 seconds", file=sys.stderr)
        return 2
    ops_access_client_id = os.getenv(args.ops_access_client_id_env)
    ops_access_client_secret = os.getenv(args.ops_access_client_secret_env)
    if bool(ops_access_client_id) != bool(ops_access_client_secret):
        print("ERROR both Cloudflare Access credential environment variables must be set together", file=sys.stderr)
        return 2
    try:
        configuration = ProbeConfig(
            display_url=normalized_base_url(args.display_url, public=True),
            ops_url=normalized_base_url(args.ops_url, public=True),
            api_url=normalized_base_url(args.api_url, public=True),
            worker_url=normalized_base_url(args.worker_url, public=False),
            expected_version=args.expected_version.strip(),
            expected_worker_region=args.expected_worker_region.strip(),
            ops_access_client_id=ops_access_client_id,
            ops_access_client_secret=ops_access_client_secret,
            metrics_url=normalized_base_url(args.metrics_url, public=False) if args.metrics_url else None,
            metrics_token=os.getenv(args.metrics_token_env) if args.metrics_url else None,
            timeout=args.timeout,
        )
    except ValueError as exc:
        print(f"ERROR invalid deployment URL: {exc}", file=sys.stderr)
        return 2
    if not configuration.expected_version or configuration.expected_version in {"dev", "latest"}:
        print("ERROR --expected-version must identify an immutable release", file=sys.stderr)
        return 2

    checks = run_probe(configuration)
    payload = {
        "status": "passed" if all(check.status == "passed" for check in checks) else "failed",
        "checked_at": datetime.now(UTC).isoformat(),
        "expected_version": configuration.expected_version,
        "checks": [asdict(check) for check in checks],
    }
    rendered = json.dumps(payload, ensure_ascii=False, indent=2)
    print(rendered)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered + "\n", encoding="utf-8")
    return 0 if payload["status"] == "passed" else 1


if __name__ == "__main__":
    raise SystemExit(main())
