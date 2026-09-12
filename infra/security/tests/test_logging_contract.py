from __future__ import annotations

import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
LOGGING_ROOTS = (
    ROOT / "services" / "platform-api" / "cmd",
    ROOT / "services" / "platform-api" / "internal" / "httpapi",
    ROOT / "services" / "platform-worker" / "cmd",
    ROOT / "services" / "platform-worker" / "internal",
)


class LoggingContractTests(unittest.TestCase):
    def test_postgres_slow_query_logs_never_include_bound_parameter_values(self) -> None:
        for relative_path in ("compose.yaml", "infra/compose/japan/compose.yaml"):
            source = (ROOT / relative_path).read_text(encoding="utf-8")
            self.assertIn("log_min_duration_statement=500", source, relative_path)
            self.assertIn("log_parameter_max_length=0", source, relative_path)

    def test_operational_logs_do_not_emit_sensitive_error_values(self) -> None:
        forbidden = (
            '"error", err',
            '"error", failErr',
            '"error", recovered',
            '"user_id", user.ID',
            '"content_id", request.ContentID',
            '"aggregate_id", event.AggregateID',
        )
        violations: list[str] = []
        for root in LOGGING_ROOTS:
            for path in root.rglob("*.go"):
                source = path.read_text(encoding="utf-8")
                for pattern in forbidden:
                    if pattern in source:
                        violations.append(f"{path.relative_to(ROOT)}: {pattern}")
        self.assertEqual([], violations)

    def test_request_and_worker_failures_use_safe_classification(self) -> None:
        sources = "\n".join(
            path.read_text(encoding="utf-8")
            for root in LOGGING_ROOTS
            for path in root.rglob("*.go")
        )
        self.assertIn('"error_class", logsafe.ErrorClass(err)', sources)
        self.assertIn('"error_class", "internal"', sources)


if __name__ == "__main__":
    unittest.main()
