#!/usr/bin/env python3
from __future__ import annotations

import argparse
import http.cookiejar
import json
import os
import secrets
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Protocol


class E2EFailure(RuntimeError):
    pass


@dataclass(frozen=True)
class HTTPResult:
    status: int
    body: Any


class Client(Protocol):
    def request(self, method: str, path: str, payload: Any | None = None) -> HTTPResult: ...


class ReleaseAClient:
    def __init__(self, api_base: str, origin: str, timeout: float) -> None:
        self.api_base = api_base.rstrip("/")
        self.origin = origin
        self.timeout = timeout
        cookies = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

    def request(self, method: str, path: str, payload: Any | None = None) -> HTTPResult:
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        headers = {"Accept": "application/json", "Origin": self.origin}
        if data is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(self.api_base + path, data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=self.timeout) as response:
                return HTTPResult(response.status, decode_body(response.read()))
        except urllib.error.HTTPError as error:
            return HTTPResult(error.code, decode_body(error.read()))
        except OSError as error:
            raise E2EFailure(f"cannot reach API {self.api_base}: {error}") from error


def decode_body(raw: bytes) -> Any:
    if not raw:
        return None
    text = raw.decode("utf-8", errors="replace")
    try:
        return json.loads(text)
    except json.JSONDecodeError:
        return text


def expect(result: HTTPResult, status: int, step: str) -> Any:
    if result.status != status:
        raise E2EFailure(f"{step}: expected HTTP {status}, got {result.status}: {result.body!r}")
    return result.body


def require_object(value: Any, step: str) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise E2EFailure(f"{step}: expected a JSON object, got {value!r}")
    return value


def require_string(value: dict[str, Any], field: str, step: str) -> str:
    item = value.get(field)
    if not isinstance(item, str) or not item:
        raise E2EFailure(f"{step}: response is missing {field}")
    return item


def require_items(value: Any, step: str) -> list[dict[str, Any]]:
    body = require_object(value, step)
    items = body.get("items")
    if not isinstance(items, list) or any(not isinstance(item, dict) for item in items):
        raise E2EFailure(f"{step}: response does not contain an object items list")
    return items


def login(client: Client, email: str, password: str, step: str) -> dict[str, Any]:
    body = require_object(
        expect(client.request("POST", "/api/v1/auth/login", {"email": email, "password": password}), 200, step),
        step,
    )
    return require_object(body.get("user"), step)


def contains_entity(items: list[dict[str, Any]], entity_id: str) -> bool:
    return any(item.get("id") == entity_id for item in items)


def contains_slug(items: list[dict[str, Any]], slug: str) -> bool:
    return any(item.get("slug") == slug and item.get("entity_type") == "work" for item in items)


def canonical_slug(base: str, entity_id: str) -> str:
    short_id = entity_id.replace("-", "").lower()[-8:]
    if len(short_id) != 8 or not short_id.isalnum():
        raise E2EFailure("create work: entity id cannot produce a stable short id")
    available = 200 - len(short_id) - 1
    normalized = "-".join(part for part in base.lower().replace("_", "-").split("-") if part)
    normalized = "".join(character for character in normalized if character.isascii() and (character.isalnum() or character == "-"))
    normalized = normalized[:available].rstrip("-") or "work"
    return f"{normalized}-{short_id}"


