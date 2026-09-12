from __future__ import annotations

import importlib.util
import io
import json
import os
import sys
import tempfile
import threading
import unittest
from contextlib import redirect_stderr, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

SCRIPT = Path(__file__).resolve().parents[1] / "release_a_deployment_probe.py"
SPEC = importlib.util.spec_from_file_location("release_a_deployment_probe", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


VERSION = "2026.09.09-a1"


def response(url: str, body: str, *, cache: str, status: int = 200, html: bool = False) -> Any:
    headers = {
        "content-type": "text/html; charset=utf-8" if html else "application/json; charset=utf-8",
        "cache-control": cache,
        "content-security-policy": "default-src 'self'; object-src 'none'; frame-ancestors 'none'",
        "x-content-type-options": "nosniff",
        "x-frame-options": "DENY",
        "referrer-policy": "no-referrer",
        "permissions-policy": "camera=()",
    }
    return MODULE.ProbeResponse(status=status, headers=headers, body=body, elapsed_ms=12, final_url=url)


def valid_responses() -> dict[str, Any]:
    display = "http://127.0.0.1:3000/"
    ops = "http://127.0.0.1:3001/login"
    api = "http://127.0.0.1:8080"
    worker = "http://127.0.0.1:8081"
    return {
        display: response(
            display,
            "资料和图片来自经审核的公开来源 本站不提供视频、音频、下载、磁力、网盘或资源跳转 "
            '仅限 18 岁以上用户 data-ad-state="placeholder" 测试占位，不加载第三方脚本',
            cache="public, s-maxage=300",
            html=True,
        ),
        ops: response(ops, "幕鉴 运营后台 后台登录", cache="private, no-store", html=True),
        f"{api}/healthz": response(
            f"{api}/healthz", json.dumps({"status": "ok", "service": "platform-api", "version": VERSION}), cache="no-store"
        ),
        f"{api}/readyz": response(
            f"{api}/readyz",
            json.dumps(
                {
                    "status": "ready",
                    "service": "platform-api",
                    "version": VERSION,
                    "dependencies": [{"name": "database", "status": "ok"}],
                }
            ),
            cache="no-store",
        ),
        f"{worker}/readyz": response(
            f"{worker}/readyz",
            json.dumps(
                {
                    "status": "ready",
                    "service": "platform-worker",
                    "version": VERSION,
                    "region": "japan",
                    "capabilities": ["publish"],
                }
            ),
            cache="no-store",
        ),
        f"{worker}/metrics": MODULE.ProbeResponse(
            status=200,
            headers={"cache-control": "no-store", "content-type": "text/plain"},
            body=f'self_deepsearch_worker_info{{version="{VERSION}"}} 1',
            elapsed_ms=5,
            final_url=f"{worker}/metrics",
        ),
    }


def configuration(*, metrics: bool = False, access: bool = False) -> Any:
    return MODULE.ProbeConfig(
        display_url="http://127.0.0.1:3000",
        ops_url="http://127.0.0.1:3001",
        api_url="http://127.0.0.1:8080",
        worker_url="http://127.0.0.1:8081",
        expected_version=VERSION,
        expected_worker_region="japan",
        ops_access_client_id="access-client-id" if access else None,
        ops_access_client_secret="access-client-secret" if access else None,
        metrics_url="http://127.0.0.1:8081" if metrics else None,
        metrics_token="secret-token" if metrics else None,
    )


class FakeFetcher:
    def __init__(self, responses: dict[str, Any]) -> None:
        self.responses = responses
        self.calls: list[tuple[str, dict[str, str]]] = []

    def __call__(self, url: str, headers: dict[str, str], _timeout: float) -> Any:
        self.calls.append((url, headers))
        if url not in self.responses:
            raise OSError("unavailable")
        return self.responses[url]


class ReleaseADeploymentProbeTests(unittest.TestCase):
    def test_valid_read_only_deployment_passes_all_checks(self) -> None:
        fetcher = FakeFetcher(valid_responses())
        checks = MODULE.run_probe(configuration(metrics=True, access=True), fetcher)
        self.assertEqual(len(checks), 6)
        self.assertTrue(all(check.status == "passed" for check in checks), checks)
        self.assertEqual(fetcher.calls[-1][1]["Authorization"], "Bearer secret-token")
        ops_headers = next(headers for url, headers in fetcher.calls if url.endswith("/login"))
        self.assertEqual(ops_headers["CF-Access-Client-Id"], "access-client-id")
        self.assertEqual(ops_headers["CF-Access-Client-Secret"], "access-client-secret")
        self.assertTrue(all("CF-Access-Client-Secret" not in headers for url, headers in fetcher.calls if not url.endswith("/login")))
        self.assertTrue(all(url.endswith(("/", "/login", "/healthz", "/readyz", "/metrics")) for url, _ in fetcher.calls))

    def test_version_database_ads_cache_and_redirect_failures_are_reported(self) -> None:
        responses = valid_responses()
        api_ready = "http://127.0.0.1:8080/readyz"
        responses[api_ready] = response(
            api_ready,
            json.dumps({"status": "ready", "service": "platform-api", "version": "old", "dependencies": []}),
            cache="public",
        )
        display = "http://127.0.0.1:3000/"
        responses[display] = response(
            "http://different.example/",
            responses[display].body + " adsbygoogle",
            cache="no-store",
            html=True,
        )
        checks = MODULE.run_probe(configuration(), FakeFetcher(responses))
        failures = "\n".join(failure for check in checks for failure in check.failures)
        self.assertIn("request redirected to a different origin", failures)
        self.assertIn("forbidden ad signal", failures)
        self.assertIn("public homepage must use a public cache policy", failures)
        self.assertIn("database dependency is not ready", failures)
        self.assertIn("JSON field version", failures)

    def test_missing_metrics_token_fails_without_sending_request(self) -> None:
        fetcher = FakeFetcher(valid_responses())
        config = configuration(metrics=True)
        config = MODULE.ProbeConfig(**{**config.__dict__, "metrics_token": None})
        checks = MODULE.run_probe(config, fetcher)
        self.assertEqual(checks[-1].name, "metrics")
        self.assertEqual(checks[-1].status, "failed")
        self.assertNotIn("Authorization", "\n".join(str(headers) for _, headers in fetcher.calls))

    def test_url_validation_requires_safe_public_origins_and_http_redirects_are_not_followed(self) -> None:
        self.assertEqual(MODULE.normalized_base_url("http://127.0.0.1:8080/", public=True), "http://127.0.0.1:8080")
        with self.assertRaisesRegex(ValueError, "HTTPS"):
            MODULE.normalized_base_url("http://public.example.test", public=True)
        with self.assertRaisesRegex(ValueError, "without path"):
            MODULE.normalized_base_url("https://display.example.test/path", public=True)

        destination_requests: list[dict[str, str]] = []

        class DestinationHandler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                destination_requests.append({key.lower(): value for key, value in self.headers.items()})
                self.send_response(200)
                self.end_headers()

            def log_message(self, _format: str, *args: object) -> None:
                del args

        destination = ThreadingHTTPServer(("127.0.0.1", 0), DestinationHandler)
        destination_thread = threading.Thread(target=destination.serve_forever, daemon=True)
        destination_thread.start()
        destination_url = f"http://127.0.0.1:{destination.server_port}/should-not-be-requested"

        class RedirectHandler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                self.send_response(302)
                self.send_header("Location", destination_url)
                self.end_headers()

            def log_message(self, _format: str, *args: object) -> None:
                del args

        redirect = ThreadingHTTPServer(("127.0.0.1", 0), RedirectHandler)
        redirect_thread = threading.Thread(target=redirect.serve_forever, daemon=True)
        redirect_thread.start()
        redirect_url = f"http://127.0.0.1:{redirect.server_port}/redirect"
        try:
            redirect_response = MODULE.fetch_url(
                redirect_url,
                {
                    "Authorization": "Bearer must-not-follow",
                    "CF-Access-Client-Secret": "must-not-follow",
                },
                2,
            )
            self.assertEqual(redirect_response.status, 302)
            self.assertEqual(redirect_response.final_url, redirect_url)
            self.assertEqual(destination_requests, [])
        finally:
            redirect.shutdown()
            redirect.server_close()
            redirect_thread.join(timeout=2)
            destination.shutdown()
            destination.server_close()
            destination_thread.join(timeout=2)

    def test_cli_rejects_dev_version_and_never_reads_metrics_token_without_metrics(self) -> None:
        previous = os.environ.pop("DO_NOT_READ_TOKEN", None)
        try:
            stdout, stderr = io.StringIO(), io.StringIO()
            with tempfile.TemporaryDirectory() as directory:
                output = Path(directory) / "probe.json"
                with redirect_stdout(stdout), redirect_stderr(stderr):
                    result = MODULE.main(
                        [
                            "--display-url",
                            "http://127.0.0.1:3000",
                            "--ops-url",
                            "http://127.0.0.1:3001",
                            "--api-url",
                            "http://127.0.0.1:8080",
                            "--worker-url",
                            "http://127.0.0.1:8081",
                            "--expected-version",
                            "dev",
                            "--metrics-token-env",
                            "DO_NOT_READ_TOKEN",
                            "--output",
                            str(output),
                        ]
                    )
                self.assertFalse(output.exists())
            self.assertEqual(result, 2)
            self.assertIn("immutable release", stderr.getvalue())
            self.assertNotIn("DO_NOT_READ_TOKEN=", stdout.getvalue() + stderr.getvalue())
        finally:
            if previous is not None:
                os.environ["DO_NOT_READ_TOKEN"] = previous


if __name__ == "__main__":
    unittest.main()
