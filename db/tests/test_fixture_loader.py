from __future__ import annotations

import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
LOADER = ROOT / "infra" / "database" / "load_release_a_fixtures.sh"
SH = shutil.which("sh")
if SH is None:
    candidate = Path("C:/Program Files/Git/bin/sh.exe")
    if candidate.exists():
        SH = str(candidate)


@unittest.skipUnless(SH, "POSIX sh is required")
class FixtureLoaderTests(unittest.TestCase):
    def test_disabled_loader_refuses_before_psql(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            env, call_log = self.mock_environment(Path(temporary))
            result = self.run_loader(env)

            self.assertEqual(result.returncode, 2)
            self.assertIn("synthetic fixture loading is disabled", result.stderr)
            self.assertFalse(call_log.exists())

    def test_wrong_confirmation_refuses_before_psql(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            env, call_log = self.mock_environment(Path(temporary))
            env.update(
                {
                    "ALLOW_SYNTHETIC_FIXTURES": "true",
                    "CONFIRM_SYNTHETIC_FIXTURES": "not-confirmed",
                }
            )
            result = self.run_loader(env)

            self.assertEqual(result.returncode, 2)
            self.assertIn("must be release-a-test-only", result.stderr)
            self.assertFalse(call_log.exists())

    def test_missing_seed_refuses_before_psql(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, call_log = self.mock_environment(root)
            env.update(self.enabled_environment(root / "missing.sql"))
            result = self.run_loader(env)

            self.assertEqual(result.returncode, 2)
            self.assertIn("fixture seed file is missing", result.stderr)
            self.assertFalse(call_log.exists())

    def test_success_passes_all_psql_safety_arguments(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, call_log = self.mock_environment(root)
            seed = root / "fixture.sql"
            seed.write_text("SELECT 1;\n", encoding="utf-8", newline="\n")
            env.update(self.enabled_environment(seed))
            result = self.run_loader(env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(result.stdout, "release_a_synthetic_fixtures=loaded\n")
            arguments = call_log.read_text(encoding="utf-8").splitlines()
            self.assertEqual(arguments[0], "postgres://admin.invalid/test")
            self.assertIn("--no-psqlrc", arguments)
            self.assertIn("--single-transaction", arguments)
            self.assertIn("ON_ERROR_STOP=1", arguments)
            self.assertIn("release_a_fixture=1", arguments)
            self.assertEqual(arguments[-2], "--file")
            self.assertEqual(arguments[-1], self.shell_path(seed))

    def test_psql_failure_is_propagated_without_success_marker(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, call_log = self.mock_environment(root)
            seed = root / "fixture.sql"
            seed.write_text("SELECT 1;\n", encoding="utf-8", newline="\n")
            env.update(self.enabled_environment(seed))
            env["MOCK_PSQL_EXIT"] = "9"
            result = self.run_loader(env)

            self.assertEqual(result.returncode, 9)
            self.assertNotIn("release_a_synthetic_fixtures=loaded", result.stdout)
            self.assertTrue(call_log.exists())
            self.assertNotIn(env["ADMIN_DATABASE_URL"], result.stdout + result.stderr)

    def mock_environment(self, root: Path) -> tuple[dict[str, str], Path]:
        mock_bin = root / "bin"
        mock_bin.mkdir()
        call_log = root / "psql-arguments.log"
        self.write_executable(
            mock_bin / "psql",
            """#!/bin/sh
printf '%s\n' "$@" >"$MOCK_PSQL_CALL_LOG"
exit "${MOCK_PSQL_EXIT:-0}"
""",
        )
        env = os.environ.copy()
        env.update(
            {
                "PATH": str(mock_bin) + os.pathsep + env.get("PATH", ""),
                "ADMIN_DATABASE_URL": "postgres://admin.invalid/test",
                "MOCK_PSQL_CALL_LOG": self.shell_path(call_log),
            }
        )
        env.pop("ALLOW_SYNTHETIC_FIXTURES", None)
        env.pop("CONFIRM_SYNTHETIC_FIXTURES", None)
        env.pop("SEED_FILE", None)
        env.pop("MOCK_PSQL_EXIT", None)
        return env, call_log

    def enabled_environment(self, seed: Path) -> dict[str, str]:
        return {
            "ALLOW_SYNTHETIC_FIXTURES": "true",
            "CONFIRM_SYNTHETIC_FIXTURES": "release-a-test-only",
            "SEED_FILE": self.shell_path(seed),
        }

    def run_loader(self, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [str(SH), LOADER.as_posix()],
            cwd=ROOT,
            env=env,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
        )

    @staticmethod
    def write_executable(path: Path, content: str) -> None:
        path.write_text(content, encoding="utf-8", newline="\n")
        path.chmod(0o700)

    @staticmethod
    def shell_path(path: Path) -> str:
        resolved = path.resolve()
        try:
            return resolved.relative_to(ROOT).as_posix()
        except ValueError:
            pass
        if resolved.drive:
            return f"/{resolved.drive[0].lower()}{resolved.as_posix()[2:]}"
        return resolved.as_posix()


if __name__ == "__main__":
    unittest.main()