def run_catalog_flow(
    editor: Client,
    reviewer: Client,
    *,
    editor_email: str,
    editor_password: str,
    reviewer_email: str,
    reviewer_password: str,
    code: str,
    slug: str,
    checked_at: str,
) -> dict[str, str]:
    expect(editor.request("GET", "/healthz"), 200, "API health")
    expect(editor.request("GET", "/readyz"), 200, "API readiness")

    editor_user = login(editor, editor_email, editor_password, "editor login")
    reviewer_user = login(reviewer, reviewer_email, reviewer_password, "reviewer login")
    editor_id = require_string(editor_user, "id", "editor login")
    reviewer_id = require_string(reviewer_user, "id", "reviewer login")
    editor_role = require_string(editor_user, "role", "editor login")
    reviewer_role = require_string(reviewer_user, "role", "reviewer login")
    if editor_role not in {"editor", "admin", "owner"}:
        raise E2EFailure(f"editor login: role {editor_role!r} cannot create catalog revisions")
    if reviewer_role not in {"admin", "owner"}:
        raise E2EFailure(f"reviewer login: role {reviewer_role!r} cannot approve or publish")
    if editor_id == reviewer_id:
        raise E2EFailure("editor and reviewer must be different accounts because authors cannot review their own revisions")

    source = {
        "source_type": "other",
        "source_url": None,
        "source_title": "Release A synthetic catalog E2E fixture",
        "checked_at": checked_at,
    }
    created = require_object(
        expect(
            editor.request(
                "POST",
                "/admin/v1/works",
                {
                    "code": code,
                    "title": f"Release A catalog E2E {code}",
                    "title_original": None,
                    "release_date": checked_at[:10],
                    "studio_id": None,
                    "summary": "Synthetic record created only to verify the Release A publication workflow.",
                    "performer_ids": [],
                    "sources": [source],
                    "reason": "Release A catalog end-to-end verification",
                },
            ),
            201,
            "create work",
        ),
        "create work",
    )
    entity_id = require_string(created, "id", "create work")
    revision_id = require_string(created, "revision_id", "create work")
    slug = canonical_slug(slug, entity_id)

    pending = require_items(
        expect(reviewer.request("GET", "/admin/v1/review-tasks?status=pending"), 200, "list pending review tasks"),
        "list pending review tasks",
    )
    matching_tasks = [item for item in pending if item.get("entity_id") == entity_id]
    if len(matching_tasks) != 1:
        raise E2EFailure(f"list pending review tasks: expected one task for {entity_id}, got {len(matching_tasks)}")
    task_id = require_string(matching_tasks[0], "id", "list pending review tasks")

    expect(reviewer.request("POST", f"/admin/v1/review-tasks/{task_id}/claim"), 200, "claim review task")
    expect(
        reviewer.request("POST", "/api/v1/auth/reauth", {"password": reviewer_password}),
        200,
        "reviewer recent password confirmation",
    )
    expect(
        reviewer.request(
            "POST",
            f"/admin/v1/review-tasks/{task_id}/approve",
            {"reason": "Release A catalog E2E approval"},
        ),
        200,
        "approve review task",
    )

    published = False
    hidden = False
    hide_payload = {"entity_type": "work", "reason": "Release A catalog E2E cleanup"}
    try:
        expect(
            reviewer.request(
                "POST",
                f"/admin/v1/entities/{entity_id}/publish",
                {
                    "entity_type": "work",
                    "revision_id": revision_id,
                    "canonical_slug": slug,
                    "reason": "Release A catalog E2E publication",
                },
            ),
            200,
            "publish work",
        )
        published = True

        query = urllib.parse.urlencode({"q": code})
        search_items = require_items(
            expect(editor.request("GET", f"/api/v1/search/works?{query}"), 200, "search published work"),
            "search published work",
        )
        if not contains_entity(search_items, entity_id):
            raise E2EFailure("search published work: the published entity is missing")

        detail = require_object(
            expect(editor.request("GET", f"/api/v1/works/{urllib.parse.quote(slug)}"), 200, "read published work"),
            "read published work",
        )
        if detail.get("id") != entity_id or detail.get("code") != code:
            raise E2EFailure("read published work: detail identity does not match the created entity")

        sitemap_items = require_items(
            expect(editor.request("GET", "/api/v1/site/sitemap?entity_type=work"), 200, "read work sitemap"),
            "read work sitemap",
        )
        if not contains_slug(sitemap_items, slug):
            raise E2EFailure("read work sitemap: published slug is missing")

        expect(
            reviewer.request("POST", f"/admin/v1/entities/{entity_id}/hide", hide_payload),
            200,
            "hide work",
        )
        hidden = True

        hidden_search = require_items(
            expect(editor.request("GET", f"/api/v1/search/works?{query}"), 200, "search hidden work"),
            "search hidden work",
        )
        if contains_entity(hidden_search, entity_id):
            raise E2EFailure("search hidden work: hidden entity is still searchable")
        expect(
            editor.request("GET", f"/api/v1/works/{urllib.parse.quote(slug)}"),
            404,
            "read hidden work",
        )
        hidden_sitemap = require_items(
            expect(editor.request("GET", "/api/v1/site/sitemap?entity_type=work"), 200, "read sitemap after hide"),
            "read sitemap after hide",
        )
        if contains_slug(hidden_sitemap, slug):
            raise E2EFailure("read sitemap after hide: hidden slug is still present")
    except E2EFailure as error:
        if published and not hidden:
            cleanup = reviewer.request("POST", f"/admin/v1/entities/{entity_id}/hide", hide_payload)
            if cleanup.status != 200:
                raise E2EFailure(
                    f"{error}; cleanup hide also failed with HTTP {cleanup.status}: {cleanup.body!r}"
                ) from error
        raise

    return {
        "status": "passed",
        "entity_id": entity_id,
        "revision_id": revision_id,
        "review_task_id": task_id,
        "code": code,
        "slug": slug,
        "final_state": "hidden",
    }


def default_identity() -> tuple[str, str, str]:
    suffix = datetime.now(timezone.utc).strftime("%Y%m%d%H%M%S") + "-" + secrets.token_hex(3)
    code = f"E2E-{suffix.upper()}"
    return code, code.lower(), datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def required_secret(env_name: str) -> str:
    value = os.environ.get(env_name, "")
    if not value:
        raise E2EFailure(f"required password environment variable is not set: {env_name}")
    return value


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Verify Release A create/review/publish/search/hide flow against a running API.",
        allow_abbrev=False,
    )
    parser.add_argument("--api-base", default="http://127.0.0.1:8080")
    parser.add_argument("--origin", default="http://127.0.0.1:3001")
    parser.add_argument("--editor-email", required=True)
    parser.add_argument("--reviewer-email", required=True)
    parser.add_argument("--editor-password-env", default="RELEASE_A_EDITOR_PASSWORD")
    parser.add_argument("--reviewer-password-env", default="RELEASE_A_REVIEWER_PASSWORD")
    parser.add_argument("--timeout", type=float, default=15.0)
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    code, slug, checked_at = default_identity()
    try:
        editor_password = required_secret(args.editor_password_env)
        reviewer_password = required_secret(args.reviewer_password_env)
        result = run_catalog_flow(
            ReleaseAClient(args.api_base, args.origin, args.timeout),
            ReleaseAClient(args.api_base, args.origin, args.timeout),
            editor_email=args.editor_email,
            editor_password=editor_password,
            reviewer_email=args.reviewer_email,
            reviewer_password=reviewer_password,
            code=code,
            slug=slug,
            checked_at=checked_at,
        )
    except E2EFailure as error:
        print(f"Release A catalog E2E failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
