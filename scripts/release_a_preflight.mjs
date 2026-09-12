#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

import { resolvePython } from "./run_python.mjs";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

export function parseArgs(argv) {
  const options = {
    build: true,
    compose: false,
    e2e: false,
    performance: false,
    python: process.env.PYTHON?.trim() || "",
    skipPython: false,
  };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--skip-build") options.build = false;
    else if (argument === "--with-compose") options.compose = true;
    else if (argument === "--with-e2e") options.e2e = true;
    else if (argument === "--with-performance") options.performance = true;
    else if (argument === "--skip-python") options.skipPython = true;
    else if (argument === "--python") {
      options.python = argv[++index]?.trim() || "";
      if (!options.python) throw new Error("--python 需要可执行文件路径");
    } else if (argument === "--help" || argument === "-h") {
      options.help = true;
    } else {
      throw new Error(`未知参数：${argument}`);
    }
  }
  return options;
}

export function npmInvocation(args) {
  if (process.env.npm_execpath?.trim()) return { command: process.execPath, args: [process.env.npm_execpath, ...args] };
  if (process.platform === "win32") return { command: process.env.ComSpec || "cmd.exe", args: ["/d", "/s", "/c", `npm ${args.join(" ")}`] };
  return { command: "npm", args };
}

function run(name, command, args, extraEnv = {}) {
  process.stdout.write(`\n==> ${name}\n`);
  const result = spawnSync(command, args, {
    cwd: ROOT,
    env: { ...process.env, NEXT_TELEMETRY_DISABLED: "1", ...extraEnv },
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw new Error(`${name} 无法启动：${result.error.message}`);
  if (result.status !== 0) throw new Error(`${name} 失败（退出码 ${result.status ?? "未知"}）`);
}

function runNpm(name, args) {
  const invocation = npmInvocation(args);
  run(name, invocation.command, invocation.args);
}

export function goEnvironment(repositoryRoot = ROOT) {
  return {
    GOMODCACHE: path.join(repositoryRoot, ".cache", "go-mod"),
    GOCACHE: path.join(repositoryRoot, ".cache", "go-build"),
  };
}

export function dockerEnvironment(repositoryRoot = ROOT) {
  return {
    DOCKER_CONFIG: path.join(repositoryRoot, ".cache", "docker-config-preflight"),
  };
}

export function pythonExecutable(options, root = ROOT, platform = process.platform, env = process.env) {
  if (options.python) return options.python;
  return resolvePython(root, platform, env);
}

function runOpenAPITypeCheck(options, executed, skipped) {
  if (options.skipPython) {
    skipped.push("OpenAPI TypeScript generation check（Python 显式跳过）");
    return;
  }
  run("OpenAPI TypeScript generation check", pythonExecutable(options), ["scripts/generate_openapi_types.py", "--check"]);
  executed.push("OpenAPI TypeScript generation check");
}

function runPythonChecks(options, executed, skipped) {
  if (options.skipPython) {
    skipped.push("Python lint/typecheck/pytest（显式跳过）");
    return;
  }
  const python = pythonExecutable(options);
  run("Python ruff", python, ["-m", "ruff", "check", "workers/collector-python", "workers/media-python", "db/tests", "infra/backup", "infra/reverse-proxy", "infra/security", "scripts"]);
  executed.push("Python ruff");
  run("Python mypy", python, [
    "-m", "mypy",
    "workers/collector-python/collector",
    "workers/media-python/media_worker",
    "scripts/generate_openapi_types.py",
    "scripts/release_a_deployment_probe.py",
    "scripts/release_a_env_check.py",
    "scripts/release_a_email_e2e.py",
    "scripts/release_a_catalog_e2e.py",
    "scripts/release_a_core_e2e.py",
  ]);
  executed.push("Python mypy");
  run("Python pytest", python, ["-m", "pytest", "-q"], { PYTEST_DISABLE_PLUGIN_AUTOLOAD: "1" });
  executed.push("Python pytest");
}

function help() {
  return [
    "用法：node scripts/release_a_preflight.mjs [选项]",
    "",
    "默认执行 Go test/vet、OpenAPI 类型漂移、Markdown 本地链接、前端 lint/typecheck/build、Python ruff/mypy/pytest、前端单元和 standalone HTTP 冒烟。",
    "默认不启动 Docker、PostgreSQL、邮件或对象存储。",
    "",
    "--python <路径>   指定 Python 3.12 可执行文件",
    "--skip-python     显式跳过 Python（结果会标记为 skipped）",
    "--skip-build      跳过 Next.js production build，使用现有 standalone 产物",
    "--with-e2e        在 production build 后执行 Playwright 桌面/移动浏览器回归",
    "--with-performance 在 production build 后执行本地预热 LCP/CLS 性能门槛",
    "--with-compose    额外执行本地、日本和北京 Compose 静态渲染，不启动容器",
  ].join("\n");
}

export function main(argv = process.argv.slice(2)) {
  const options = parseArgs(argv);
  if (options.help) {
    process.stdout.write(`${help()}\n`);
    return;
  }
  const executed = [];
  const skipped = [];
  const goEnv = goEnvironment();
  run("Go test", "go", ["test", "./services/platform-api/...", "./services/platform-worker/..."], goEnv);
  executed.push("Go test");
  run("Go vet", "go", ["vet", "./services/platform-api/...", "./services/platform-worker/..."], goEnv);
  executed.push("Go vet");
  runOpenAPITypeCheck(options, executed, skipped);
  runNpm("Markdown local link check", ["run", "check:markdown-links"]);
  executed.push("Markdown local link check");
  runNpm("TypeScript", ["run", "typecheck"]);
  executed.push("TypeScript");
  runNpm("ESLint", ["run", "lint"]);
  executed.push("ESLint");
  runNpm("Production dependency audit", ["run", "audit:production"]);
  executed.push("Production dependency audit");
  runPythonChecks(options, executed, skipped);
  runNpm("Release tooling contract tests", ["run", "test:preflight"]);
  executed.push("Release tooling contract tests");
  runNpm("Frontend smoke unit tests", ["run", "test:frontend:smoke"]);
  executed.push("Frontend smoke unit tests");
  if (options.build) {
    runNpm("Next.js production build", ["run", "build"]);
    executed.push("Next.js production build");
  } else {
    skipped.push("Next.js production build（显式跳过）");
  }
  runNpm("Standalone frontend HTTP smoke", ["run", "smoke:frontend:release-a"]);
  executed.push("Standalone frontend HTTP smoke");
  run("Go to Next cache contract", process.execPath, ["scripts/release_a_cache_contract.mjs"], goEnv);
  executed.push("Go to Next cache contract");
  if (options.performance) {
    runNpm("Local browser performance probe", ["run", "probe:performance:release-a"]);
    executed.push("Local browser performance probe");
    runNpm("Local HTTP latency probe", ["run", "probe:http-latency:release-a"]);
    executed.push("Local HTTP latency probe");
  } else {
    skipped.push("Local browser/HTTP performance probes（使用 --with-performance 才执行）");
  }
  if (options.e2e) {
    runNpm("Playwright browser E2E", ["run", "test:e2e:release-a"]);
    executed.push("Playwright browser E2E");
  } else {
    skipped.push("Playwright browser E2E（使用 --with-e2e 才执行）");
  }
  if (options.compose) {
    const dockerEnv = dockerEnvironment();
    run("Local Compose config", "docker", ["compose", "config", "--quiet"], dockerEnv);
    run("Japan Compose config", "docker", ["compose", "--env-file", "infra/compose/japan/.env.example", "-f", "infra/compose/japan/compose.yaml", "--profile", "edge", "--profile", "monitoring", "--profile", "tools", "config", "--quiet"], dockerEnv);
    run("Beijing Compose config", "docker", ["compose", "--env-file", "infra/compose/beijing/.env.example", "-f", "infra/compose/beijing/compose.yaml", "config", "--quiet"], dockerEnv);
    executed.push("Compose config (local/japan/beijing)");
  } else {
    skipped.push("Compose config（使用 --with-compose 才执行）");
  }
  process.stdout.write(`\n${JSON.stringify({ status: "passed", executed, skipped }, null, 2)}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main();
  } catch (error) {
    process.stderr.write(`\nRelease A preflight failed: ${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  }
}
