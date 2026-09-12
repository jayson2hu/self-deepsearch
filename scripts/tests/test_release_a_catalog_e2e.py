from __future__ import annotations

import importlib.util
import io
import sys
import unittest
from collections import defaultdict
from contextlib import redirect_stderr
from pathlib import Path
from typing import Any

SCRIPT = Path(__file__).resolve().parents[1] / "release_a_catalog_e2e.py"
SPEC = importlib.util.spec_from_file_location("release_a_catalog_e2e", SCRIPT)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class FakeClient:
    def __init__(self, responses: dict[tuple[str, str], list[Any]]) -> None:
        self.responses = defaultdict(list, responses)
        self.calls: list[tuple[str, str, Any]] = []

    def request(self, method: str, path: str, payload: Any | None = None) -> Any:
        self.calls.append((method, path, payload))
        values = self.responses[(method, path)]
        if not values:
            raise AssertionError(f"unexpected request: {method} {path}")
        return values.pop(0)


def result(status: int, body: Any = None) -> Any:
    return MODULE.HTTPResult(status, body)


class ReleaseACatalogE2ETests(unittest.TestCase):
    def test_catalog_flow_publishes_verifies_and_hides_synthetic_work(self) -> None:
        code = "E2E-20260908-ABC123"
        slug_base = code.lower()
        entity_id = "10000000-0000-4000-8000-000000000001"
        slug = f"{slug_base}-00000001"
        revision_id = "20000000-0000-4000-8000-000000000001"
        task_id = "30000000-0000-4000-8000-000000000001"
        query_path = "/api/v1/search/works?q=E2E-20260908-ABC123"
        sitemap_path = "/api/v1/site/sitemap?entity_type=work"

        editor = FakeClient(
            {
                ("GET", "/healthz"): [result(200, {"status": "ok"})],
                ("GET", "/readyz"): [result(200, {"status": "ready"})],
                ("POST", "/api/v1/auth/login"): [
                    result(200, {"user": {"id": "editor-id", "role": "editor", "email": "editor@example.test"}})
                ],
                ("POST", "/admin/v1/works"): [
                    result(201, {"id": entity_id, "revision_id": revision_id})
                ],
                ("GET", query_path): [
                    result(200, {"items": [{"id": entity_id}], "next_cursor": None}),
                    result(200, {"items": [], "next_cursor": None}),
                ],
                ("GET", f"/api/v1/works/{slug}"): [
                    result(200, {"id": entity_id, "code": code}),
                    result(404, {"code": "WORK_NOT_FOUND"}),
                ],
                ("GET", sitemap_path): [
                    result(200, {"items": [{"entity_type": "work", "slug": slug}]}),
                    result(200, {"items": []}),
                ],
            }
        )
        reviewer = FakeClient(
            {
                ("POST", "/api/v1/auth/login"): [
                    result(200, {"user": {"id": "reviewer-id", "role": "owner", "email": "owner@example.test"}})
                ],
                ("GET", "/admin/v1/review-tasks?status=pending"): [
                    result(200, {"items": [{"id": task_id, "entity_id": entity_id}]})
                ],
                ("POST", f"/admin/v1/review-tasks/{task_id}/claim"): [result(200, {"id": task_id})],
                ("POST", "/api/v1/auth/reauth"): [result(200, {"message": "ok"})],
                ("POST", f"/admin/v1/review-tasks/{task_id}/approve"): [result(200, {"id": task_id})],
                ("POST", f"/admin/v1/entities/{entity_id}/publish"): [result(200, {"id": entity_id})],
                ("POST", f"/admin/v1/entities/{entity_id}/hide"): [result(200, {"id": entity_id})],
            }
        )

        output = MODULE.run_catalog_flow(
            editor,
            reviewer,
            editor_email="editor@example.test",
            editor_password="editor-password-not-logged",
            reviewer_email="owner@example.test",
            reviewer_password="reviewer-password-not-logged",
            code=code,
            slug=slug_base,
            checked_at="2026-09-08T12:34:56Z",
        )

        self.assertEqual(output["final_state"], "hidden")
        self.assertEqual(output["entity_id"], entity_id)
        reauth_index = next(i for i, call in enumerate(reviewer.calls) if call[1] == "/api/v1/auth/reauth")
        approve_index = next(i for i, call in enumerate(reviewer.calls) if call[1].endswith("/approve"))
        publish_index = next(i for i, call in enumerate(reviewer.calls) if call[1].endswith("/publish"))
        self.assertLess(reauth_index, approve_index)
        self.assertLess(approve_index, publish_index)

    def test_catalog_flow_rejects_self_review_before_creating_data(self) -> None:
        shared_id = "same-user-id"
        editor = FakeClient(
            {
                ("GET", "/healthz"): [result(200)],
                ("GET", "/readyz"): [result(200)],
                ("POST", "/api/v1/auth/login"): [
                    result(200, {"user": {"id": shared_id, "role": "owner", "email": "owner@example.test"}})
                ],
            }
        )
        reviewer = FakeClient(
            {
                ("POST", "/api/v1/auth/login"): [
                    result(200, {"user": {"id": shared_id, "role": "owner", "email": "owner@example.test"}})
                ],
            }
        )

        with self.assertRaisesRegex(MODULE.E2EFailure, "different accounts"):
            MODULE.run_catalog_flow(
                editor,
                reviewer,
                editor_email="owner@example.test",
                editor_password="not-logged",
                reviewer_email="owner@example.test",
                reviewer_password="not-logged",
                code="E2E-ONE",
                slug="e2e-one",
                checked_at="2026-09-08T12:34:56Z",
            )
        self.assertFalse(any(path == "/admin/v1/works" for _, path, _ in editor.calls))

    def test_catalog_passwords_are_not_accepted_as_command_line_arguments(self) -> None:
        with redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            MODULE.parse_args(
                [
                    "--editor-email",
                    "editor@example.test",
                    "--reviewer-email",
                    "owner@example.test",
                    "--editor-password",
                    "must-not-appear-in-process-list",
                ]
            )

    def test_canonical_slug_combines_searchable_code_and_stable_entity_suffix(self) -> None:
        self.assertEqual(
            MODULE.canonical_slug("MK-1042", "10000000-0000-4000-8000-0000A1B2C3D4"),
            "mk-1042-a1b2c3d4",
        )


if __name__ == "__main__":
    unittest.main()
