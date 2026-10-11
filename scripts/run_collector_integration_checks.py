#!/usr/bin/env python3
"""Reproduce offline regressions and the collector's real archive export/showcase."""

import json
import os
import re
import shutil
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
COLLECTOR = ROOT.parent / "self-deepsearch-collector"
RUNTIME = ROOT / ".cache/collector-integration"
EVIDENCE = ROOT / "docs/evidence/collector-integration-2026-09-19"


def main():
    RUNTIME.mkdir(parents=True, exist_ok=True)
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    report = {"checked_at": datetime.now(timezone.utc).isoformat(), "status": "running", "checks": []}

    def run(name, command, cwd=ROOT):
        environment = {**os.environ, "PYTHONDONTWRITEBYTECODE": "1"}
        result = subprocess.run(command, cwd=cwd, env=environment, text=True, capture_output=True, timeout=120)
        output = result.stdout + result.stderr
        (RUNTIME / (name + ".log")).write_text(output)
        totals = re.findall(r"^# tests (\d+)$", output, flags=re.M)
        python_total = re.search(r"Ran (\d+) tests", output)
        entry = {
            "name": name,
            "exit_code": result.returncode,
            "status": "passed" if result.returncode == 0 else "failed",
        }
        if totals:
            entry["assertion_tests"] = sum(map(int, totals))
        if python_total:
            entry["python_tests"] = int(python_total[1])
        report["checks"].append(entry)
        print(json.dumps(entry), flush=True)
        if result.returncode:
            raise RuntimeError(name + " failed; inspect local runtime log")

    try:
        run(
            "adapter-unit",
            ["python3", "-m", "unittest", "discover", "-s", "scripts/tests", "-p", "test_collector_integration.py"],
        )
        run("collector-default", ["npm", "test"], COLLECTOR)
        # Direct invocation is compatible with Node 22 and reports actual individual tests.
        run("collector-capture", ["node", "scripts/tests/jable_capture.test.mjs"], COLLECTOR)
        for name in [
            "display_security_policy",
            "release_a_frontend_smoke",
            "work_csv_errors",
            "media_policy",
            "media_upload_client",
            "media_edge_gateway",
            "ops_security_policy",
            "redesign_handoff",
        ]:
            run(name, ["node", f"scripts/tests/{name}.test.mjs"])
        run("markdown-links", ["npm", "run", "check:markdown-links"])
        archive = COLLECTOR / "runtime/jable-archive/20260919-final"
        # Even SQLite mode=ro may create WAL/SHM sidecars; all collector commands use a copy.
        working = RUNTIME / "collector-input"
        working.mkdir(exist_ok=True)
        shutil.copy2(archive / "collector.db", working / "collector.db")
        shutil.copytree(archive / "media", working / "media", dirs_exist_ok=True)
        run(
            "collector-archive-audit",
            [
                "npm",
                "run",
                "collector",
                "--",
                "audit-jable",
                "--database",
                str(working / "collector.db"),
                "--output-dir",
                str(RUNTIME / "collector-audit"),
            ],
            COLLECTOR,
        )
        run(
            "collector-export",
            [
                "npm",
                "run",
                "collector",
                "--",
                "export-selfdeepsearch",
                "--database",
                str(working / "collector.db"),
                "--output-dir",
                str(RUNTIME / "collector-export"),
            ],
            COLLECTOR,
        )

        def lines(file):
            return {
                json.dumps(json.loads(line), sort_keys=True, ensure_ascii=False)
                for line in file.read_text().splitlines()
                if line.strip()
            }

        assert lines(RUNTIME / "collector-export/performers.jsonl") == lines(
            archive / "selfdeepsearch-export/performers.jsonl"
        )
        assert lines(RUNTIME / "collector-export/performer_media_staging.jsonl") == lines(
            archive / "selfdeepsearch-export/performer_media_staging.jsonl"
        )
        report["checks"].append({"name": "fresh_export_matches_archive", "status": "passed"})
        run(
            "collector-showcase-build",
            [
                "npm",
                "run",
                "collector",
                "--",
                "build-performer-showcase",
                "--database",
                str(working / "collector.db"),
                "--output-dir",
                str(RUNTIME / "collector-showcase"),
            ],
            COLLECTOR,
        )
        run(
            "collector-showcase-browser",
            ["node", "scripts/test_jable_showcase.mjs", str(RUNTIME / "collector-showcase")],
            COLLECTOR,
        )
        showcase = json.loads((RUNTIME / "collector-showcase/browser-report.json").read_text())
        (EVIDENCE / "collector-browser-report.json").write_text(json.dumps(showcase, indent=2) + "\n")
        run("archive-still-unchanged", ["python3", "scripts/collector_integration.py", "verify"])
        report["status"] = "passed"
    finally:
        if report["status"] != "passed":
            report["status"] = "failed"
        (EVIDENCE / "regression-report.json").write_text(json.dumps(report, indent=2) + "\n")


if __name__ == "__main__":
    main()
