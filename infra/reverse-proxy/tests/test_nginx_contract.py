from __future__ import annotations

import re
import unittest
from pathlib import Path

CONFIG = Path(__file__).resolve().parents[1] / "nginx.conf.template"
ROOT = CONFIG.parents[2]


class NginxContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.config = CONFIG.read_text(encoding="utf-8")

    def server(self, host_variable: str) -> str:
        match = re.search(
            rf"server \{{(?:(?!\n\}}).)*server_name \$\{{{host_variable}\}};(?:(?!\n\}}).)*\n\}}",
            self.config,
            re.DOTALL,
        )
        self.assertIsNotNone(match, f"missing server for {host_variable}")
        return match.group(0)

    def test_only_expected_public_upstreams_exist(self) -> None:
        self.assertIn("server display-web:3000", self.config)
        self.assertIn("server platform-api:8080", self.config)
        self.assertIn("server ops-web:3001", self.config)
        self.assertNotIn("platform-worker-japan:8081", self.config)
        self.assertNotIn("postgres:5432", self.config)
        self.assertNotIn("prometheus:9090", self.config)

    def test_template_filter_is_limited_to_host_variables(self) -> None:
        compose = (CONFIG.parents[1] / "compose" / "japan" / "compose.yaml").read_text(encoding="utf-8")
        match = re.search(r"NGINX_ENVSUBST_FILTER: '([^']+)'", compose)
        self.assertIsNotNone(match)
        # Compose escapes the regex end anchor as $$; the Nginx entrypoint
        # matches variable names with awk before passing them to envsubst.
        pattern = re.compile(match.group(1).replace("$$", "$"))
        for variable in ("DISPLAY_HOST", "API_HOST", "OPS_HOST"):
            self.assertIsNotNone(pattern.search(variable), variable)
        for variable in ("HOSTNAME", "PATH", "host", "uri", "DISPLAY_HOST_SECRET", "OTHER_API_HOST"):
            self.assertIsNone(pattern.search(variable), variable)

    def test_internal_revalidation_is_not_public(self) -> None:
        display = self.server("DISPLAY_HOST")
        self.assertIn("location ^~ /api/internal/", display)
        self.assertIn("return 404", display)

    def test_only_the_authenticated_media_edge_policy_path_reaches_api(self) -> None:
        api = self.server("API_HOST")
        self.assertIn("location = /edge/v1/media-delivery-policy", api)
        self.assertIn("proxy_pass http://platform_api", api)
        self.assertIn("location ^~ /edge/", api)
        self.assertIn("return 404", api.split("location ^~ /edge/", 1)[1])
        exact = api.split("location = /edge/v1/media-delivery-policy", 1)[1].split("location ^~ /edge/", 1)[0]
        self.assertNotIn("CF-Connecting-IP", exact)
        self.assertNotIn("X-Forwarded-For", exact)

    def test_dynamic_surfaces_are_never_cached(self) -> None:
        display = self.server("DISPLAY_HOST")
        self.assertIn("map $uri $display_cache_control", self.config)
        self.assertIn('default "public, max-age=300, s-maxage=300, stale-while-revalidate=60"', self.config)
        self.assertIn('~^/account(?:/|$) "private, no-store"', self.config)
        self.assertIn('~^/logout(?:/|$) "private, no-store"', self.config)
        self.assertIn('~^/search(?:/|$) "private, no-store"', self.config)
        self.assertIn("map $request_method $display_method_cache_control", self.config)
        self.assertIn('default "private, no-store"', self.config)
        self.assertIn("GET $display_cache_control", self.config)
        self.assertIn("HEAD $display_cache_control", self.config)
        self.assertIn("map $upstream_status $display_response_cache_control", self.config)
        self.assertIn('~^20[0-6]$ $display_method_cache_control', self.config)
        self.assertIn('default "private, no-store"', self.config)
        self.assertIn("map $upstream_http_x_media_delivery_mode $display_media_cache_control", self.config)
        self.assertIn("default $display_response_cache_control", self.config)
        self.assertIn('default_only "private, no-store"', self.config)
        self.assertIn("add_header Cache-Control $display_media_cache_control always", display)
        self.assertIn('add_header Cache-Control "no-store" always', self.server("API_HOST"))
        self.assertIn('add_header Cache-Control "private, no-store" always', self.server("OPS_HOST"))

    def test_security_headers_survive_nginx_add_header_inheritance(self) -> None:
        display = self.server("DISPLAY_HOST")
        self.assertNotIn("X-Frame-Options SAMEORIGIN", display)
        for header in (
            "Strict-Transport-Security",
            "X-Content-Type-Options",
            "X-Frame-Options DENY",
            "Referrer-Policy",
            "Permissions-Policy",
            "Content-Security-Policy",
        ):
            self.assertEqual(display.count(f"add_header {header}"), 3, header)

        api = self.server("API_HOST")
        self.assertIn("X-Frame-Options DENY", api)
        self.assertIn("Content-Security-Policy", api)
        self.assertIn("frame-ancestors 'none'", api)

        ops = self.server("OPS_HOST")
        self.assertIn("X-Frame-Options DENY", ops)
        self.assertNotIn("add_header Content-Security-Policy", ops)
        self.assertIn("proxy_pass_header Content-Security-Policy", ops)

    def test_csp_allows_only_the_public_turnstile_integration(self) -> None:
        display = self.server("DISPLAY_HOST")
        ops = self.server("OPS_HOST")
        self.assertIn("script-src 'self' 'unsafe-inline' https://challenges.cloudflare.com", display)
        self.assertIn("script-src-attr 'none'", display)
        self.assertIn("frame-src https://challenges.cloudflare.com", display)
        self.assertNotIn("challenges.cloudflare.com", ops)

        display_next = (ROOT / "apps" / "display-web" / "next.config.ts").read_text(encoding="utf-8")
        ops_next = (ROOT / "apps" / "ops-web" / "next.config.ts").read_text(encoding="utf-8")
        ops_proxy = (ROOT / "apps" / "ops-web" / "proxy.ts").read_text(encoding="utf-8")
        ops_policy = (ROOT / "apps" / "ops-web" / "lib" / "security-policy.ts").read_text(encoding="utf-8")
        media_policy = (ROOT / "apps/display-web/lib/media-policy.ts").read_text(encoding="utf-8")
        self.assertIn('import { mediaContentSecurityPolicy } from "./lib/media-policy"', display_next)
        self.assertIn("value: mediaContentSecurityPolicy()", display_next)
        self.assertIn('key: "Content-Security-Policy"', display_next)
        self.assertNotIn('key: "Content-Security-Policy"', ops_next)
        for source in (display_next, ops_next):
            self.assertIn('{ key: "X-Frame-Options", value: "DENY" }', source)
        for source in (media_policy, ops_policy):
            self.assertIn('"frame-ancestors \'none\'"', source)
        self.assertIn("https://challenges.cloudflare.com", media_policy)
        self.assertNotIn("https://challenges.cloudflare.com", ops_policy)
        self.assertIn("'strict-dynamic'", ops_policy)
        self.assertNotIn("script-src 'self' 'unsafe-inline'", ops_policy)
        self.assertIn('requestHeaders.set("Content-Security-Policy", policy)', ops_proxy)
        self.assertIn('response.headers.set("Content-Security-Policy", policy)', ops_proxy)

    def test_only_versioned_next_assets_get_immutable_cache(self) -> None:
        display = self.server("DISPLAY_HOST")
        self.assertIn("location ^~ /_next/static/", display)
        self.assertEqual(display.count("max-age=31536000, immutable"), 1)

    def test_metrics_and_unknown_tls_hosts_are_closed(self) -> None:
        self.assertIn("location = /metrics", self.server("API_HOST"))
        self.assertRegex(self.config, r"server_name _;\s+ssl_certificate[^}]+return 444;")

    def test_access_log_omits_client_ip_query_and_path(self) -> None:
        log_line = re.search(r"log_format self_deepsearch ([^;]+);", self.config, re.DOTALL)
        self.assertIsNotNone(log_line)
        value = log_line.group(1)
        for sensitive in ("$remote_addr", "$forwarded_client_ip", "$request_uri", "$uri", "$args"):
            self.assertNotIn(sensitive, value)

    def test_frontend_api_calls_are_same_origin(self) -> None:
        for application in ("display-web", "ops-web"):
            for path in (ROOT / "apps" / application).rglob("*"):
                if path.suffix in {".ts", ".tsx"} and ".next" not in path.parts:
                    self.assertNotIn("NEXT_PUBLIC_PLATFORM_API_URL", path.read_text(encoding="utf-8"), str(path))

    def test_proxy_implementations_and_route_boundaries_match(self) -> None:
        display_proxy = (ROOT / "apps" / "display-web" / "lib" / "platform-proxy.ts").read_text(encoding="utf-8")
        ops_proxy = (ROOT / "apps" / "ops-web" / "lib" / "platform-proxy.ts").read_text(encoding="utf-8")
        self.assertEqual(display_proxy, ops_proxy)
        self.assertIn('const maximumBodyBytes = 2 * 1024 * 1024', display_proxy)
        self.assertIn('redirect: "manual"', display_proxy)
        self.assertIn('outgoing.set("cache-control", "no-store")', display_proxy)
        self.assertFalse((ROOT / "apps" / "display-web" / "app" / "admin").exists())
        self.assertTrue((ROOT / "apps" / "ops-web" / "app" / "admin" / "v1" / "[...path]" / "route.ts").is_file())

    def test_trusted_client_ip_reaches_api_through_frontend_proxies(self) -> None:
        self.assertEqual(self.config.count("proxy_set_header CF-Connecting-IP $forwarded_client_ip;"), 3)
        for host in ("DISPLAY_HOST", "API_HOST", "OPS_HOST"):
            self.assertIn("proxy_set_header CF-Connecting-IP $forwarded_client_ip;", self.server(host))
        for relative in ("apps/display-web/lib/platform-proxy.ts", "apps/ops-web/lib/platform-proxy.ts"):
            source = (ROOT / relative).read_text(encoding="utf-8")
            self.assertIn('"cf-connecting-ip"', source)

    def test_frontend_proxy_local_errors_are_no_store(self) -> None:
        for relative in ("apps/display-web/lib/platform-proxy.ts", "apps/ops-web/lib/platform-proxy.ts"):
            source = (ROOT / relative).read_text(encoding="utf-8")
            self.assertIn("function proxyError", source)
            self.assertIn('response.headers.set("cache-control", "no-store")', source)
            self.assertIn('proxyError("API_UNAVAILABLE"', source)


if __name__ == "__main__":
    unittest.main()
