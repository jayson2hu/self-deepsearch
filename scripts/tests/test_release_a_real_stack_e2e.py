from __future__ import annotations

import copy
import importlib.util
import io
import json
import os
import signal
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
SPEC = importlib.util.spec_from_file_location("release_a_real_stack_e2e", SCRIPTS / "release_a_real_stack_e2e.py")
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)
API_URL = "postgres://platform_api_login:test_password@127.0.0.1:15432/self_deepsearch_core_e2e_test"
WORKER_URL = API_URL.replace("platform_api_login", "platform_worker_login")
ENVIRONMENT = {
    "CONFIRM_RELEASE_A_REAL_STACK_E2E": "disposable-database",
    "CORE_E2E_DATABASE_URL": API_URL,
    "REAL_STACK_WORKER_DATABASE_URL": WORKER_URL,
}


class RealStackTests(unittest.TestCase):
    def test_complete_browser_evidence_is_required_and_whitelisted(self) -> None:
        details = {
            "status": "passed", "stage": "complete", "checks": dict.fromkeys(MODULE.BROWSER_CHECKS, "passed"),
            "password": "private-value", "fixture": {"email": "private-value"},
            "cache": {"ttl_seconds": 300, "observation_timeout_seconds": 60, "manual_revalidation_requests": 0,
                      "cached_surfaces": ["home", "detail", "sitemap"], "uncached_visibility_surfaces": ["search"],
                      "publication_elapsed_ms": 1200, "hidden_elapsed_ms": 900,
                      "publication_surfaces": ["home", "search", "detail", "sitemap"],
                      "hidden_surfaces": ["home", "search", "detail", "sitemap"], "token": "private-value"},
        }
        report: dict[str, object] = {"checks": {}}
        MODULE.merge_browser_report(0, details, report)
        self.assertNotIn("private-value", json.dumps(report))
        self.assertEqual(len(report["checks"]), 16)
        self.assertEqual(report["cache"]["publication_elapsed_ms"], 1200)
        for section, key, value in (
            ("checks", "owner_browser_login", "not_run"),
            ("checks", "private_value", "passed"),
            ("cache", "manual_revalidation_requests", 1),
            ("cache", "manual_revalidation_requests", False),
            ("cache", "publication_elapsed_ms", 60000),
            ("cache", "hidden_elapsed_ms", -1),
            ("cache", "hidden_surfaces", ["detail"]),
            ("cache", "cached_surfaces", ["home", "search", "detail", "sitemap"]),
        ):
            changed = copy.deepcopy(details)
            changed[section][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(RuntimeError):
                MODULE.merge_browser_report(0, changed, {"checks": {}})
        del details["checks"]["owner_browser_login"]
        with self.assertRaises(RuntimeError):
            MODULE.merge_browser_report(0, details, {"checks": {}})

    def test_failed_browser_evidence_only_retains_fixed_stage_and_failure_code(self) -> None:
        report: dict[str, object] = {"checks": {}}
        with self.assertRaises(RuntimeError):
            MODULE.merge_browser_report(1, {
                "stage": "editor_work_submit", "failure_code": "new_work_create_http_status", "error": "secret",
            }, report)
        self.assertEqual(report["browser_failure_code"], "new_work_create_http_status")
        self.assertEqual(report["browser_stage"], "editor_work_submit")
        report = {"checks": {}}
        with self.assertRaises(RuntimeError):
            MODULE.merge_browser_report(1, {"stage": "secret", "failure_code": "secret"}, report)
        self.assertNotIn("secret", json.dumps(report))

    def test_confirmation_and_database_guards_precede_all_processes(self) -> None:
        with mock.patch.dict(os.environ, {}, clear=True), mock.patch.object(MODULE.core, "available_port") as port:
            with mock.patch.object(MODULE.subprocess, "run") as run:
                with self.assertRaises(ValueError):
                    MODULE.execute(Namespace(), {})
        port.assert_not_called()
        run.assert_not_called()

    def test_database_roles_and_addresses_are_restricted(self) -> None:
        self.assertEqual(MODULE.validate_environment(ENVIRONMENT), (API_URL, WORKER_URL))
        for value in (
            WORKER_URL.replace("platform_worker_login", "platform"),
            WORKER_URL.replace("127.0.0.1", "db.example.test"),
            WORKER_URL.replace(":15432", ":15433"),
            WORKER_URL.replace(":15432", ":99999"),
            WORKER_URL.replace("self_deepsearch_core_e2e_test", "production"),
            WORKER_URL.replace(":test_password", ""),
            WORKER_URL + "?host=other", WORKER_URL + "#", WORKER_URL + "#fragment",
        ):
            with self.subTest(value=value), self.assertRaises(ValueError):
                MODULE.validate_environment({**ENVIRONMENT, "REAL_STACK_WORKER_DATABASE_URL": value})
        with self.assertRaises(ValueError):
            MODULE.validate_environment({**ENVIRONMENT, "CORE_E2E_DATABASE_URL": API_URL.replace("127.0.0.1", "remote")})

    def test_children_inherit_no_provider_or_credential_configuration(self) -> None:
        with mock.patch.dict(os.environ, {
            "PATH": "path", "HOME": "home", "PLAYWRIGHT_BROWSERS_PATH": "browser-cache",
            "AWS_SECRET_ACCESS_KEY": "private", "GITHUB_TOKEN": "private", "SMTP_URL": "private",
            "HTTP_PROXY": "private", "NODE_OPTIONS": "private", "DATABASE_URL": "private",
        }, clear=True):
            api, worker, browser = MODULE.runtime_environments(
                API_URL, WORKER_URL, (12340, 12341, 12342, 12343), "version", "a" * 16,
                "owner@example.test", "synthetic-password",
            )
        for environment in (api, worker, browser):
            for key in ("AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN", "HTTP_PROXY", "NODE_OPTIONS"):
                self.assertNotIn(key, environment)
            self.assertEqual(environment["NO_PROXY"], "*")
        self.assertEqual(api["ALLOWED_ORIGINS"], "http://127.0.0.1:12342,http://127.0.0.1:12343")
        self.assertEqual(worker["WORKER_REGION"], "japan")
        self.assertEqual(worker["COLLECTION_ENABLED"], "false")
        self.assertEqual(worker["HISTORY_ARCHIVE_ENABLED"], "false")
        self.assertEqual(worker["MEDIA_USAGE_MONITOR_MODE"], "off")
        self.assertEqual(worker["CACHE_HMAC_SECRET"], browser["CACHE_HMAC_SECRET"])
        self.assertNotEqual(worker["MEDIA_HMAC_SECRET"], worker["CACHE_HMAC_SECRET"])
        self.assertNotIn("DATABASE_URL", browser)
        self.assertNotIn("REAL_BROWSER_OWNER_PASSWORD", api)
        self.assertNotIn("REAL_BROWSER_OWNER_PASSWORD", worker)
        self.assertEqual(browser["PLAYWRIGHT_BROWSERS_PATH"], "browser-cache")

    def test_readiness_rejects_unrelated_or_exited_services(self) -> None:
        process = mock.Mock()
        process.poll.return_value = 1
        with self.assertRaisesRegex(RuntimeError, "exited"):
            MODULE.wait_service(process, "http://127.0.0.1:12340", "expected", 1)
        process.poll.return_value = None
        response = mock.MagicMock()
        response.__enter__.return_value = response
        response.status, response.read.return_value = 200, b'{"version":"unrelated"}'
        with mock.patch.object(MODULE.urllib.request, "build_opener") as opener:
            opener.return_value.open.return_value = response
            with self.assertRaisesRegex(RuntimeError, "another service"):
                MODULE.wait_service(process, "http://127.0.0.1:12340", "expected", 1)

    def test_readiness_never_follows_redirects(self) -> None:
        handler = MODULE.NoRedirect()
        self.assertIsNone(handler.redirect_request(None, None, 302, "redirect", {}, "https://other.test"))

    def test_browser_timeout_signals_only_its_owned_process_group(self) -> None:
        process = mock.Mock(pid=123456)
        process.poll.return_value = None
        with (
            mock.patch.object(MODULE.os, "name", "posix"),
            mock.patch.object(MODULE, "process_start_ticks", return_value="123"),
            mock.patch.object(MODULE.os, "killpg", create=True) as kill,
        ):
            MODULE.stop_browser(process, node_start_ticks="123", grace_seconds=0)
        self.assertEqual([call.args for call in kill.call_args_list], [(123456, signal.SIGTERM), (123456, signal.SIGKILL)])
        process.wait.assert_called_once_with(timeout=5)

    def test_browser_registry_rejects_forged_or_reused_process_identity(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            registry = Path(directory) / "browser.json"
            valid = {"node_pid": 123456, "node_start_ticks": "123", "browser_pid": 123457, "browser_start_ticks": "124"}
            for change in (
                {"node_pid": 9}, {"node_start_ticks": "122"}, {"browser_pid": os.getpid()},
                {"browser_pid": True}, {"browser_start_ticks": "125"}, {"unexpected_pid": 9},
            ):
                with self.subTest(change=change):
                    registry.write_text(json.dumps({**valid, **change}), encoding="utf-8")
                    with mock.patch.object(MODULE, "process_start_ticks", return_value="124"), mock.patch.object(MODULE.os, "getpgid", return_value=123457):
                        with self.assertRaisesRegex(RuntimeError, "registration"):
                            MODULE.registered_browser_group(registry, 123456, "123")

    @unittest.skipUnless(sys.platform == "linux", "Linux process-group and /proc identity contract")
    def test_cleanup_stops_surviving_next_and_detached_browser_after_node_exit(self) -> None:
        import ctypes

        # Reap only this test's orphaned descendants, rather than leaving zombies
        # to the container's PID 1. Restore the caller's subreaper setting below.
        libc = ctypes.CDLL(None, use_errno=True)
        previous = ctypes.c_int()
        self.assertEqual(libc.prctl(37, ctypes.byref(previous), 0, 0, 0), 0)
        self.assertEqual(libc.prctl(36, 1, 0, 0, 0), 0)
        child_code = "import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); print('ready',flush=True); time.sleep(60)"
        browser_leader_code = "\n".join([
            "import subprocess,sys",
            f"child=subprocess.Popen([sys.executable,'-c',{child_code!r}],stdout=subprocess.PIPE,text=True)",
            "child.stdout.readline()",
            "print(child.pid,flush=True)",
            "sys.stdin.readline()",
        ])
        leader_code = "\n".join([
            "import json,subprocess,sys,time",
            f"code={child_code!r}",
            f"browser_code={browser_leader_code!r} if sys.argv[1] == 'renderer' else code",
            "children=[subprocess.Popen([sys.executable,'-c',value],stdin=subprocess.PIPE,stdout=subprocess.PIPE,text=True,start_new_session=detached) for value,detached in ((code,False),(browser_code,True))]",
            "ready=[child.stdout.readline().strip() for child in children]",
            "print(json.dumps({'children':[child.pid for child in children],'renderer':int(ready[1]) if sys.argv[1] == 'renderer' else None}),flush=True)",
            "command=sys.stdin.readline().strip()",
            "if command == 'renderer':",
            " children[1].stdin.write('exit\\n'); children[1].stdin.flush(); children[1].wait(timeout=3); print('browser-exited',flush=True)",
            "time.sleep(60) if command != 'exit' else None",
        ])
        unrelated = subprocess.Popen([sys.executable, "-c", child_code], stdout=subprocess.PIPE, text=True, start_new_session=True)
        assert unrelated.stdout is not None
        unrelated.stdout.readline()
        try:
            for scenario in ("running", "exit", "renderer"):
                with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                    leader = subprocess.Popen(
                        [sys.executable, "-c", leader_code, scenario], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                        stderr=subprocess.DEVNULL, text=True, start_new_session=True,
                    )
                    children: list[int] = []
                    try:
                        assert leader.stdout is not None and leader.stdin is not None
                        details = json.loads(leader.stdout.readline())
                        children = details["children"]
                        node_ticks = MODULE.process_start_ticks(leader.pid)
                        browser_pid = children[1]
                        registry = Path(directory) / "browser.json"
                        registry.write_text(json.dumps({
                            "node_pid": leader.pid, "node_start_ticks": node_ticks,
                            "browser_pid": browser_pid, "browser_start_ticks": MODULE.process_start_ticks(browser_pid),
                        }), encoding="utf-8")
                        leader.stdin.write(scenario + "\n")
                        leader.stdin.flush()
                        if scenario == "exit":
                            self.assertEqual(leader.wait(timeout=3), 0)
                        elif scenario == "renderer":
                            self.assertEqual(leader.stdout.readline().strip(), "browser-exited")
                            self.assertIsNone(MODULE.process_start_ticks(browser_pid))
                            children[1] = details["renderer"]
                        MODULE.stop_browser(leader, registry=registry, node_start_ticks=node_ticks, grace_seconds=0.1)
                        for pid in children:
                            waited, status = os.waitpid(pid, 0)
                            self.assertEqual(waited, pid)
                            self.assertTrue(os.WIFSIGNALED(status))
                            self.assertEqual(os.WTERMSIG(status), signal.SIGKILL)
                        children.clear()
                        self.assertIsNone(unrelated.poll(), "cleanup touched an unrelated process")
                    finally:
                        for pid in [leader.pid, *children]:
                            try:
                                os.kill(pid, signal.SIGKILL)
                            except ProcessLookupError:
                                pass
                        leader.wait(timeout=3)
                        for pid in children:
                            try:
                                os.waitpid(pid, 0)
                            except ChildProcessError:
                                pass
                        if leader.stdin is not None:
                            leader.stdin.close()
                        if leader.stdout is not None:
                            leader.stdout.close()
        finally:
            unrelated.kill()
            unrelated.wait(timeout=3)
            unrelated.stdout.close()
            self.assertEqual(libc.prctl(36, previous.value, 0, 0, 0), 0)

    def test_startup_failure_stops_every_owned_service(self) -> None:
        api, worker = mock.Mock(), mock.Mock()
        script = SCRIPTS / "release_a_real_stack_e2e.py"
        args = Namespace(api_binary=script, worker_binary=script, owner_binary=script, browser_script=script, timeout=1)
        with (
            mock.patch.dict(os.environ, ENVIRONMENT, clear=True),
            mock.patch.object(MODULE.core, "available_port", side_effect=[12340, 12341, 12342, 12343]),
            mock.patch.object(MODULE.subprocess, "run"),
            mock.patch.object(MODULE.subprocess, "Popen", side_effect=[api, worker]),
            mock.patch.object(MODULE, "wait_service", side_effect=[None, RuntimeError("startup")]),
            mock.patch.object(MODULE.core, "stop_process") as stop,
        ):
            with self.assertRaisesRegex(RuntimeError, "startup"):
                MODULE.execute(args, {"checks": {}})
        self.assertEqual([call.args[0] for call in stop.call_args_list], [worker, api])

    def test_main_redacts_all_raw_error_text(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "result.json"
            stdout = io.StringIO()
            with mock.patch.object(MODULE, "execute", side_effect=RuntimeError("password=private-value")):
                with redirect_stdout(stdout):
                    result = MODULE.main(["--output", str(output)])
            evidence = output.read_text(encoding="utf-8")
            self.assertEqual(result, 1)
            self.assertNotIn("private-value", evidence + stdout.getvalue())
            self.assertEqual(json.loads(evidence)["error_type"], "RuntimeError")


if __name__ == "__main__":
    unittest.main()
