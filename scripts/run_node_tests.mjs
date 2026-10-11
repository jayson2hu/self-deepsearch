#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const TEST_DIRECTORY = fileURLToPath(new URL("./tests", import.meta.url));

export function discoverTests(directory = TEST_DIRECTORY) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const filename = path.join(directory, entry.name);
    if (entry.isDirectory()) return discoverTests(filename);
    return entry.isFile() && entry.name.endsWith(".test.mjs") ? [filename] : [];
  }).sort();
}

export function runTests(directory = TEST_DIRECTORY, stdio = "inherit") {
  const tests = discoverTests(directory);
  if (!tests.length) throw new Error(`No Node test files found in ${directory}`);
  const env = { ...process.env };
  // This process is a test runner, not a child test file of a surrounding runner.
  delete env.NODE_TEST_CONTEXT;
  const result = spawnSync(process.execPath, ["--test", ...tests], { stdio, env, windowsHide: true });
  if (result.error) throw result.error;
  return result.status ?? 1;
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = runTests();
}
