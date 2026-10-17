import copy
import json
import sys
import tempfile
import unittest
from datetime import date
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import organize_resources as subject


def person(identifier="one", name="Sample", aliases=None, checked_at="2026-10-01T00:00:00Z"):
    return {"performer_id": identifier, "external_id": identifier, "source_id": "fixture",
            "name": name, "aliases": aliases or [], "checked_at": checked_at,
            "profile_url": "https://source.example/" + identifier, "current_content_hash": "sha256:fixture"}


def avatar(owner="one", identifier="media-one"):
    return {"performer_id": owner, "media_candidate_id": identifier, "sha256": "fixture",
            "width": 125, "height": 125, "byte_size": 100}


class ResourceTests(unittest.TestCase):
    def test_normalization_preserves_observations_and_does_not_merge_names(self):
        people = [person("one", "  ＡＩＫＡ  ", ["AIKA", "Alias", "Ａｌｉａｓ", "  "]), person("two", "aika")]
        original = copy.deepcopy(people)
        rows, report = subject.build_inventory(people, [avatar()], {}, date(2026, 10, 17))
        self.assertEqual(people, original)
        self.assertEqual(rows[0]["display_name"], "AIKA")
        self.assertEqual(rows[0]["aliases"], ["Alias"])
        self.assertEqual(rows[0]["source_observation"]["name"], "  ＡＩＫＡ  ")
        self.assertEqual((len(rows), report["counts"]["same_name_groups"]), (2, 1))
        self.assertTrue(all("same_name_requires_identity_review" in row["warnings"] for row in rows))

    def test_every_resource_remains_unpublished_and_noindex(self):
        rows, report = subject.build_inventory([person()], [avatar()], {}, date(2026, 10, 17))
        self.assertFalse(rows[0]["indexable"])
        self.assertEqual(rows[0]["adult_status"], "unconfirmed")
        self.assertEqual(rows[0]["publication_status"], "staging")
        self.assertEqual(rows[0]["blockers"], list(subject.PUBLICATION_BLOCKERS))
        self.assertEqual(report["counts"]["indexable"], 0)
        self.assertNotIn("local_path", rows[0]["avatar_candidate"])

    def test_missing_avatar_is_only_a_warning_and_observation_timezone_is_respected(self):
        rows, report = subject.build_inventory([person(checked_at="2026-10-18T00:30:00+08:00")], [], {}, date(2026, 10, 17))
        self.assertEqual(report["counts"]["without_avatar"], 1)
        self.assertIn("missing_avatar", rows[0]["warnings"])
        self.assertNotIn("future_observation", rows[0]["warnings"])
        self.assertNotIn("missing_avatar", rows[0]["blockers"])

    def test_stale_and_future_observations_are_not_silently_refreshed(self):
        people = [person("past", checked_at="2026-01-01T00:00:00Z"), person("future", checked_at="2027-01-01T00:00:00Z")]
        rows, report = subject.build_inventory(people, [], {}, date(2026, 10, 17))
        self.assertEqual(report["warnings"]["future_observation"], 1)
        self.assertEqual(report["warnings"]["observation_older_than_30_days"], 1)
        self.assertTrue(all(row["source_observation"]["checked_at"] != "2026-10-17T00:00:00Z" for row in rows))

    def test_duplicate_identities_or_empty_names_are_rejected(self):
        for people in [[person(), person()], [person(name=" \t ")], [person(checked_at="2026-10-01T00:00:00")]]:
            with self.subTest(people=people), self.assertRaises(ValueError):
                subject.build_inventory(people, [], {}, date(2026, 10, 17))

    def test_duplicate_or_orphan_media_is_rejected(self):
        for media in [[avatar("missing")], [avatar(), avatar()], [avatar(), avatar("two")]]:
            with self.subTest(media=media), self.assertRaises(ValueError):
                subject.build_inventory([person()], media, {}, date(2026, 10, 17))

    def test_repeated_output_is_identical_and_existing_review_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with patch.object(subject, "load_archive", return_value=([person()], [avatar()], {"verified": True})):
                first = subject.organize(root / "archive", root / "a", date(2026, 10, 17))
                second = subject.organize(root / "archive", root / "b", date(2026, 10, 17))
                self.assertEqual(first, second)
                self.assertEqual((root / "a/inventory.jsonl").read_bytes(), (root / "b/inventory.jsonl").read_bytes())
                self.assertEqual(json.loads((root / "a/summary.json").read_text())["counts"]["performers"], 1)
                with self.assertRaises(FileExistsError):
                    subject.organize(root / "archive", root / "a", date(2026, 10, 17))

    def test_output_cannot_change_source_archive(self):
        with tempfile.TemporaryDirectory() as directory:
            archive = Path(directory)
            with patch.object(subject, "load_archive") as load:
                with self.assertRaises(ValueError):
                    subject.organize(archive, archive / "output", date(2026, 10, 17))
                load.assert_not_called()


if __name__ == "__main__":
    unittest.main()
