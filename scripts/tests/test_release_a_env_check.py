from __future__ import annotations

import importlib.util
import io
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[1] / "release_a_env_check.py"
SPEC = importlib.util.spec_from_file_location("release_a_env_check", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


def valid_japan() -> dict[str, str]:
    def secret(letter: str) -> str:
        return letter * 40

    return {
        "APP_ENV": "production",
        "MEDIA_DELIVERY_MODE": "normal",
        "MEDIA_DELIVERY_DYNAMIC_MODE": "off",
        "MEDIA_EDGE_POLICY_MODE": "off",
        "MEDIA_EDGE_POLICY_TOKEN": "",
        "BUILD_VERSION": "2026.09.09-a1",
        "PLATFORM_API_IMAGE": "registry.test/platform-api:2026.09.09-a1",
        "PLATFORM_WORKER_IMAGE": "registry.test/platform-worker:2026.09.09-a1",
        "DISPLAY_WEB_IMAGE": "registry.test/display-web:2026.09.09-a1",
        "OPS_WEB_IMAGE": "registry.test/ops-web:2026.09.09-a1",
        "EDGE_IMAGE": "nginx:1.27.5-alpine",
        "DISPLAY_HOST": "display.test.example",
        "API_HOST": "api.test.example",
        "OPS_HOST": "ops.test.example",
        "ORIGIN_CERT_FILE": "/srv/secrets/origin.crt",
        "ORIGIN_KEY_FILE": "/srv/secrets/origin.key",
        "POSTGRES_DB": "self_deepsearch",
        "POSTGRES_USER": "platform",
        "POSTGRES_PASSWORD": secret("p"),
        "ADMIN_DATABASE_URL": f"postgres://platform:{secret('p')}@postgres:5432/self_deepsearch?sslmode=disable",
        "PLATFORM_API_DB_PASSWORD": secret("a"),
        "PLATFORM_WORKER_DB_PASSWORD": secret("w"),
        "PLATFORM_API_DATABASE_URL": (
            f"postgres://platform_api_login:{secret('a')}@postgres:5432/self_deepsearch?sslmode=disable"
        ),
        "PLATFORM_WORKER_DATABASE_URL": (
            f"postgres://platform_worker_login:{secret('w')}@postgres:5432/self_deepsearch?sslmode=disable"
        ),
        "WORKER_ID": "japan-worker-1",
        "WORKER_POLL_INTERVAL": "2s",
        "WORKER_LEASE_DURATION": "30s",
        "ALLOWED_ORIGINS": "https://display.test.example,https://ops.test.example",
        "SITE_URL": "https://display.test.example",
        "NEXT_PUBLIC_SITE_URL": "https://display.test.example",
        "RIGHTS_CONTACT_EMAIL": "rights@test.example",
        "S3_PUBLIC_BASE_URL": "https://media.test.example",
        "AUTH_HMAC_SECRET": secret("h"),
        "CACHE_HMAC_SECRET": secret("c"),
        "DISPLAY_REVALIDATE_URL": "http://display-web:3000/api/internal/revalidate",
        "MEDIA_DELETE_URL": "http://10.0.0.2:8090/v1/delete",
        "MEDIA_HMAC_SECRET": secret("m"),
        "MEDIA_RECONCILE_ENABLED": "true",
        "MEDIA_RECONCILE_URL": "http://10.0.0.2:8090/v1/reconcile",
        "MEDIA_RECONCILE_INTERVAL": "24h",
        "MEDIA_INSPECT_ENABLED": "true",
        "MEDIA_INSPECT_DISPLAY_ORIGIN": "https://display.test.example",
        "SMTP_URL": "smtp://mailpit:1025",
        "EMAIL_DAILY_LIMIT": "500",
        "EMAIL_FAILURE_THRESHOLD": "5",
        "EMAIL_CIRCUIT_COOLDOWN": "15m",
        "EMAIL_SEND_TIMEOUT": "10s",
        "TURNSTILE_BYPASS": "false",
        "TURNSTILE_SITE_KEY": "site-key-for-test",
        "TURNSTILE_SECRET_KEY": "secret-key-for-test",
        "TURNSTILE_EXPECTED_HOSTNAME": "display.test.example",
        "TRUST_PROXY_HEADERS": "true",
        "METRICS_TOKEN": secret("t"),
        "METRICS_TOKEN_FILE": "/srv/secrets/metrics-token",
        "ALERTMANAGER_CONFIG_FILE": "/srv/secrets/alertmanager.yml",
        "ALERTMANAGER_SMTP_PASSWORD_FILE": "/srv/secrets/alertmanager-smtp-password",
        "HISTORY_ARCHIVE_ENABLED": "false",
        "HISTORY_ARCHIVE_INTERVAL": "6h",
        "HISTORY_ARCHIVE_MIN_AGE": "720h",
        "HISTORY_ARCHIVE_BATCH_SIZE": "1000",
        "COLLECTION_ENABLED": "false",
        "ALLOW_SYNTHETIC_FIXTURES": "false",
        "CONFIRM_SYNTHETIC_FIXTURES": "disabled",
    }


def valid_beijing(japan: dict[str, str]) -> dict[str, str]:
    return {
        "APP_ENV": "production",
        "MEDIA_DELIVERY_MODE": japan["MEDIA_DELIVERY_MODE"],
        "BUILD_VERSION": japan["BUILD_VERSION"],
        "WORKER_ID": "beijing-worker-1",
        "WORKER_POLL_INTERVAL": "2s",
        "WORKER_LEASE_DURATION": "30s",
        "METRICS_TOKEN": "b" * 40,
        "PLATFORM_WORKER_IMAGE": japan["PLATFORM_WORKER_IMAGE"],
        "MEDIA_PYTHON_IMAGE": "registry.test/media-python:2026.09.09-a1",
        "INGEST_API_URL": "https://api.test.example/internal/v1/ingest",
        "MEDIA_HMAC_SECRET": japan["MEDIA_HMAC_SECRET"],
        "S3_ENDPOINT_URL": "https://account.r2.cloudflarestorage.com",
        "S3_REGION": "auto",
        "S3_PRIVATE_BUCKET": "private-master",
        "S3_PUBLIC_BUCKET": "public-derivatives",
        "S3_PUBLIC_BASE_URL": japan["S3_PUBLIC_BASE_URL"],
        "MEDIA_STORAGE_LIMIT_BYTES": str(10 * 1024**3),
        "MEDIA_UPLOAD_LOCK_FILE": "/var/lib/self-deepsearch/media-state/upload.lock",
        "AWS_ACCESS_KEY_ID": "access-key-id",
        "AWS_SECRET_ACCESS_KEY": "s" * 40,
        "CLOUDFLARE_ZONE_ID": "zone-id-123",
        "CLOUDFLARE_API_TOKEN": "f" * 40,
    }


def write_env(path: Path, values: dict[str, str]) -> None:
    path.write_text("\n".join(f"{key}={value}" for key, value in values.items()) + "\n", encoding="utf-8")


class ReleaseAEnvCheckTests(unittest.TestCase):
    def test_upload_admission_requires_paired_enforcement_private_origin_and_independent_secret(self) -> None:
        japan = valid_japan()
        japan.update(MEDIA_UPLOAD_QUEUE_MODE="dispatch", MEDIA_UPLOAD_CONTROL_URL="http://192.0.2.2:8090",
                     MEDIA_UPLOAD_CONTROL_SECRET="u" * 40, MEDIA_UPLOAD_CONTROL_ALLOW_HTTP="true",
                     MEDIA_UPLOAD_ADMISSION_MODE="enforce", MEDIA_UPLOAD_ADMISSION_SECRET="n" * 40,
                     MEDIA_USAGE_MONITOR_MODE="observe", R2_ANALYTICS_ACCOUNT_ID="a" * 32,
                     R2_ANALYTICS_API_TOKEN="t" * 40, MEDIA_USAGE_PERIOD_START="2026-09-01T00:00:00Z",
                     MEDIA_USAGE_PERIOD_END="2026-10-01T00:00:00Z")
        beijing = valid_beijing(japan)
        beijing.update(MEDIA_UPLOAD_CONTROL_MODE="enforce", MEDIA_UPLOAD_CONTROL_SECRET="u" * 40,
                       MEDIA_UPLOAD_ADMISSION_MODE="enforce", MEDIA_UPLOAD_ADMISSION_SECRET="n" * 40,
                       MEDIA_UPLOAD_ADMISSION_URL="http://192.0.2.1:18081", MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP="true")
        for origin in ("http://192.0.2.1:18081", "https://192.0.2.1", "https://[2001:db8::1]:443"):
            report = MODULE.Report()
            MODULE.validate_japan(japan, report, mode="full", check_files=False)
            MODULE.validate_beijing({**beijing, "MEDIA_UPLOAD_ADMISSION_URL": origin}, japan, report)
            self.assertFalse(report.errors, report.errors)
        cases = [("japan", "MEDIA_UPLOAD_ADMISSION_MODE", "off"),
                 ("japan", "MEDIA_UPLOAD_ADMISSION_MODE", "typo"),
                 ("japan", "MEDIA_USAGE_MONITOR_MODE", "off"),
                 ("japan", "MEDIA_UPLOAD_QUEUE_MODE", "off"),
                 ("japan", "MEDIA_UPLOAD_ADMISSION_SECRET", ""),
                 ("japan", "MEDIA_UPLOAD_ADMISSION_SECRET", "x" * 4097),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_MODE", "off"),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_MODE", "typo"),
                 ("beijing", "MEDIA_UPLOAD_CONTROL_MODE", "off"),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_SECRET", ""),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_SECRET", "short"),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_SECRET", "other" * 10),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "false"),
                 ("beijing", "MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP", "1")]
        cases += [(scope, "MEDIA_UPLOAD_ADMISSION_SECRET", values[key]) for scope, values in (("japan", japan), ("beijing", beijing))
                  for key in ("MEDIA_HMAC_SECRET", "MEDIA_UPLOAD_CONTROL_SECRET", "METRICS_TOKEN")]
        cases += [("beijing", "MEDIA_UPLOAD_ADMISSION_URL", origin) for origin in (
            "", "https://worker.test", "https://user:secret@192.0.2.1", "https://@192.0.2.1",
            "https://192.0.2.1/path", "https://192.0.2.1?", "https://192.0.2.1#",
            "https://0.0.0.0", "https://224.0.0.1", "https://[fe80::1%eth0]", "https://192.0.2.1:0",
            "https://192.0.2.1:65536", "ftp://192.0.2.1", " https://192.0.2.1",
        )]
        for scope, key, value in cases:
            with self.subTest(scope=scope, key=key):
                jp, bj = dict(japan), dict(beijing)
                (jp if scope == "japan" else bj)[key] = value
                report = MODULE.Report()
                MODULE.validate_japan(jp, report, mode="full", check_files=False)
                MODULE.validate_beijing(bj, jp, report)
                self.assertTrue(any("MEDIA_UPLOAD_ADMISSION" in error for error in report.errors), report.errors)
                self.assertNotIn("n" * 40, " ".join(report.errors + report.warnings))
                self.assertNotIn("user:secret", " ".join(report.errors + report.warnings))

    def test_upload_queue_requires_matching_enforcement_and_explicit_http(self) -> None:
        japan = valid_japan()
        japan.update(MEDIA_UPLOAD_QUEUE_MODE="dispatch", MEDIA_UPLOAD_CONTROL_URL="http://192.0.2.2:8090",
                     MEDIA_UPLOAD_CONTROL_SECRET="u" * 40, MEDIA_UPLOAD_CONTROL_ALLOW_HTTP="true")
        beijing = valid_beijing(japan)
        beijing.update(MEDIA_UPLOAD_CONTROL_MODE="enforce", MEDIA_UPLOAD_CONTROL_SECRET="u" * 40)
        report = MODULE.Report()
        MODULE.validate_japan(japan, report, mode="full", check_files=False)
        MODULE.validate_beijing(beijing, japan, report)
        self.assertFalse(report.errors)
        self.assertTrue(any("resume" in warning for warning in report.warnings))
        for scope, key, value in [("japan", "MEDIA_UPLOAD_QUEUE_MODE", "auto"),
                                  ("japan", "MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "false"),
                                  ("japan", "MEDIA_UPLOAD_CONTROL_ALLOW_HTTP", "1"),
                                  ("beijing", "MEDIA_UPLOAD_CONTROL_MODE", "off")]:
            with self.subTest(scope=scope, key=key, value=value):
                jp, bj = dict(japan), dict(beijing)
                (jp if scope == "japan" else bj)[key] = value
                report = MODULE.Report()
                MODULE.validate_japan(jp, report, mode="full", check_files=False)
                MODULE.validate_beijing(bj, jp, report)
                self.assertTrue(any("MEDIA_UPLOAD" in error for error in report.errors))

    def test_upload_control_enforce_requires_private_origin_and_separate_matching_secret(self) -> None:
        japan = valid_japan()
        japan.update(MEDIA_UPLOAD_CONTROL_URL="http://192.0.2.2:8090", MEDIA_UPLOAD_CONTROL_SECRET="u" * 40)
        beijing = valid_beijing(japan)
        beijing.update(MEDIA_UPLOAD_CONTROL_MODE="enforce", MEDIA_UPLOAD_CONTROL_SECRET="u" * 40)
        report = MODULE.Report()
        MODULE.validate_japan(japan, report, mode="full", check_files=False)
        MODULE.validate_beijing(beijing, japan, report)
        self.assertFalse(report.errors)
        cases = [("japan", "MEDIA_UPLOAD_CONTROL_URL", "http://192.0.2.2:8090/v1/delete"),
                 ("japan", "MEDIA_UPLOAD_CONTROL_URL", "https://user:password@media.test"),
                 ("japan", "MEDIA_UPLOAD_CONTROL_SECRET", japan["MEDIA_HMAC_SECRET"]),
                 ("beijing", "MEDIA_UPLOAD_CONTROL_SECRET", ""),
                 ("beijing", "MEDIA_UPLOAD_CONTROL_SECRET", "v" * 40),
                 ("beijing", "MEDIA_UPLOAD_CONTROL_MODE", "on")]
        for scope, key, value in cases:
            with self.subTest(scope=scope, key=key):
                jp, bj = dict(japan), dict(beijing)
                (jp if scope == "japan" else bj)[key] = value
                report = MODULE.Report()
                MODULE.validate_japan(jp, report, mode="full", check_files=False)
                MODULE.validate_beijing(bj, jp, report)
                self.assertTrue(any("MEDIA_UPLOAD_CONTROL" in error for error in report.errors))

    def test_media_delivery_mode_requires_two_region_agreement(self) -> None:
        japan = valid_japan()
        for mode in ("normal", "default_only"):
            japan["MEDIA_DELIVERY_MODE"] = mode
            beijing = valid_beijing(japan)
            report = MODULE.Report()
            MODULE.validate_japan(japan, report, mode="full", check_files=False)
            MODULE.validate_beijing(beijing, japan, report)
            self.assertFalse(report.errors)
            beijing["MEDIA_DELIVERY_MODE"] = "default_only" if mode == "normal" else "normal"
            MODULE.validate_beijing(beijing, japan, report)
            self.assertTrue(any("MEDIA_DELIVERY_MODE" in error for error in report.errors))

    def test_dynamic_media_delivery_requires_explicit_observer(self) -> None:
        japan = valid_japan()
        for mode in ("off", "enforce", "typo"):
            with self.subTest(mode=mode):
                env = dict(japan)
                env["MEDIA_DELIVERY_DYNAMIC_MODE"] = mode
                if mode == "enforce":
                    env.update(
                        MEDIA_USAGE_MONITOR_MODE="observe",
                        R2_ANALYTICS_ACCOUNT_ID="a" * 32,
                        R2_ANALYTICS_API_TOKEN="t" * 40,
                        MEDIA_USAGE_PERIOD_START="2026-09-01T00:00:00Z",
                        MEDIA_USAGE_PERIOD_END="2026-10-01T00:00:00Z",
                    )
                report = MODULE.Report()
                MODULE.validate_japan(env, report, mode="core", check_files=False)
                self.assertEqual(report.errors == [], mode in {"off", "enforce"}, report.errors)

        without_observer = valid_japan()
        without_observer["MEDIA_DELIVERY_DYNAMIC_MODE"] = "enforce"
        report = MODULE.Report()
        MODULE.validate_japan(without_observer, report, mode="core", check_files=False)
        self.assertTrue(any("MEDIA_DELIVERY_DYNAMIC_MODE" in error for error in report.errors))

    def test_dynamic_delivery_is_private_and_only_injected_into_api_and_display(self) -> None:
        repository = SCRIPT.parents[1]
        root_compose = (repository / "compose.yaml").read_text(encoding="utf-8")
        japan_compose = (repository / "infra/compose/japan/compose.yaml").read_text(encoding="utf-8")
        beijing_compose = (repository / "infra/compose/beijing/compose.yaml").read_text(encoding="utf-8")
        nginx = (repository / "infra/reverse-proxy/nginx.conf.template").read_text(encoding="utf-8")
        self.assertEqual(root_compose.count("\n      MEDIA_DELIVERY_DYNAMIC_MODE:"), 2)
        self.assertEqual(japan_compose.count("\n      MEDIA_DELIVERY_DYNAMIC_MODE:"), 2)
        self.assertNotIn("MEDIA_DELIVERY_DYNAMIC_MODE", beijing_compose)
        api_server = nginx.split("server_name ${API_HOST};", 1)[1].split("server_name ${OPS_HOST};", 1)[0]
        self.assertIn("location ^~ /internal/", api_server)
        self.assertIn("return 404;", api_server)

    def test_media_edge_policy_is_optional_independent_and_api_only(self) -> None:
        japan = valid_japan()
        report = MODULE.Report()
        MODULE.validate_japan(japan, report, mode="core", check_files=False)
        self.assertEqual(report.errors, [])

        for token in ("", "short", japan["AUTH_HMAC_SECRET"], japan["CACHE_HMAC_SECRET"], japan["MEDIA_HMAC_SECRET"], japan["METRICS_TOKEN"]):
            with self.subTest(token=token[:5]):
                configured = dict(japan)
                configured["MEDIA_EDGE_POLICY_MODE"] = "enforce"
                configured["MEDIA_EDGE_POLICY_TOKEN"] = token
                result = MODULE.Report()
                MODULE.validate_japan(configured, result, mode="core", check_files=False)
                self.assertTrue(any("MEDIA_EDGE_POLICY_TOKEN" in error for error in result.errors))
                if token:
                    self.assertNotIn(token, " ".join(result.errors))

        configured = dict(japan)
        configured["MEDIA_EDGE_POLICY_MODE"] = "enforce"
        configured["MEDIA_EDGE_POLICY_TOKEN"] = "edge-policy-secret-" + "e" * 40
        result = MODULE.Report()
        MODULE.validate_japan(configured, result, mode="core", check_files=False)
        self.assertEqual(result.errors, [])

        configured["S3_PUBLIC_BASE_URL"] = "https://public-bucket.example.r2.dev"
        result = MODULE.Report()
        MODULE.validate_japan(configured, result, mode="core", check_files=False)
        self.assertTrue(any("controlled media gateway origin" in error for error in result.errors))

        repository = SCRIPT.parents[1]
        root_compose = (repository / "compose.yaml").read_text(encoding="utf-8")
        japan_compose = (repository / "infra/compose/japan/compose.yaml").read_text(encoding="utf-8")
        beijing_compose = (repository / "infra/compose/beijing/compose.yaml").read_text(encoding="utf-8")
        for source in (root_compose, japan_compose):
            self.assertEqual(source.count("\n      MEDIA_EDGE_POLICY_MODE:"), 1)
            self.assertEqual(source.count("\n      MEDIA_EDGE_POLICY_TOKEN:"), 1)
        self.assertNotIn("MEDIA_EDGE_POLICY_TOKEN", beijing_compose)

    def test_media_upload_lock_must_use_the_shared_deployment_volume(self) -> None:
        japan = valid_japan()
        for value in ("", "upload.lock", "/tmp/upload.lock", "/var/lib/self-deepsearch/media-state/other.lock"):
            with self.subTest(value=value):
                beijing = valid_beijing(japan)
                beijing["MEDIA_UPLOAD_LOCK_FILE"] = value
                report = MODULE.Report()
                MODULE.validate_beijing(beijing, japan, report)
                self.assertTrue(any("MEDIA_UPLOAD_LOCK_FILE" in error for error in report.errors))

    def test_daily_image_inspection_requires_enabled_and_public_origin_in_full_mode(self) -> None:
        for key, value, mode in (
            ("MEDIA_INSPECT_ENABLED", "false", "core"),
            ("MEDIA_INSPECT_DISPLAY_ORIGIN", "https://display.test.example/arbitrary", "core"),
            ("MEDIA_INSPECT_DISPLAY_ORIGIN", "http://display-web:3000", "full"),
        ):
            with self.subTest(key=key, mode=mode):
                env = valid_japan()
                env[key] = value
                report = MODULE.Report()
                MODULE.validate_japan(env, report, mode=mode, check_files=False)
                self.assertTrue(report.errors)

    def test_turnstile_expected_hostname_must_be_plain_and_match_public_site(self) -> None:
        for value in (
            "",
            "display.test.example:443",
            "https://display.test.example",
            "display.test.example/path",
            "*.test.example",
            "display_.test.example",
            "192.0.2.10",
            "other.test.example",
        ):
            with self.subTest(value=value):
                env = valid_japan()
                env["TURNSTILE_EXPECTED_HOSTNAME"] = value
                report = MODULE.Report()
                MODULE.validate_japan(env, report, mode="core", check_files=False)
                self.assertTrue(
                    any("TURNSTILE_EXPECTED_HOSTNAME" in error for error in report.errors), report.errors
                )

    def test_valid_core_and_full_environments_pass(self) -> None:
        japan = valid_japan()
        report = MODULE.Report()
        MODULE.validate_japan(japan, report, mode="core", check_files=False)
        self.assertEqual(report.errors, [])

        japan["ALLOW_SYNTHETIC_FIXTURES"] = "true"
        japan["CONFIRM_SYNTHETIC_FIXTURES"] = "release-a-test-only"
        MODULE.validate_japan(japan, report, mode="core", check_files=False)
        self.assertEqual(report.errors, [])

        japan["BOOTSTRAP_OWNER_EMAIL"] = "owner@test.example"
        japan["BOOTSTRAP_OWNER_PASSWORD"] = "one-time-owner-password"
        MODULE.validate_japan(japan, report, mode="core", check_files=False)
        self.assertEqual(report.errors, [])

        MODULE.validate_japan(japan, report, mode="full", check_files=False)
        MODULE.validate_beijing(valid_beijing(japan), japan, report)
        self.assertEqual(report.errors, [])

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            japan["ORIGIN_CERT_FILE"] = str(root / "origin.crt")
            japan["ORIGIN_KEY_FILE"] = str(root / "origin.key")
            japan["METRICS_TOKEN_FILE"] = str(root / "metrics-token")
            japan["ALERTMANAGER_CONFIG_FILE"] = str(root / "alertmanager.yml")
            japan["ALERTMANAGER_SMTP_PASSWORD_FILE"] = str(root / "alertmanager-smtp-password")
            Path(japan["ORIGIN_CERT_FILE"]).write_text("certificate", encoding="utf-8")
            Path(japan["ORIGIN_KEY_FILE"]).write_text("private-key", encoding="utf-8")
            Path(japan["METRICS_TOKEN_FILE"]).write_text(japan["METRICS_TOKEN"], encoding="utf-8")
            Path(japan["ALERTMANAGER_CONFIG_FILE"]).write_text(
                """global:
  smtp_auth_password_file: /run/secrets/alertmanager_smtp_password
route:
  receiver: warning
  routes:
    - matchers: ['severity="critical"']
    - matchers: ['severity=~"warning|info"']
receivers:
  - name: warning
    email_configs:
      - to: operations@test.example
        send_resolved: true
""",
                encoding="utf-8",
            )
            Path(japan["ALERTMANAGER_SMTP_PASSWORD_FILE"]).write_text("smtp-password-value", encoding="utf-8")
            file_report = MODULE.Report()
            MODULE.validate_japan(japan, file_report, mode="core", check_files=True)
        self.assertEqual(file_report.errors, [])

    def test_alertmanager_files_reject_placeholders_inline_passwords_and_short_secrets(self) -> None:
        japan = valid_japan()
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for key, name in (
                ("ORIGIN_CERT_FILE", "origin.crt"),
                ("ORIGIN_KEY_FILE", "origin.key"),
                ("METRICS_TOKEN_FILE", "metrics-token"),
                ("ALERTMANAGER_CONFIG_FILE", "alertmanager.yml"),
                ("ALERTMANAGER_SMTP_PASSWORD_FILE", "alertmanager-password"),
            ):
                japan[key] = str(root / name)
            Path(japan["ORIGIN_CERT_FILE"]).write_text("certificate", encoding="utf-8")
            Path(japan["ORIGIN_KEY_FILE"]).write_text("private-key", encoding="utf-8")
            Path(japan["METRICS_TOKEN_FILE"]).write_text(japan["METRICS_TOKEN"], encoding="utf-8")
            Path(japan["ALERTMANAGER_CONFIG_FILE"]).write_text(
                "smtp_smarthost: smtp.example.invalid:587\nsmtp_auth_password: leaked-inline-secret\n",
                encoding="utf-8",
            )
            Path(japan["ALERTMANAGER_SMTP_PASSWORD_FILE"]).write_text("short", encoding="utf-8")
            report = MODULE.Report()
            MODULE.validate_japan(japan, report, mode="core", check_files=True)
        rendered = "\n".join(report.errors)
        self.assertIn("still contains documented placeholder values", rendered)
        self.assertIn("must use smtp_auth_password_file", rendered)
        self.assertIn("12 to 4096 byte secret", rendered)
        self.assertNotIn("leaked-inline-secret", rendered)

    def test_placeholders_and_reused_secrets_fail_without_leaking_values(self) -> None:
        japan = valid_japan()
        leaked = "do-not-print-this-secret-value-1234567890"
        japan["AUTH_HMAC_SECRET"] = leaked
        japan["CACHE_HMAC_SECRET"] = leaked
        japan["SMTP_URL"] = "smtps://replace-with-provider-credentials"
        japan["PLATFORM_API_DATABASE_URL"] = (
            "postgres://platform_api_login:wrong-password@postgres:5432/other_database?sslmode=disable"
        )
        japan["MEDIA_DELETE_URL"] = "http://10.0.0.2:not-a-port/v1/delete"
        japan["BOOTSTRAP_OWNER_EMAIL"] = "not-an-email"
        japan["BOOTSTRAP_OWNER_PASSWORD"] = "short"
        japan["ALLOW_SYNTHETIC_FIXTURES"] = "true"
        japan["CONFIRM_SYNTHETIC_FIXTURES"] = "not-confirmed"
        report = MODULE.Report()
        MODULE.validate_japan(japan, report, mode="core", check_files=False)
        rendered = "\n".join(report.errors)
        self.assertIn("values must be different", rendered)
        self.assertIn("SMTP_URL", rendered)
        self.assertIn("password must match PLATFORM_API_DB_PASSWORD", rendered)
        self.assertIn("database name must match POSTGRES_DB", rendered)
        self.assertIn("valid host and port", rendered)
        self.assertIn("BOOTSTRAP_OWNER_EMAIL", rendered)
        self.assertIn("BOOTSTRAP_OWNER_PASSWORD", rendered)
        self.assertIn("CONFIRM_SYNTHETIC_FIXTURES", rendered)
        self.assertNotIn(leaked, rendered)

        repository = SCRIPT.parents[1]
        japan_example, japan_parse_errors = MODULE.parse_env_file(repository / "infra/compose/japan/.env.example")
        beijing_example, beijing_parse_errors = MODULE.parse_env_file(
            repository / "infra/compose/beijing/.env.example"
        )
        self.assertEqual(japan_parse_errors + beijing_parse_errors, [])
        example_report = MODULE.Report()
        MODULE.validate_japan(japan_example, example_report, mode="full", check_files=False)
        MODULE.validate_beijing(beijing_example, japan_example, example_report)
        for scope, values in (("japan", japan_example), ("beijing", beijing_example)):
            for key, value in values.items():
                if key in {"MEDIA_UPLOAD_ADMISSION_URL", "MEDIA_UPLOAD_ADMISSION_SECRET"} and values.get("MEDIA_UPLOAD_ADMISSION_MODE", "off") == "off":
                    continue
                if key in {"MEDIA_UPLOAD_CONTROL_URL", "MEDIA_UPLOAD_CONTROL_SECRET"} and beijing_example.get("MEDIA_UPLOAD_CONTROL_MODE", "off") == "off":
                    # Optional execution control stays disabled in templates;
                    # enforce-mode tests require matching, independent secrets.
                    continue
                if scope == "japan" and key == "MEDIA_EDGE_POLICY_TOKEN" and values.get("MEDIA_EDGE_POLICY_MODE", "off") == "off":
                    # The edge gateway is an optional Release A deployment
                    # stage; enforce-mode validation above requires a real,
                    # independent token.
                    continue
                if scope == "japan" and values.get("MEDIA_USAGE_MONITOR_MODE") == "off" and key in {
                    "MEDIA_USAGE_PERIOD_START", "MEDIA_USAGE_PERIOD_END", "R2_ANALYTICS_ACCOUNT_ID", "R2_ANALYTICS_API_TOKEN",
                }:
                    # Observer-only settings are intentionally empty while off;
                    # the enabled-mode test below requires each of them.
                    continue
                if MODULE.is_placeholder(value):
                    self.assertTrue(
                        any(error.startswith(f"{scope}:{key}:") for error in example_report.errors),
                        f"placeholder {scope}:{key} is not covered by the environment validator",
                    )

    def test_full_environment_rejects_cross_region_drift_and_bucket_aliasing(self) -> None:
        japan = valid_japan()
        beijing = valid_beijing(japan)
        beijing["BUILD_VERSION"] = "different-release"
        beijing["MEDIA_PYTHON_IMAGE"] = "registry.test/media-python:stale-release"
        beijing["MEDIA_HMAC_SECRET"] = "z" * 40
        beijing["S3_PUBLIC_BUCKET"] = beijing["S3_PRIVATE_BUCKET"]
        beijing["MEDIA_STORAGE_LIMIT_BYTES"] = str(10 * 1024**3 + 1)
        report = MODULE.Report()
        MODULE.validate_beijing(beijing, japan, report)
        rendered = "\n".join(report.errors)
        self.assertIn("must match the Japan release version", rendered)
        self.assertIn("tag must match BUILD_VERSION", rendered)
        self.assertIn("must match the Japan MEDIA_HMAC_SECRET", rendered)
        self.assertIn("private and public buckets must be different", rendered)
        self.assertIn("must be between 1 and 10737418240", rendered)

    def test_parser_rejects_duplicate_or_malformed_entries(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / ".env"
            path.write_text("VALID=one\nVALID=two\nnot-an-assignment\n", encoding="utf-8")
            values, errors = MODULE.parse_env_file(path)
        self.assertEqual(values, {"VALID": "one"})
        self.assertEqual(len(errors), 2)
        self.assertTrue(any("duplicate key VALID" in error for error in errors))
        self.assertTrue(any("expected KEY=VALUE" in error for error in errors))

    def test_cli_requires_beijing_env_for_full_mode(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            japan_path = Path(directory) / "japan.env"
            write_env(japan_path, valid_japan())
            stdout = io.StringIO()
            stderr = io.StringIO()
            with redirect_stdout(stdout), redirect_stderr(stderr):
                result = MODULE.main(["--japan-env", str(japan_path), "--mode", "full"])
        self.assertEqual(result, 2)
        self.assertIn("--beijing-env", stderr.getvalue())
        self.assertNotIn("AUTH_HMAC_SECRET=", stdout.getvalue() + stderr.getvalue())


class MediaUsageEnvironmentTests(unittest.TestCase):
    def test_reconciliation_guard_is_explicit_and_requires_observe_and_reconciliation(self) -> None:
        base = {
            "MEDIA_USAGE_MONITOR_MODE": "observe", "MEDIA_RECONCILE_ENABLED": "true",
            "MEDIA_RECONCILE_USAGE_GUARD": "enforce", "R2_ANALYTICS_ACCOUNT_ID": "a" * 32,
            "R2_ANALYTICS_API_TOKEN": "t" * 40, "MEDIA_USAGE_PERIOD_START": "2026-09-01T00:00:00Z",
            "MEDIA_USAGE_PERIOD_END": "2026-10-01T00:00:00Z",
        }
        report = MODULE.Report()
        MODULE.validate_usage_monitor(base, report)
        self.assertEqual([], report.errors)
        for key, value in (("MEDIA_USAGE_MONITOR_MODE", "off"), ("MEDIA_RECONCILE_ENABLED", "false"), ("MEDIA_RECONCILE_USAGE_GUARD", "private-invalid-value")):
            with self.subTest(key=key):
                report = MODULE.Report()
                MODULE.validate_usage_monitor({**base, key: value}, report)
                self.assertTrue(any(item.startswith("japan:MEDIA_RECONCILE_USAGE_GUARD:") for item in report.errors))
                self.assertNotIn("private-invalid-value", " ".join(report.errors))

    def test_enabled_observer_requires_all_optional_settings(self) -> None:
        report = MODULE.Report()
        MODULE.validate_usage_monitor({"MEDIA_USAGE_MONITOR_MODE": "observe"}, report)
        for key in ("MEDIA_USAGE_PERIOD_START", "MEDIA_USAGE_PERIOD_END", "R2_ANALYTICS_ACCOUNT_ID", "R2_ANALYTICS_API_TOKEN"):
            self.assertTrue(any(error.startswith(f"japan:{key}:") for error in report.errors))

    def test_observer_is_optional_and_period_is_explicit(self) -> None:
        report = MODULE.Report()
        MODULE.validate_usage_monitor({}, report)
        self.assertEqual([], report.errors)
        values = {
            "MEDIA_USAGE_MONITOR_MODE": "observe",
            "R2_ANALYTICS_ACCOUNT_ID": "a" * 32,
            "R2_ANALYTICS_API_TOKEN": "t" * 40,
            "MEDIA_USAGE_PERIOD_START": "2026-09-01T00:00:00Z",
            "MEDIA_USAGE_PERIOD_END": "2026-10-01T00:00:00Z",
            "MEDIA_USAGE_INTERVAL": "15m",
        }
        MODULE.validate_usage_monitor(values, report)
        self.assertEqual([], report.errors)
        for key, value in (
            ("MEDIA_USAGE_MONITOR_MODE", "enforce"),
            ("R2_ANALYTICS_ACCOUNT_ID", ""),
            ("R2_ANALYTICS_API_TOKEN", "private-value-do-not-echo?"),
            ("MEDIA_USAGE_PERIOD_START", ""),
            ("MEDIA_USAGE_PERIOD_END", "2026-11-01T00:00:00Z"),
            ("MEDIA_USAGE_PERIOD_END", "2026-08-01T00:00:00Z"),
            ("MEDIA_USAGE_PERIOD_END", "2026-10-01"),
            ("MEDIA_USAGE_PERIOD_END", "2026-10-01T00:00:00.1Z"),
            ("MEDIA_USAGE_INTERVAL", "1m"),
            ("MEDIA_USAGE_INTERVAL", "16m"),
            ("MEDIA_USAGE_INTERVAL", "garbage"),
        ):
            with self.subTest(key=key, value=value):
                report = MODULE.Report()
                MODULE.validate_usage_monitor({**values, key: value}, report)
                self.assertTrue(report.errors)
                self.assertNotIn("private-value-do-not-echo", " ".join(report.errors))


if __name__ == "__main__":
    unittest.main()
