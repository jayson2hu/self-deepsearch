#!/usr/bin/env python3
"""Read a pinned archive and create a private, deterministic metadata review inventory.

No network, database writes, image copies, automatic identity merging or publication.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import unicodedata
from collections import Counter, defaultdict
from datetime import date, datetime, timezone
from pathlib import Path

from collector_integration import ARCHIVE, load_archive

PUBLICATION_BLOCKERS = (
    "rights_review_required", "adult_confirmation_required", "editorial_review_required",
    "seo_classification_required", "seo_quality_review_required",
)


def normalized_text(value: str) -> str:
    return " ".join(unicodedata.normalize("NFKC", value).split())


def build_inventory(people: list[dict], media: list[dict], archive: dict, as_of: date) -> tuple[list, dict]:
    """Derived normalization never changes source values or establishes identities."""
    by_owner = {}
    media_ids = set()
    for item in media:
        owner, identifier = item["performer_id"], item["media_candidate_id"]
        if owner in by_owner or identifier in media_ids:
            raise ValueError("Duplicate avatar identity or owner")
        by_owner[owner] = item
        media_ids.add(identifier)
    rows, seen_ids, seen_keys = [], set(), set()
    name_groups = defaultdict(list)
    for person in sorted(people, key=lambda item: item["performer_id"]):
        identifier = person["performer_id"]
        key = (person["source_id"], person["external_id"])
        if identifier in seen_ids or key in seen_keys:
            raise ValueError("Duplicate source identity; refusing silent merge")
        seen_ids.add(identifier)
        seen_keys.add(key)
        name = normalized_text(person["name"])
        if not name:
            raise ValueError("Empty normalized name")
        aliases, alias_keys = [], {name.casefold()}
        for alias in person.get("aliases", []):
            value = normalized_text(alias)
            if value and value.casefold() not in alias_keys:
                alias_keys.add(value.casefold())
                aliases.append(value)
        observed = datetime.fromisoformat(person["checked_at"].replace("Z", "+00:00"))
        if observed.tzinfo is None:
            raise ValueError("Source observation must include a timezone")
        age = (as_of - observed.astimezone(timezone.utc).date()).days
        avatar = by_owner.get(identifier)
        warnings = []
        if not avatar:
            warnings.append("missing_avatar")
        if not aliases:
            warnings.append("no_distinct_aliases")
        if age < 0:
            warnings.append("future_observation")
        elif age > 30:
            warnings.append("observation_older_than_30_days")
        rows.append({
            "schema_version": "resource.inventory.v1", "performer_id": identifier,
            "source_id": person["source_id"], "external_id": person["external_id"],
            "display_name": name, "aliases": aliases,
            "source_observation": {field: person.get(field) for field in
                                   ("name", "aliases", "profile_url", "checked_at", "current_content_hash")},
            "avatar_candidate": {field: avatar[field] for field in
                                 ("media_candidate_id", "sha256", "width", "height", "byte_size")} if avatar else None,
            "rights_status": "needs_review", "adult_status": "unconfirmed",
            "publication_status": "staging", "indexable": False,
            "blockers": list(PUBLICATION_BLOCKERS), "warnings": warnings,
        })
        name_groups[name.casefold()].append(identifier)
    if set(by_owner) - seen_ids:
        raise ValueError("Orphan avatar candidate")
    collisions = [identifiers for _, identifiers in sorted(name_groups.items()) if len(identifiers) > 1]
    collision_ids = {identifier for group in collisions for identifier in group}
    for row in rows:
        if row["performer_id"] in collision_ids:
            row["warnings"].append("same_name_requires_identity_review")
    warnings = Counter(warning for row in rows for warning in row["warnings"])
    summary = {
        "schema_version": "resource.audit.v1", "as_of": as_of.isoformat(),
        "mode": "offline-staging-review-only", "archive": archive,
        "counts": {"performers": len(rows), "avatar_candidates": len(media),
                   "without_avatar": len(rows) - len(media), "with_distinct_aliases": sum(bool(r["aliases"]) for r in rows),
                   "same_name_groups": len(collisions), "same_name_records": len(collision_ids),
                   "blocked_from_publication": len(rows), "indexable": 0},
        "warnings": dict(sorted(warnings.items())), "same_name_review_groups": collisions,
        "publication_blockers": {reason: len(rows) for reason in PUBLICATION_BLOCKERS},
        "scope": "Existing pinned metadata only; no new crawl, media transfer, DB import, public release or SEO submission.",
    }
    return rows, summary


def organize(archive: Path, output: Path, as_of: date) -> dict:
    if output.resolve().is_relative_to(archive.resolve()):
        raise ValueError("Output must not modify the source archive")
    people, media, verification = load_archive(archive)
    rows, summary = build_inventory(people, media, verification, as_of)
    # Validate everything first; never overwrite a previous review or its decisions.
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    raw = "".join(json.dumps(row, ensure_ascii=False, sort_keys=True, allow_nan=False) + "\n" for row in rows).encode()
    (output / "inventory.jsonl").write_bytes(raw)
    summary["inventory_sha256"] = hashlib.sha256(raw).hexdigest()
    (output / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (output / "README.md").write_text(
        "# Private resource review inventory\n\n"
        "Do not serve this directory publicly or commit its data. The source archive is unchanged.\n"
        "inventory.jsonl preserves source observations; normalization is derived only.\n"
        "Same-name records are flagged, never merged. Avatar gaps are warnings, not evidence of absence.\n"
        "Every record remains staging/noindex until separate rights, adult, editorial and SEO review.\n"
        "No images were copied or downloaded; candidate references are not display permissions.\n",
        encoding="utf-8",
    )
    return summary


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, default=ARCHIVE)
    parser.add_argument("--output", type=Path, required=True, help="New private directory (prefer .cache/resource-audit/)")
    parser.add_argument("--as-of", type=date.fromisoformat, default=datetime.now(timezone.utc).date())
    args = parser.parse_args()
    summary = organize(args.archive, args.output, args.as_of)
    # Never print names, candidate URLs, archive paths or credentials into logs.
    print(json.dumps({"mode": summary["mode"], "counts": summary["counts"],
                      "inventory_sha256": summary["inventory_sha256"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
