from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

SCRIPTS = Path(__file__).resolve().parents[1]
# Console pytest does not add the repository root to sys.path like python -m
# pytest does. Resolve this sibling tool from its file location in either case.
sys.path.insert(0, str(SCRIPTS))

from generate_openapi_types import OPENAPI_PATH, OUTPUT_PATH, TypeGenerator, generated_source  # noqa: E402


class OpenAPITypesGeneratorTests(unittest.TestCase):
    def test_collection_does_not_require_the_repository_on_python_path(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                [sys.executable, "-I", "-m", "pytest", "--collect-only", "-q", "-p", "no:cacheprovider", str(Path(__file__).resolve())],
                cwd=directory, env={"PYTEST_DISABLE_PLUGIN_AUTOLOAD": "1"}, capture_output=True, text=True, timeout=15,
            )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("OpenAPITypesGeneratorTests::test_every_openapi_schema_is_exported", result.stdout)

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
