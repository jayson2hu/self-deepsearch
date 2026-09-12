from __future__ import annotations

import hashlib
import os
import shutil
import subprocess
import tempfile
import time
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
BACKUP_DIR = ROOT / "infra" / "backup"
SH = shutil.which("sh")
if SH is None:
    candidate = Path("C:/Program Files/Git/bin/sh.exe")
    if candidate.exists():
        SH = str(candidate)


@unittest.skipUnless(SH, "POSIX sh is required")
class BackupWorkflowTests(unittest.TestCase):
    def test_weekly_backup_records_completed_audit(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("status=completed", result.stdout)
            self.assertIn("backup_run_id=91000000-0000-4000-8000-000000000001", result.stdout)
            sql = sql_log.read_text(encoding="utf-8")
            self.assertIn("INSERT INTO audit.backup_runs", sql)
            self.assertIn("backup_status = 'completed'", sql)
            self.assertNotIn("backup_status = 'failed'", sql)
            self.assertEqual(len(list((root / "backups").glob("*.dump"))), 1)
            self.assertEqual(len(list((root / "backups").glob("*.dump.sha256"))), 1)
            self.assertTrue((root / "backups" / "self-deepsearch-20260910T120000Z.dump").is_file())
            self.assertFalse((root / "backups" / ".weekly-backup.lock").exists())

    def test_weekly_backup_records_failed_audit(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=False)
            backup_dir = root / "backups"
            backup_dir.mkdir()
            preserved = backup_dir / "self-deepsearch-20000101T000000Z.dump"
            preserved.write_bytes(b"last known good backup\n")
            preserved_checksum = preserved.with_suffix(".dump.sha256")
            preserved_checksum.write_text("last known good checksum\n", encoding="utf-8")
            result = self.run_script("weekly_backup.sh", env)

            self.assertNotEqual(result.returncode, 0)
            sql = sql_log.read_text(encoding="utf-8")
            self.assertIn("INSERT INTO audit.backup_runs", sql)
            self.assertIn("backup_status = 'failed'", sql)
            self.assertFalse(list((root / "backups").glob("*.partial")))
            self.assertTrue(preserved.exists())
            self.assertTrue(preserved_checksum.exists())
            self.assertFalse((backup_dir / ".weekly-backup.lock").exists())

    def test_weekly_backup_keeps_only_the_newest_configured_backup_pairs(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            backup_dir = root / "backups"
            backup_dir.mkdir()
            for year in range(2000, 2005):
                self.add_backup(root, year, verified=True)
            unrelated = backup_dir / "operator-notes.txt"
            unrelated.write_text("keep me\n", encoding="utf-8")
            env["RETENTION_COUNT"] = "4"

            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("retention_count=4", result.stdout)
            self.assertIn("retention_status=complete", result.stdout)
            self.assertIn("retention_removed_count=2", result.stdout)
            backups = sorted(backup_dir.glob("self-deepsearch-*.dump"))
            checksums = sorted(backup_dir.glob("self-deepsearch-*.dump.sha256"))
            self.assertEqual(len(backups), 4)
            self.assertEqual(len(checksums), 4)
            self.assertFalse((backup_dir / "self-deepsearch-20000101T000000Z.dump").exists())
            self.assertFalse((backup_dir / "self-deepsearch-20010101T000000Z.dump").exists())
            self.assertTrue(unrelated.exists())

    def test_weekly_backup_rejects_invalid_retention_count_before_writing(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            for value in ("0", "", "-1", "1.5", "0004", "101", "9" * 40):
                with self.subTest(value=value):
                    env["RETENTION_COUNT"] = value
                    result = self.run_script("weekly_backup.sh", env)
                    self.assertEqual(result.returncode, 2)
                    self.assertIn("RETENTION_COUNT must be an integer between 1 and 100", result.stderr)
                    self.assertFalse(sql_log.exists())
                    self.assertFalse((root / "backups").exists())

    def test_retention_preserves_last_verified_recovery_point_and_unverified_files(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            files = [self.add_backup(root, year, verified=year <= 2001) for year in range(2000, 2006)]

            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse(files[0].exists())
            for backup in files[1:]:
                self.assertTrue(backup.exists(), backup.name)
                self.assertTrue(backup.with_suffix(".dump.sha256").exists())
            self.assertIn("retention_status=deferred", result.stdout)
            self.assertIn("retention_extra_count=2", result.stdout)

    def test_retention_preserves_corrupt_or_incomplete_pairs(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            files = [self.add_backup(root, year, verified=True) for year in range(2000, 2005)]
            files[0].write_bytes(b"changed after verification\n")
            files[1].with_suffix(".dump.sha256").unlink()

            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(all(backup.exists() for backup in files))
            self.assertIn("retention_status=deferred", result.stdout)
            self.assertIn("retention_removed_count=0", result.stdout)

    def test_retention_requires_hash_match_with_audit_evidence(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            files = [self.add_backup(root, year, verified=True) for year in range(2000, 2005)]
            changed = b"new content and matching sidecar, but old audit hash\n"
            files[0].write_bytes(changed)
            files[0].with_suffix(".dump.sha256").write_text(
                f"{hashlib.sha256(changed).hexdigest()}  {files[0].name}\n", encoding="utf-8"
            )

            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(files[0].exists())
            self.assertFalse(files[1].exists())
            self.assertIn("retention_extra_count=1", result.stdout)

    def test_retention_lookup_failure_does_not_execute_partial_deletion_plan(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            files = [self.add_backup(root, year, verified=True) for year in range(2000, 2005)]
            env["MOCK_RETENTION_FAIL_NAME"] = files[0].name

            result = self.run_script("weekly_backup.sh", env)

            self.assertNotEqual(result.returncode, 0)
            self.assertTrue(all(backup.exists() for backup in files))
            self.assertNotIn("backup_status = 'failed'", sql_log.read_text(encoding="utf-8"))
            self.assertFalse((root / "backups" / ".weekly-backup.lock").exists())

    def test_clock_rollback_keeps_new_dump_and_counts_protected_extra(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            future = self.add_backup(root, 2030, verified=True)
            env["RETENTION_COUNT"] = "1"

            result = self.run_script("weekly_backup.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertTrue(future.exists())
            self.assertTrue((root / "backups" / "self-deepsearch-20260910T120000Z.dump").exists())
            self.assertIn("retention_status=deferred", result.stdout)
            self.assertIn("retention_extra_count=1", result.stdout)
            self.assertIn("retention_removed_count=0", result.stdout)

    def test_completed_audit_failure_never_rotates_old_backups(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            files = [self.add_backup(root, year, verified=True) for year in range(2000, 2005)]
            env["MOCK_COMPLETE_FAIL"] = "1"

            result = self.run_script("weekly_backup.sh", env)

            self.assertNotEqual(result.returncode, 0)
            self.assertTrue(all(backup.exists() for backup in files))
            self.assertIn("backup_status = 'failed'", sql_log.read_text(encoding="utf-8"))

    def test_same_second_retry_does_not_overwrite_backup(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            first = self.run_script("weekly_backup.sh", env)
            self.assertEqual(first.returncode, 0, first.stderr)
            backup = next((root / "backups").glob("*.dump"))
            before = {path.name: path.read_bytes() for path in (backup, backup.with_suffix(".dump.sha256"))}
            audit_before = sql_log.read_bytes()

            retry = self.run_script("weekly_backup.sh", env)

            self.assertEqual(retry.returncode, 73, retry.stderr)
            self.assertIn("no files were overwritten", retry.stderr)
            self.assertEqual(sql_log.read_bytes(), audit_before)
            for name, data in before.items():
                self.assertEqual((root / "backups" / name).read_bytes(), data)
            self.assertFalse((root / "backups" / ".weekly-backup.lock").exists())

    def test_concurrent_backup_is_rejected_while_first_dump_is_running(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, _ = self.mock_environment(root, dump_succeeds=True)
            started, release = root / "dump-started", root / "dump-release"
            env["MOCK_DUMP_STARTED"] = self.shell_path(started)
            env["MOCK_DUMP_RELEASE"] = self.shell_path(release)
            first = subprocess.Popen(
                self.script_command("weekly_backup.sh"), cwd=ROOT, env=env,
                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, encoding="utf-8", errors="replace",
            )
            try:
                deadline = time.monotonic() + 10
                while not started.exists() and first.poll() is None and time.monotonic() < deadline:
                    time.sleep(0.02)
                self.assertTrue(started.exists(), "first backup never reached pg_dump")
                second = self.run_script("weekly_backup.sh", env)
                self.assertEqual(second.returncode, 75, second.stderr)
                self.assertTrue((root / "backups" / ".weekly-backup.lock").is_dir())
            finally:
                release.write_text("continue\n", encoding="utf-8")
                stdout, stderr = first.communicate(timeout=20)
            self.assertEqual(first.returncode, 0, stderr)
            self.assertIn("status=completed", stdout)
            self.assertEqual(len(list((root / "backups").glob("*.dump"))), 1)
            self.assertFalse((root / "backups" / ".weekly-backup.lock").exists())

    def test_beijing_copy_verification_uses_source_hash(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            backup = Path(temporary) / "copied.dump"
            data = b"verified backup fixture\n"
            backup.write_bytes(data)
            env = os.environ.copy()
            env.update({
                "BACKUP_FILE": self.shell_path(backup),
                "SOURCE_SHA256": hashlib.sha256(data).hexdigest(),
                "DESTINATION_REFERENCE": "beijing:/srv/backups/copied.dump",
            })
            result = self.run_script("verify_copy.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(f"observed_bytes={len(data)}", result.stdout)
            self.assertIn(f"observed_sha256={hashlib.sha256(data).hexdigest()}", result.stdout)

    def test_restore_verification_records_verified_audit(self) -> None:
        with tempfile.TemporaryDirectory(dir=ROOT) as temporary:
            root = Path(temporary)
            env, sql_log = self.mock_environment(root, dump_succeeds=True)
            backup = root / "restore.dump"
            data = b"restore fixture\n"
            digest = hashlib.sha256(data).hexdigest()
            backup.write_bytes(data)
            backup.with_suffix(".dump.sha256").write_text(
                f"{digest}  {backup.name}\n", encoding="utf-8", newline="\n"
            )
            env.update({
                "BACKUP_FILE": self.shell_path(backup),
                "BACKUP_RUN_ID": "91000000-0000-4000-8000-000000000001",
                "VERIFY_DATABASE_URL": "postgres://restore.invalid/test",
            })
            result = self.run_script("verify_restore.sh", env)

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("schema_version=21", result.stdout)
            self.assertIn("status=verified", result.stdout)
            sql = sql_log.read_text(encoding="utf-8")
            self.assertIn("backup_status = 'verified'", sql)
            self.assertIn("copied_at IS NOT NULL", sql)

    def mock_environment(self, root: Path, dump_succeeds: bool) -> tuple[dict[str, str], Path]:
        mock_bin = root / "bin"
        mock_bin.mkdir()
        sql_log = root / "sql.log"
        self.write_executable(mock_bin / "psql", """#!/bin/sh
quiet=0
reference=''
observed_hash=''
observed_bytes=''
for argument in "$@"; do
  case "$argument" in
    file_reference=*) reference="${argument#file_reference=}" ;;
    observed_sha256=*) observed_hash="${argument#observed_sha256=}" ;;
    observed_bytes=*) observed_bytes="${argument#observed_bytes=}" ;;
  esac
done
case " $* " in *" --quiet "*) quiet=1;; esac
case "$*" in
  *"count(*) FROM pg_class"*) printf '%s\n' '0'; exit 0 ;;
  *"SELECT schema_version"*) printf '%s\n' '21'; exit 0 ;;
  *"SELECT string_agg"*) printf '%s\n' 'audit,collector,platform'; exit 0 ;;
esac
input="$(cat)"
printf '%s\n---\n' "$input" >>"$MOCK_SQL_LOG"
case "$input" in
  *"SELECT CASE WHEN EXISTS"*)
    [ "${reference##*/}" != "${MOCK_RETENTION_FAIL_NAME:-}" ] || exit 8
    if [ -f "$MOCK_VERIFIED_BACKUPS" ]; then
      while IFS="$(printf '\\t')" read -r name stored_hash stored_bytes; do
        if [ "${reference##*/}" = "$name" ] && [ "$observed_hash" = "$stored_hash" ] &&
          [ "$observed_bytes" = "$stored_bytes" ]; then
          printf '%s\\n' verified
          exit 0
        fi
      done <"$MOCK_VERIFIED_BACKUPS"
    fi
    printf '%s\\n' unverified
    ;;
  *"INSERT INTO audit.backup_runs"*)
    printf '%s\n' '91000000-0000-4000-8000-000000000001'
    [ "$quiet" -eq 1 ] || printf '%s\n' 'INSERT 0 1'
    ;;
  *"RETURNING 'ok'"*)
    case "$input" in *"backup_status = 'completed', file_reference"*)
      [ "${MOCK_COMPLETE_FAIL:-0}" != "1" ] || exit 8 ;;
    esac
    printf '%s\n' 'ok'
    [ "$quiet" -eq 1 ] || printf '%s\n' 'UPDATE 1'
    ;;
esac
""")
        dump_body = "printf 'backup fixture\\n' >\"$target\"" if dump_succeeds else "exit 9"
        self.write_executable(mock_bin / "pg_dump", f"""#!/bin/sh
target=''
for argument in "$@"; do
  case "$argument" in --file=*) target="${{argument#--file=}}";; esac
done
[ -n "$target" ] || exit 2
if [ -n "${{MOCK_DUMP_STARTED:-}}" ]; then
  printf 'started\\n' >"$MOCK_DUMP_STARTED"
  tries=0
  while [ ! -f "$MOCK_DUMP_RELEASE" ]; do
    tries=$((tries + 1))
    [ "$tries" -lt 1000 ] || exit 7
    sleep 0.02
  done
fi
{dump_body}
""")
        self.write_executable(mock_bin / "pg_restore", "#!/bin/sh\nexit 0\n")
        self.write_executable(mock_bin / "date", "#!/bin/sh\nprintf '%s\\n' '20260910T120000Z'\n")
        env = os.environ.copy()
        env.update({
            "PATH": str(mock_bin) + os.pathsep + env.get("PATH", ""),
            "MOCK_BIN": self.shell_path(mock_bin),
            "MOCK_SQL_LOG": self.shell_path(sql_log),
            "MOCK_VERIFIED_BACKUPS": self.shell_path(root / "verified.tsv"),
            "MOCK_RETENTION_FAIL_NAME": "",
            "MOCK_COMPLETE_FAIL": "0",
            "MOCK_DUMP_STARTED": "",
            "MOCK_DUMP_RELEASE": "",
            "DATABASE_URL": "postgres://backup.invalid/test",
            "AUDIT_DATABASE_URL": "postgres://audit.invalid/test",
            "BACKUP_DIR": self.shell_path(root / "backups"),
            "RETENTION_COUNT": "4",
        })
        return env, sql_log

    def add_backup(self, root: Path, year: int, *, verified: bool) -> Path:
        backup = root / "backups" / f"self-deepsearch-{year}0101T000000Z.dump"
        backup.parent.mkdir(exist_ok=True)
        data = f"backup-{year}\n".encode()
        digest = hashlib.sha256(data).hexdigest()
        backup.write_bytes(data)
        backup.with_suffix(".dump.sha256").write_text(f"{digest}  {backup.name}\n", encoding="utf-8")
        if verified:
            with (root / "verified.tsv").open("a", encoding="utf-8", newline="\n") as stream:
                stream.write(f"{backup.name}\t{digest}\t{len(data)}\n")
        return backup

    def run_script(self, name: str, env: dict[str, str]) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            self.script_command(name),
            cwd=ROOT,
            env=env,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            check=False,
            timeout=30,
        )

    @staticmethod
    def script_command(name: str) -> list[str]:
        # Git for Windows prepends /usr/bin when starting sh from a native
        # process. Restore mock precedence inside that shell, including date.
        return [
            str(SH), "-c",
            'if [ -n "${MOCK_BIN:-}" ]; then PATH="$MOCK_BIN:$PATH"; export PATH; fi; exec sh "$1"',
            "backup-workflow-test", (BACKUP_DIR / name).as_posix(),
        ]

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
