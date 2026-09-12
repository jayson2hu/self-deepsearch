import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[3]


class ContentProvenanceProductContractTests(unittest.TestCase):
    def test_manual_write_contract_requires_source_evidence(self) -> None:
        spec = yaml.safe_load((ROOT / "packages" / "api-contracts" / "openapi.yaml").read_text(encoding="utf-8"))
        schemas = spec["components"]["schemas"]
        for name in ("CreateWorkRequest", "CreatePerformerRequest", "CreateStudioRequest", "CreateRevisionRequest"):
            with self.subTest(schema=name):
                self.assertIn("sources", schemas[name]["required"])
                self.assertEqual(schemas[name]["properties"]["sources"]["minItems"], 1)
        self.assertIn("sources", schemas["ContentRevision"]["required"])
        self.assertEqual(schemas["SourceEvidenceInput"]["properties"]["source_url"]["type"], ["string", "null"])

    def test_provenance_persistence_is_transactional_and_publish_gated(self) -> None:
        store = (ROOT / "services" / "platform-api" / "internal" / "database" / "operations_store.go").read_text(encoding="utf-8")
        self.assertIn("insertRevisionProvenance", store)
        self.assertIn("copyRevisionProvenance", store)
        self.assertIn("revisionProvenanceComplete", store)
        self.assertIn("return operations.Entity{}, operations.ErrConflict", store)
        self.assertIn("INSERT INTO platform.field_provenance", store)

    def test_operations_ui_collects_and_displays_source_evidence(self) -> None:
        components = ROOT / "apps" / "ops-web" / "components"
        source_fields = (components / "source-evidence-fields.tsx").read_text(encoding="utf-8")
        versions = (ROOT / "apps" / "ops-web" / "app" / "catalog" / "[entityType]" / "[entityID]" / "page.tsx").read_text(encoding="utf-8")
        queue = (components / "review-queue.tsx").read_text(encoding="utf-8")
        self.assertIn('name="source_type"', source_fields)
        self.assertIn('name="source_checked_at"', source_fields)
        self.assertIn("字段来源", versions)
        self.assertIn("ReviewTaskActions", versions)
        self.assertIn("查看版本和来源", queue)
        self.assertNotIn('act(task, "approve")', queue)


if __name__ == "__main__":
    unittest.main()
