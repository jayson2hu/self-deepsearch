import assert from "node:assert/strict";
import test from "node:test";

import { pythonCandidates, resolvePython } from "../run_python.mjs";

test("python resolver prefers an explicit PYTHON executable", () => {
  const candidates = pythonCandidates("C:/workspace", "win32", { PYTHON: "C:/Python312/python.exe" }).map((candidate) => candidate.replaceAll("\\", "/"));
  assert.deepEqual(candidates, [
    "C:/Python312/python.exe",
    "C:/workspace/.venv/Scripts/python.exe",
    "python",
  ]);
});

test("python resolver falls back to the repository virtualenv", () => {
  const candidate = resolvePython(process.cwd(), process.platform, {});
  assert.ok(candidate.endsWith(process.platform === "win32" ? ".venv\\Scripts\\python.exe" : ".venv/bin/python") || candidate === (process.platform === "win32" ? "python" : "python3"));
});
