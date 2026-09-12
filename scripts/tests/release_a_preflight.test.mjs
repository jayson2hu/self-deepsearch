import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { dockerEnvironment, goEnvironment, npmInvocation, parseArgs, pythonExecutable } from "../release_a_preflight.mjs";

test("preflight options keep Docker disabled by default", () => {
  const options = parseArgs([]);
  assert.equal(options.build, true);
  assert.equal(options.compose, false);
  assert.equal(options.e2e, false);
  assert.equal(options.performance, false);
  assert.equal(options.skipPython, false);
});

test("preflight includes the production dependency audit", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  assert.match(source, /runNpm\("Production dependency audit", \["run", "audit:production"\]\)/);
});

test("preflight requires the real cache contract after the standalone smoke", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  const smoke = source.indexOf('runNpm("Standalone frontend HTTP smoke"');
  const cache = source.indexOf('run("Go to Next cache contract", process.execPath, ["scripts/release_a_cache_contract.mjs"], goEnv)');
  assert.ok(smoke >= 0 && cache > smoke);
});

test("preflight parses explicit dependency options", () => {
  const options = parseArgs(["--skip-build", "--with-compose", "--with-e2e", "--with-performance", "--python", "C:/Python312/python.exe"]);
  assert.equal(options.build, false);
  assert.equal(options.compose, true);
  assert.equal(options.e2e, true);
  assert.equal(options.performance, true);
  assert.equal(options.python, "C:/Python312/python.exe");
});

test("preflight rejects a missing explicit Python path and resolves the repository virtualenv", () => {
  assert.throws(() => parseArgs(["--python"]), /需要可执行文件路径/);

  const root = mkdtempSync(path.join(os.tmpdir(), "release-a-preflight-python-"));
  try {
    const repositoryPython = path.join(root, ".venv", "Scripts", "python.exe");
    mkdirSync(path.dirname(repositoryPython), { recursive: true });
    writeFileSync(repositoryPython, "");
    assert.equal(pythonExecutable({ python: "" }, root, "win32", {}), repositoryPython);
    assert.equal(
      pythonExecutable({ python: "C:/Python312/python.exe" }, root, "win32", {}),
      "C:/Python312/python.exe",
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("npm invocation uses the active npm CLI when available", () => {
  const previous = process.env.npm_execpath;
  process.env.npm_execpath = "C:/npm/npm-cli.js";
  try {
    assert.deepEqual(npmInvocation(["run", "lint"]), {
      command: process.execPath,
      args: ["C:/npm/npm-cli.js", "run", "lint"],
    });
  } finally {
    if (previous === undefined) delete process.env.npm_execpath;
    else process.env.npm_execpath = previous;
  }
});

test("Go caches stay inside the repository workspace", () => {
  const environment = goEnvironment("C:/workspace/self-deepsearch");
  assert.match(environment.GOMODCACHE.replaceAll("\\", "/"), /\/\.cache\/go-mod$/);
  assert.match(environment.GOCACHE.replaceAll("\\", "/"), /\/\.cache\/go-build$/);
  const docker = dockerEnvironment("C:/workspace/self-deepsearch");
  assert.match(docker.DOCKER_CONFIG.replaceAll("\\", "/"), /\/\.cache\/docker-config-preflight$/);
});

test("preflight checks generated OpenAPI types before TypeScript", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  const generation = source.lastIndexOf("runOpenAPITypeCheck(options, executed, skipped)");
  const typecheck = source.indexOf('runNpm("TypeScript", ["run", "typecheck"])');
  assert.ok(generation >= 0 && typecheck > generation);
});

test("preflight runs Python type checking and isolated pytest", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  assert.match(source, /run\("Python mypy"/);
  assert.match(source, /scripts\/release_a_env_check\.py/);
  assert.match(source, /scripts\/release_a_deployment_probe\.py/);
  assert.match(source, /scripts\/release_a_core_e2e\.py/);
  assert.match(source, /run\("Python pytest"/);
  assert.match(source, /PYTEST_DISABLE_PLUGIN_AUTOLOAD: "1"/);
});

test("optional Playwright E2E runs after the standalone HTTP smoke", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  const smoke = source.indexOf('runNpm("Standalone frontend HTTP smoke"');
  const e2e = source.indexOf('runNpm("Playwright browser E2E"');
  assert.ok(smoke >= 0 && e2e > smoke);
  assert.match(source, /Playwright browser E2E（使用 --with-e2e 才执行）/);
});

test("optional browser performance probe runs after HTTP smoke and before E2E", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  const smoke = source.indexOf('runNpm("Standalone frontend HTTP smoke"');
  const performance = source.indexOf('runNpm("Local browser performance probe"');
  const latency = source.indexOf('runNpm("Local HTTP latency probe"');
  const e2e = source.indexOf('runNpm("Playwright browser E2E"');
  assert.ok(smoke >= 0 && performance > smoke && latency > performance && e2e > latency);
  assert.match(source, /Local browser\/HTTP performance probes（使用 --with-performance 才执行）/);
});

test("unified preflight executes its release tooling contracts", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preflight.mjs", import.meta.url), "utf8"));
  assert.match(source, /runNpm\("Markdown local link check", \["run", "check:markdown-links"\]\)/);
  assert.match(source, /runNpm\("Release tooling contract tests", \["run", "test:preflight"\]\)/);
  assert.match(source, /"--profile", "edge", "--profile", "monitoring", "--profile", "tools"/);
});
