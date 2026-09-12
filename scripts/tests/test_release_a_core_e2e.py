from __future__ import annotations

import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from argparse import Namespace
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS))
SPEC = importlib.util.spec_from_file_location("release_a_core_e2e", SCRIPTS / "release_a_core_e2e.py")
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)
DATABASE_URL = "postgres://platform_api_login:test_password@127.0.0.1:5432/self_deepsearch_core_e2e_test?sslmode=disable"
ENVIRONMENT = {"CORE_E2E_DATABASE_URL": DATABASE_URL, "CONFIRM_RELEASE_A_CORE_E2E": "disposable-database"}


class ReleaseACoreE2ETests(unittest.TestCase):
    def test_confirmation_rejected_before_process_or_network_activity(self) -> None:
        with mock.patch.dict(os.environ, {}, clear=True), mock.patch.object(MODULE, "available_port") as port:
            with mock.patch.object(MODULE.subprocess, "run") as command:
                with self.assertRaisesRegex(ValueError, "confirmation"):
                    MODULE.execute(Namespace(), {})
        port.assert_not_called()
        command.assert_not_called()

    def test_only_dedicated_loopback_database_and_restricted_login_are_allowed(self) -> None:
        self.assertEqual(MODULE.validate_database(ENVIRONMENT), DATABASE_URL)
        for invalid in (
            DATABASE_URL.replace("127.0.0.1", "db.example.com"),
            DATABASE_URL.replace("platform_api_login", "platform"),
            DATABASE_URL.replace("self_deepsearch_core_e2e_test", "production"),
            DATABASE_URL + "&host=db.example.com",
            DATABASE_URL + "#ignored-fragment",
            DATABASE_URL.replace(":5432", ":99999"),
            DATABASE_URL.replace(":test_password", ""),
        ):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                MODULE.validate_database({**ENVIRONMENT, "CORE_E2E_DATABASE_URL": invalid})

    def test_child_environment_disables_external_integrations_and_excludes_secrets(self) -> None:
        with mock.patch.dict(os.environ, {
            "AWS_SECRET_ACCESS_KEY": "do-not-inherit", "SMTP_URL": "smtps://private-provider",
            "HTTP_PROXY": "http://proxy.invalid", "PATH": "system-path", "APP_ENV": "production",
        }, clear=True):
            environment = MODULE.runtime_environment(DATABASE_URL, 12345, "expected-version")
        self.assertNotIn("AWS_SECRET_ACCESS_KEY", environment)
        self.assertNotIn("HTTP_PROXY", environment)
        self.assertEqual(environment["APP_ENV"], "development")
        self.assertEqual(environment["API_ADDR"], "127.0.0.1:12345")
        self.assertEqual(environment["COLLECTION_ENABLED"], "false")
        self.assertEqual(environment["TRUST_PROXY_HEADERS"], "false")
        self.assertTrue(environment["SMTP_URL"].startswith("smtp://127.0.0.1:1025?"))
        self.assertGreaterEqual(len(environment["AUTH_HMAC_SECRET"]), 32)

    def test_readiness_rejects_terminal_or_unrelated_api_process(self) -> None:
        process, client = mock.Mock(), mock.Mock()
        process.poll.return_value = 1
        with self.assertRaisesRegex(RuntimeError, "exited"):
            MODULE.wait_for_api(process, client, "version", 1)
        client.request.assert_not_called()
        process.poll.return_value = None
        client.request.return_value = MODULE.email.HTTPResult(200, {"version": "some-other-server"})
        with self.assertRaisesRegex(RuntimeError, "unexpected API"):
            MODULE.wait_for_api(process, client, "version", 1)
        client.message_ids.assert_not_called()

    def test_readiness_requires_api_database_and_mailpit(self) -> None:
        process, client = mock.Mock(), mock.Mock()
        process.poll.return_value = None
        client.request.side_effect = [
            MODULE.email.HTTPResult(200, {"version": "expected"}), MODULE.email.HTTPResult(200, {}),
        ]
        MODULE.wait_for_api(process, client, "expected", 1)
        self.assertEqual([call.args[1] for call in client.request.call_args_list], ["/healthz", "/readyz"])
        client.message_ids.assert_called_once()

    def test_slow_shutdown_kills_and_reaps_only_owned_process(self) -> None:
        process = mock.Mock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired("owned-api", 10), 0]
        MODULE.stop_process(process)
        process.terminate.assert_called_once()
        process.kill.assert_called_once()
        self.assertEqual(process.wait.call_count, 2)

    def test_real_flow_orchestration_uses_invited_editor_and_always_stops_api(self) -> None:
        for failed in (False, True):
            with self.subTest(failed=failed), mock.patch.dict(os.environ, ENVIRONMENT, clear=True):
                process = mock.Mock()
                args = Namespace(api_binary=SCRIPTS / "release_a_core_e2e.py", owner_binary=SCRIPTS / "release_a_core_e2e.py", timeout=1)
                report = {"status": "failed", "stage": "configuration", "checks": {}}
                checks = {key: "passed" for key in ("signup", "password_reset", "account_close", "session_revocation", "invitation")}
                with (
                    mock.patch.object(MODULE, "available_port", return_value=12345),
                    mock.patch.object(MODULE.subprocess, "run") as bootstrap,
                    mock.patch.object(MODULE.subprocess, "Popen", return_value=process),
                    mock.patch.object(MODULE, "wait_for_api"),
                    mock.patch.object(MODULE, "stop_process") as stop,
                    mock.patch.object(MODULE.email, "ReleaseAClient"),
                    mock.patch.object(MODULE.catalog, "ReleaseAClient"),
                    mock.patch.object(MODULE.email, "run", return_value=checks),
                    mock.patch.object(MODULE.email, "run_invitation") as invitation,
                    mock.patch.object(MODULE.catalog, "run_catalog_flow", return_value={"status": "passed", "final_state": "hidden"}) as catalog,
                ):
                    if failed:
                        catalog.side_effect = MODULE.catalog.E2EFailure("secret response must not escape main")
                        with self.assertRaises(MODULE.catalog.E2EFailure):
                            MODULE.execute(args, report)
                        self.assertEqual(report["status"], "failed")
                    else:
                        MODULE.execute(args, report)
                        self.assertEqual(report["status"], "passed")
                    stop.assert_called_once_with(process)
                    self.assertEqual(invitation.call_args.kwargs["invited_role"], "editor")
                    self.assertNotEqual(catalog.call_args.kwargs["editor_email"], catalog.call_args.kwargs["reviewer_email"])
                    self.assertEqual(catalog.call_args.kwargs["editor_email"], invitation.call_args.kwargs["invited_email"])
                    self.assertEqual(len(bootstrap.call_args.args[0]), 1)
                    self.assertIn("BOOTSTRAP_OWNER_PASSWORD", bootstrap.call_args.kwargs["env"])

    def test_failure_report_never_includes_exception_body_or_credentials(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "evidence.json"
            stdout = io.StringIO()
            with mock.patch.object(MODULE, "execute", side_effect=RuntimeError("password=very-private-value")):
                with redirect_stdout(stdout):
                    code = MODULE.main(["--output", str(output)])
            report = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual(code, 1)
            self.assertEqual(report["status"], "failed")
            self.assertEqual(report["error_type"], "RuntimeError")
            self.assertNotIn("very-private-value", output.read_text(encoding="utf-8") + stdout.getvalue())
            self.assertIn("worker", report["not_covered"])


if __name__ == "__main__":
    unittest.main()
