from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

import yaml

from scripts.generate_openapi_types import OPENAPI_PATH, OUTPUT_PATH, TypeGenerator, generated_source


class OpenAPITypesGeneratorTests(unittest.TestCase):
    def test_release_a_generated_types_are_current(self) -> None:
        self.assertEqual(generated_source(), OUTPUT_PATH.read_text(encoding="utf-8"))

    def test_every_openapi_schema_is_exported(self) -> None:
        document = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))
        schemas = document["components"]["schemas"]
        generated = generated_source()
        self.assertGreaterEqual(len(schemas), 87)
        for name in schemas:
            self.assertIn(f"export type {name} =", generated)

    def test_all_of_merges_required_properties_from_referenced_objects(self) -> None:
        schemas = {
            "Base": {
                "type": "object",
                "required": ["optional_until_required"],
                "properties": {"optional_until_required": {"type": "string"}},
            },
            "Child": {
                "allOf": [
                    {"$ref": "#/components/schemas/Base"},
                    {"type": "object", "required": ["id"], "properties": {"id": {"type": "integer"}}},
                ]
            },
        }
        generated = TypeGenerator(schemas).render_document()
        self.assertIn("optional_until_required: string;", generated)
        self.assertIn("id: number;", generated)
        self.assertNotIn("optional_until_required?:", generated)

    def test_generation_is_deterministic_for_an_equivalent_file(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            duplicate = Path(directory) / "openapi.yaml"
            duplicate.write_text(OPENAPI_PATH.read_text(encoding="utf-8"), encoding="utf-8")
            self.assertEqual(generated_source(), generated_source(duplicate))


if __name__ == "__main__":
    unittest.main()
