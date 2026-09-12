#!/usr/bin/env node

import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { promises as fs } from "node:fs";
import http from "node:http";
import net from "node:net";
import path from "node:path";
import process from "node:process";
import { fileURLToPath, pathToFileURL } from "node:url";

const SCRIPT_DIR = path.dirname(fileURLToPath(import.meta.url));
export const REPOSITORY_ROOT = path.resolve(SCRIPT_DIR, "..");
export const DEFAULT_TIMEOUT_MS = 45_000;
const POLL_INTERVAL_MS = 100;
const REQUEST_TIMEOUT_MS = 5_000;

export class SmokeFailure extends Error {
  constructor(message, details = "") {
    super(details ? `${message}\n${details}` : message);
    this.name = "SmokeFailure";
  }
}

export function parseArgs(argv) {
  const options = { build: false, help: false, timeoutMs: DEFAULT_TIMEOUT_MS };
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--build") {
      options.build = true;
      continue;
    }
    if (argument === "--help" || argument === "-h") {
      options.help = true;
      continue;
    }
    if (argument === "--timeout") {
      const value = Number(argv[++index]);
      if (!Number.isFinite(value) || value < 5_000) {
        throw new SmokeFailure("--timeout 必须是至少 5000 毫秒的数字");
      }
      options.timeoutMs = value;
      continue;
    }
    throw new SmokeFailure(`未知参数：${argument}`);
  }
  return options;
}

export function standalonePaths(appName, repositoryRoot = REPOSITORY_ROOT) {
  const appRoot = path.join(repositoryRoot, "apps", appName);
  const standaloneAppRoot = path.join(appRoot, ".next", "standalone", "apps", appName);
  return {
    appRoot,
    server: path.join(standaloneAppRoot, "server.js"),
    sourceStatic: path.join(appRoot, ".next", "static"),
    targetStatic: path.join(standaloneAppRoot, ".next", "static"),
    sourcePublic: path.join(appRoot, "public"),
    targetPublic: path.join(standaloneAppRoot, "public"),
  };
}

export function missingSignals(text, signals) {
  return signals.filter((signal) => !text.includes(signal));
}

export function pageFailures(name, result, { status = 200, includes = [] } = {}) {
  const failures = [];
  if (result.status !== status) failures.push(`${name}: expected HTTP ${status}, got ${result.status}`);
  const missing = missingSignals(result.body, includes);
  if (missing.length > 0) failures.push(`${name}: missing signals: ${missing.join(", ")}`);
  return failures;
}

export function proxyFailures(name, result) {
  const failures = pageFailures(name, result, { status: 503 });
  if (result.headers.get("cache-control")?.toLowerCase() !== "no-store") {
    failures.push(`${name}: expected Cache-Control: no-store`);
  }
  if (!result.body.includes('"API_UNAVAILABLE"')) {
    failures.push(`${name}: expected API_UNAVAILABLE response code`);
  }
  return failures;
}

export function countOccurrences(text, needle) {
  if (!needle) return 0;
  let count = 0;
  let offset = 0;
  while (true) {
    const found = text.indexOf(needle, offset);
    if (found < 0) return count;
    count += 1;
    offset = found + needle.length;
  }
}

export function presentSignals(text, signals) {
  const normalized = text.toLowerCase();
  return signals.filter((signal) => normalized.includes(signal.toLowerCase()));
}

export function stylesheetPaths(text) {
  const paths = [];
  for (const match of text.matchAll(/<link\b[^>]*>/gi)) {
    const tag = match[0];
    const rel = attributeValue(tag, "rel").toLowerCase().split(/\s+/);
    const href = attributeValue(tag, "href");
    if (rel.includes("stylesheet") && href.startsWith("/")) paths.push(href);
  }
  return [...new Set(paths)];
}

function attributeValue(tag, name) {
  const match = tag.match(new RegExp(`${name}\\s*=\\s*(?:"([^"]*)"|'([^']*)'|([^\\s>]+))`, "i"));
  return match?.[1] ?? match?.[2] ?? match?.[3] ?? "";
}

export function stylesheetFailures(name, result) {
  const failures = pageFailures(name, result);
  const contentType = result.headers.get("content-type")?.toLowerCase().split(";", 1)[0];
  if (contentType !== "text/css") failures.push(`${name}: expected text/css content type, got ${contentType || "missing"}`);
  if (result.body.trim().length === 0) failures.push(`${name}: stylesheet body is empty`);
  return failures;
}

export function securityHeaderFailures(name, result, { allowTurnstile = false, requireScriptNonce = false } = {}) {
  const failures = [];
  const csp = result.headers.get("content-security-policy") ?? "";
  for (const directive of ["default-src 'self'", "object-src 'none'", "frame-ancestors 'none'", "form-action 'self'"]) {
    if (!csp.includes(directive)) failures.push(`${name}: CSP missing ${directive}`);
  }
  const hasTurnstile = csp.includes("https://challenges.cloudflare.com");
  if (hasTurnstile !== allowTurnstile) {
    failures.push(`${name}: CSP Turnstile allowance does not match this surface`);
  }
  if (requireScriptNonce) {
    const scriptPolicy = csp.split(";").map((directive) => directive.trim()).find((directive) => directive.startsWith("script-src ")) ?? "";
    const nonce = scriptPolicy.match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1] ?? "";
    if (!nonce) failures.push(`${name}: CSP script-src is missing a strong nonce`);
    if (!scriptPolicy.includes("'strict-dynamic'")) failures.push(`${name}: CSP script-src is missing strict-dynamic`);
    if (scriptPolicy.includes("'unsafe-inline'")) failures.push(`${name}: CSP script-src still allows unsafe-inline`);
    if (!csp.includes("script-src-attr 'none'")) failures.push(`${name}: CSP permits script attributes`);
    if (!(result.headers.get("cache-control") ?? "").toLowerCase().includes("no-store")) {
      failures.push(`${name}: nonce HTML must be no-store`);
    }
    for (const match of result.body.matchAll(/<script\b([^>]*)>/gi)) {
      const type = attributeValue(match[1], "type").toLowerCase();
      const executable = !type || type === "module" || type === "text/javascript" || type === "application/javascript";
      if (executable && attributeValue(match[1], "nonce") !== nonce) {
        failures.push(`${name}: executable script is missing the response CSP nonce`);
        break;
      }
    }
  }
  if (result.headers.get("x-content-type-options")?.toLowerCase() !== "nosniff") {
    failures.push(`${name}: expected X-Content-Type-Options: nosniff`);
  }
  if (result.headers.get("x-frame-options")?.toUpperCase() !== "DENY") {
    failures.push(`${name}: expected X-Frame-Options: DENY`);
  }
  if (!result.headers.get("referrer-policy")) failures.push(`${name}: missing Referrer-Policy`);
  if (!result.headers.get("permissions-policy")) failures.push(`${name}: missing Permissions-Policy`);
  return failures;
}

export function nonceRotationFailures(name, first, second) {
  const nonce = (result) => (result.headers.get("content-security-policy") ?? "")
    .match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1] ?? "";
  const firstNonce = nonce(first);
  const secondNonce = nonce(second);
  if (!firstNonce || !secondNonce) return [`${name}: response CSP nonce is missing`];
  return firstNonce === secondNonce ? [`${name}: response CSP nonce was reused across requests`] : [];
}

export function jsonLDFailures(name, result, expectedType) {
  const failures = pageFailures(name, result);
  const scripts = [];
  for (const match of result.body.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script\s*>/gi)) {
    if (attributeValue(match[1], "type").toLowerCase() === "application/ld+json") scripts.push(match[2]);
  }
  if (scripts.length === 0) {
    failures.push(`${name}: missing JSON-LD script`);
    return failures;
  }

  const documents = [];
  for (const script of scripts) {
    if (script.includes("<")) failures.push(`${name}: JSON-LD contains an unescaped less-than character`);
    try {
      documents.push(JSON.parse(script));
    } catch {
      failures.push(`${name}: JSON-LD is not valid JSON`);
    }
  }
  const document = documents.find((candidate) => candidate?.["@type"] === expectedType);
  if (!document) failures.push(`${name}: missing ${expectedType} JSON-LD type`);
  else if (!document.mainEntityOfPage) failures.push(`${name}: missing JSON-LD canonical entity reference`);
  return failures;
}

export const RELEASE_A_FORBIDDEN_AD_SIGNALS = [
  "pagead2.googlesyndication.com",
  "googleads.g.doubleclick.net",
  "adsbygoogle",
  "data-ad-state=\"active\"",
  "data-ad-behavior=\"floating\"",
  "data-ad-close-countdown",
  "popunder",
];

function helpText() {
  return [
    "用法：node scripts/release_a_frontend_smoke.mjs [--build] [--timeout <毫秒>]",
    "",
    "启动已构建的 display-web 和 ops-web standalone server，在随机端口执行 Release A 前端 HTTP 冒烟。",
    "默认不启动 Docker、PostgreSQL 或 Go API；脚本使用本地 mock API 返回合成详情，并对代理错误态主动断开连接。",
    "若 standalone 产物不存在，先运行 npm run build，或追加 --build 让脚本先构建。",
  ].join("\n");
}

async function pathExists(target) {
  try {
    await fs.access(target);
    return true;
  } catch {
    return false;
  }
}

export function npmBuildInvocation({
  platform = process.platform,
  npmExecPath = process.env.npm_execpath,
  nodeExecutable = process.execPath,
  commandShell = process.env.ComSpec,
} = {}) {
  if (npmExecPath?.trim()) {
    return { command: nodeExecutable, args: [npmExecPath, "run", "build"] };
  }
  if (platform === "win32") {
    return { command: commandShell?.trim() || "cmd.exe", args: ["/d", "/s", "/c", "npm run build"] };
  }
  return { command: "npm", args: ["run", "build"] };
}

async function runBuild(repositoryRoot) {
  const invocation = npmBuildInvocation();
  const result = spawnSync(invocation.command, invocation.args, {
    cwd: repositoryRoot,
    env: { ...process.env, NEXT_TELEMETRY_DISABLED: "1" },
    stdio: "inherit",
    windowsHide: true,
  });
  if (result.error) throw new SmokeFailure(`无法启动 npm build：${result.error.message}`);
  if (result.status !== 0) throw new SmokeFailure(`npm run build 失败（退出码 ${result.status ?? "未知"}）`);
}

export async function ensureArtifacts({ build = false, repositoryRoot = REPOSITORY_ROOT } = {}) {
  const display = standalonePaths("display-web", repositoryRoot);
  const ops = standalonePaths("ops-web", repositoryRoot);
  if (build) await runBuild(repositoryRoot);
  if (!(await pathExists(display.server)) || !(await pathExists(ops.server))) {
    throw new SmokeFailure(
      "找不到前端 standalone 构建产物。",
      "请先运行 npm run build，或使用 npm run preview:release-a:build / npm run smoke:frontend:release-a:build。",
    );
  }
  for (const [name, paths] of [["display-web", display], ["ops-web", ops]]) {
    if (!(await pathExists(paths.server))) {
      throw new SmokeFailure(`${name} standalone server.js 仍不存在：${paths.server}`);
    }
  }
  return { display, ops };
}

/**
 * Next's standalone output intentionally leaves static and public files beside
 * the output directory. Stage them only when absent so a local smoke run has
 * the same asset layout as the documented production deployment.
 */
export async function prepareStandaloneAssets(paths) {
  const created = [];
  if (!(await pathExists(paths.targetStatic)) && (await pathExists(paths.sourceStatic))) {
    await fs.cp(paths.sourceStatic, paths.targetStatic, { recursive: true });
    created.push(paths.targetStatic);
  }
  if (!(await pathExists(paths.targetPublic)) && (await pathExists(paths.sourcePublic))) {
    await fs.cp(paths.sourcePublic, paths.targetPublic, { recursive: true });
    created.push(paths.targetPublic);
  }
  return created;
}

export async function findFreePort({ host = "127.0.0.1", avoid = new Set() } = {}) {
  for (let attempt = 0; attempt < 12; attempt += 1) {
    const port = await new Promise((resolve, reject) => {
      const server = net.createServer();
      server.once("error", reject);
      server.listen(0, host, () => {
        const address = server.address();
        const selected = typeof address === "object" && address ? address.port : 0;
        server.close((error) => (error ? reject(error) : resolve(selected)));
      });
    });
    if (port > 0 && !avoid.has(port)) return port;
  }
  throw new SmokeFailure("无法分配空闲测试端口");
}

const MOCK_WORK_ITEM = {
  id: "10000000-0000-4000-8000-000000000001",
  type: "work",
  code: "TEST-JSONLD",
  title: "安全结构化数据 </script><script>alert(1)</script>",
  subtitle: "2026-09-08 · 测试厂牌",
  href: "/works/test-jsonld-00000001",
  image_url: null,
  image: null,
};

const MOCK_PREVIEW_WORK_ITEM = {
  id: "10000000-0000-4000-8000-000000000002",
  type: "work",
  code: "TEST-001",
  title: "示例作品资料：春日档案",
  subtitle: "2026-09-10 · 示例厂牌",
  href: "/works/test-001-00000002",
  image_url: null,
  image: null,
};

const MOCK_PERFORMER_ITEM = {
  id: "20000000-0000-4000-8000-000000000001",
  type: "performer",
  title: "测试人物",
  subtitle: "Test Person · 测试所属",
  href: "/performers/test-jsonld-00000001",
  image_url: null,
  image: null,
};

const MOCK_USER = { id: "40000000-0000-4000-8000-000000000001", email: "browser-user@example.test", role: "user" };
const MOCK_OPERATOR = { id: "40000000-0000-4000-8000-000000000002", email: "operator@example.test", role: "admin" };
const MOCK_REVIEWER = { id: "40000000-0000-4000-8000-000000000003", email: "reviewer@example.test", role: "admin" };
const MOCK_OWNER = { id: "40000000-0000-4000-8000-000000000004", email: "owner@example.test", role: "owner" };
const MOCK_INVITED_ADMIN = { id: "40000000-0000-4000-8000-000000000005", email: "invited-admin@example.test", role: "admin" };
const MOCK_EDITOR = { id: "40000000-0000-4000-8000-000000000006", email: "editor@example.test", role: "editor" };
const MOCK_OPERATION_ENTITY_ID = "60000000-0000-4000-8000-000000000001";
const MOCK_OPERATION_REVISION_ID = "70000000-0000-4000-8000-000000000001";
const MOCK_OPERATION_TASK_ID = "80000000-0000-4000-8000-000000000001";

function mockWorkItemByID(id) {
  return [MOCK_WORK_ITEM, MOCK_PREVIEW_WORK_ITEM].find((item) => item.id === id) ?? null;
}

export function mockUsageState() {
  const now = new Date(Math.floor(Date.now() / 1000) * 1000);
  return { config_digest: "a".repeat(64), period_start: new Date(now.getTime() - 3600000).toISOString(), period_end: new Date(now.getTime() + 86400000).toISOString(),
    updated_at: now.toISOString(), last_success_at: now.toISOString(), last_until: now.toISOString(), class_a_highwater: "100", class_b_highwater: "200",
    review_required: true, review_reason: "usage_unavailable", stop_recommended: false, last_status: "low_estimate", active_run: false, last_review_id: null, observation_fresh: true };
}

export function mockUploadState() {
  const now = new Date(Math.floor(Date.now() / 1000) * 1000 - 1000).toISOString().replace(".000Z", ".123456Z");
  const id = "91000000-0000-4000-8000-000000000001";
  return { target_ref: "d".repeat(64), receipt: { status: "observed", epoch: id, generation: 1, mode: "paused", command_id: id,
    applied_at: new Date(Date.now() - 60000).toISOString().replace(/\.\d{3}Z$/, "Z") }, last_success_at: now, updated_at: now, error_code: null, fresh: true };
}

export async function startMockUpstream(port, { accountMode = "unavailable", logoutControl = { mode: "success", attempts: 0, redirects: 0 }, mediaControl = { mode: "success", primary: null, shared: false }, inspectionControl = { mode: "healthy" }, usageControl = { mode: "healthy", state: mockUsageState(), reviews: [], requests: [] }, uploadControl = { mode: "healthy", state: mockUploadState(), commands: [], requests: [] } } = {}) {
  // In-memory fault control only; no production route or HTTP control endpoint.
  const revokedSessions = new Set();
  function mockSession(request) {
    const token = rawMockSession(request);
    return revokedSessions.has(token) ? "" : token;
  }
  const accountState = {
    favoriteWorkID: null,
    followed: false,
    hiddenWorkID: null,
    history: [],
    feedback: [],
  };
  const operationsState = {
    work: null,
    taskStatus: null,
    assignee: null,
    publishedSlug: null,
    reauthenticated: new Set(),
  };
  const invitationState = { invitation: null, nextID: 1 };
  const userManagementState = { role: "user", status: "active" };
  const takedownState = { request: null, workHidden: false };
  const server = http.createServer(async (request, response) => {
    const url = new URL(request.url ?? "/", `http://${request.headers.host ?? "127.0.0.1"}`);
    if (accountMode === "stateful" && url.pathname === "/__test/reset-operations" && request.method === "POST") {
      operationsState.work = null;
      operationsState.taskStatus = null;
      operationsState.assignee = null;
      operationsState.publishedSlug = null;
      operationsState.reauthenticated.clear();
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/__test/operations" && request.method === "GET") {
      return writeMockJSON(response, {
        has_work: operationsState.work !== null,
        task_status: operationsState.taskStatus,
        assignee: operationsState.assignee,
        published_slug: operationsState.publishedSlug,
      });
    }
    if (accountMode === "stateful" && url.pathname === "/__test/reset-invitations" && request.method === "POST") {
      invitationState.invitation = null;
      invitationState.nextID = 1;
      operationsState.reauthenticated.clear();
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/__test/reset-user-management" && request.method === "POST") {
      userManagementState.role = "user";
      userManagementState.status = "active";
      operationsState.reauthenticated.clear();
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/__test/reset-feedback" && request.method === "POST") {
      accountState.feedback = [];
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/__test/reset-takedowns" && request.method === "POST") {
      takedownState.request = null;
      takedownState.workHidden = false;
      operationsState.reauthenticated.clear();
      return writeMockNoContent(response);
    }
    if (url.pathname === "/api/v1/site/home") {
      return writeMockJSON(response, {
        generated_at: "2026-09-08T00:00:00Z",
        sections: [
          { key: "discovery", title: "发现", items: [MOCK_PREVIEW_WORK_ITEM, MOCK_PERFORMER_ITEM] },
          { key: "latest", title: "最新发行", items: [MOCK_PREVIEW_WORK_ITEM] },
          { key: "editorial", title: "编辑推荐", items: [] },
          { key: "performers", title: "人物资料", items: [MOCK_PERFORMER_ITEM] },
          { key: "trending", title: "近期热门", items: [] },
          { key: "most_viewed", title: "浏览最多", items: [] },
        ],
      });
    }
    if (url.pathname === "/api/v1/search/works") {
      const query = url.searchParams.get("q") ?? "";
      const normalizedQuery = query.replace(/[\s_-]+/g, "").toLowerCase();
      const matches = !takedownState.workHidden && normalizedQuery === "testjsonld";
      const previewMatches = normalizedQuery === "test001";
      const publishedItem = mockPublishedWorkItem(operationsState);
      const publishedMatches = publishedItem && normalizedQuery === "ops100";
      const items = previewMatches ? [MOCK_PREVIEW_WORK_ITEM] : matches ? [MOCK_WORK_ITEM] : publishedMatches ? [publishedItem] : [];
      return writeMockJSON(response, { query, items, next_cursor: null });
    }
    if (url.pathname === "/api/v1/works/test-001-00000002") {
      return writeMockJSON(response, {
        id: MOCK_PREVIEW_WORK_ITEM.id,
        code: MOCK_PREVIEW_WORK_ITEM.code,
        title: MOCK_PREVIEW_WORK_ITEM.title,
        title_original: null,
        release_date: "2026-09-10",
        studio_name: "示例厂牌",
        summary: "用于本地测试站预览的正常合成作品资料，不对应任何真实作品。",
        performers: [{ id: MOCK_PERFORMER_ITEM.id, name: MOCK_PERFORMER_ITEM.title, href: MOCK_PERFORMER_ITEM.href, image_url: null, image: null }],
        images: [],
        related_works: [],
      });
    }
    if (url.pathname === "/api/v1/works/test-jsonld-00000001") {
      if (takedownState.workHidden) return writeMockError(response, "NOT_FOUND", "未找到已发布作品", 404);
      return writeMockJSON(response, {
        id: MOCK_WORK_ITEM.id,
        code: MOCK_WORK_ITEM.code,
        title: MOCK_WORK_ITEM.title,
        title_original: null,
        release_date: "2026-09-08",
        studio_name: "测试厂牌",
        summary: "仅用于本地 HTTP 冒烟的合成资料。",
        performers: [{ id: "20000000-0000-4000-8000-000000000001", name: "测试人物", href: "/performers/test-jsonld-00000001", image_url: null, image: null }],
        images: [],
        related_works: [],
      });
    }
    if (url.pathname === "/api/v1/performers/test-jsonld-00000001") {
      return writeMockJSON(response, {
        id: "20000000-0000-4000-8000-000000000001",
        name: "测试人物",
        name_original: null,
        romanized_name: "Test Person",
        aliases: ["测试别名"],
        adult_status: "verified",
        activity_status: "active",
        agency: "测试所属",
        birth_year: null,
        height_cm: 165,
        measurements: null,
        debut_year: 2026,
        image_url: null,
        images: [],
        works: [],
      });
    }
    if (operationsState.publishedSlug && url.pathname === `/api/v1/works/${operationsState.publishedSlug}`) {
      const work = operationsState.work;
      return writeMockJSON(response, {
        id: MOCK_OPERATION_ENTITY_ID,
        code: work?.code ?? "OPS-100",
        title: work?.title ?? "运营录入作品",
        title_original: null,
        release_date: work?.release_date ?? "2026-09-09",
        studio_name: null,
        summary: work?.summary ?? "浏览器运营闭环合成资料。",
        performers: [],
        images: [],
        related_works: [],
      });
    }
    if (url.pathname === "/api/v1/studios/test-jsonld-00000001") {
      return writeMockJSON(response, {
        id: "30000000-0000-4000-8000-000000000001",
        name: "测试厂牌",
        works: [],
      });
    }
    if (url.pathname === "/api/v1/metrics/page-view" && request.method === "POST") {
      const body = await readMockJSON(request);
      if (accountMode === "stateful" && mockSession(request) === "browser-user" && body?.content_type && body?.content_id) {
        const item = body.content_type === "performer" ? MOCK_PERFORMER_ITEM : mockWorkItemByID(body.content_id) ?? MOCK_WORK_ITEM;
        accountState.history.unshift({ item, viewed_at: "2026-09-09T12:34:56Z" });
      }
      response.writeHead(204, { "cache-control": "no-store" });
      response.end();
      return undefined;
    }
    if (accountMode !== "stateful" && (url.pathname === "/api/v1/me" || url.pathname.startsWith("/admin/"))) {
      request.socket.destroy();
      return undefined;
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/signup/code" && request.method === "POST") {
      return writeMockJSON(response, { message: "验证码已发送，请检查测试邮箱" }, 202);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/signup/verify" && request.method === "POST") {
      const body = await readMockJSON(request);
      if (body?.code !== "123456" || typeof body?.password !== "string" || body.password.length < 12) {
        return writeMockError(response, "INVALID_CODE", "验证码或密码不符合要求", 400);
      }
      revokedSessions.delete("browser-user");
      return writeMockJSON(response, { user: MOCK_USER }, 201, { "set-cookie": sessionCookie("browser-user") });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/password/code" && request.method === "POST") {
      return writeMockJSON(response, { message: "验证码已发送，请检查测试邮箱" }, 202);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/password/reset" && request.method === "POST") {
      const body = await readMockJSON(request);
      if (body?.code !== "123456" || typeof body?.password !== "string" || body.password.length < 12) {
        return writeMockError(response, "INVALID_CODE", "验证码或密码不符合要求", 400);
      }
      return writeMockJSON(response, { message: "密码已重置，请使用新密码登录" }, 200, { "set-cookie": sessionCookie("", 0) });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/invitations/accept" && request.method === "POST") {
      const body = await readMockJSON(request);
      const invitation = invitationState.invitation;
      if (!invitation || invitation.status !== "pending" || body?.email !== invitation.email || body?.code !== "123456"
        || typeof body?.password !== "string" || body.password.length < 12) {
        return writeMockError(response, "INVALID_INVITATION", "邀请验证码无效或已失效", 400);
      }
      invitation.status = "accepted";
      invitation.consumed_at = "2026-09-10T09:10:00Z";
      revokedSessions.delete("browser-invited-admin");
      return writeMockJSON(response, { user: MOCK_INVITED_ADMIN }, 201, { "set-cookie": sessionCookie("browser-invited-admin") });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/login" && request.method === "POST") {
      const body = await readMockJSON(request);
      const operatorLogin = body?.email === MOCK_OPERATOR.email && body?.password === "Release-A-operator-pass";
      const reviewerLogin = body?.email === MOCK_REVIEWER.email && body?.password === "Release-A-reviewer-pass";
      const ownerLogin = body?.email === MOCK_OWNER.email && body?.password === "Release-A-owner-pass";
      const editorLogin = body?.email === MOCK_EDITOR.email && body?.password === "Release-A-editor-pass";
      const userLogin = body?.email === MOCK_USER.email && body?.password === "Release-A-browser-pass";
      if (!operatorLogin && !reviewerLogin && !ownerLogin && !editorLogin && !userLogin) {
        return writeMockError(response, "INVALID_CREDENTIALS", "邮箱或密码错误", 401);
      }
      const user = ownerLogin ? MOCK_OWNER : reviewerLogin ? MOCK_REVIEWER : editorLogin ? MOCK_EDITOR : userLogin ? MOCK_USER : MOCK_OPERATOR;
      const session = ownerLogin ? "browser-owner" : reviewerLogin ? "browser-reviewer" : editorLogin ? "browser-editor" : userLogin ? "browser-user" : "browser-operator";
      revokedSessions.delete(session);
      return writeMockJSON(response, { user }, 200, { "set-cookie": sessionCookie(session) });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/reauth" && request.method === "POST") {
      const session = mockSession(request);
      const body = await readMockJSON(request);
      const valid = (session === "browser-operator" && body?.password === "Release-A-operator-pass")
        || (session === "browser-reviewer" && body?.password === "Release-A-reviewer-pass")
        || (session === "browser-owner" && body?.password === "Release-A-owner-pass")
        || (session === "browser-invited-admin" && body?.password === "Release-A-invited-pass");
      if (!valid) return writeMockError(response, "INVALID_CREDENTIALS", "当前密码错误", 401);
      operationsState.reauthenticated.add(session);
      return writeMockJSON(response, { message: "近期密码确认完成" }, 200, { "set-cookie": "sd_reauth=mock; Path=/; HttpOnly; SameSite=Lax" });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/auth/logout" && request.method === "POST") {
      logoutControl.attempts += 1;
      if (logoutControl.mode === "unavailable") return writeMockError(response, "LOGOUT_UNAVAILABLE", "退出尚未确认", 503);
      if (logoutControl.mode === "disconnect") { request.socket.destroy(); return undefined; }
      if (logoutControl.mode === "timeout") return undefined; // Client deadline must end the request.
      if (logoutControl.mode === "html") {
        response.writeHead(200, { "content-type": "text/html" });
        response.end("<p>upstream maintenance</p>");
        return undefined;
      }
      if (logoutControl.mode === "json") return writeMockJSON(response, { ok: true }, 200);
      if (logoutControl.mode === "accepted") return writeMockJSON(response, { queued: true }, 202);
      if (logoutControl.mode === "redirect") {
        response.writeHead(302, { location: "/__test/logout-redirect" });
        response.end();
        return undefined;
      }
      revokedSessions.add(rawMockSession(request));
      operationsState.reauthenticated.delete(rawMockSession(request));
      response.writeHead(204, { "cache-control": "no-store", "set-cookie": sessionCookie("", 0) });
      response.end();
      return undefined;
    }
    if (accountMode === "stateful" && url.pathname === "/__test/logout-redirect") {
      logoutControl.redirects += 1;
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me") {
      const session = mockSession(request);
      if (session === "browser-user") return writeMockJSON(response, { user: MOCK_USER });
      if (session === "browser-operator") return writeMockJSON(response, { user: MOCK_OPERATOR });
      if (session === "browser-reviewer") return writeMockJSON(response, { user: MOCK_REVIEWER });
      if (session === "browser-owner") return writeMockJSON(response, { user: MOCK_OWNER });
      if (session === "browser-invited-admin") return writeMockJSON(response, { user: MOCK_INVITED_ADMIN });
      if (session === "browser-editor") return writeMockJSON(response, { user: MOCK_EDITOR });
      return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/favorites" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const item = mockWorkItemByID(accountState.favoriteWorkID);
      return writeMockJSON(response, { items: item ? [item] : [], next_cursor: null });
    }
    if (accountMode === "stateful" && url.pathname.startsWith("/api/v1/me/favorites/")) {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const workID = decodeURIComponent(url.pathname.slice("/api/v1/me/favorites/".length));
      if (!mockWorkItemByID(workID)) return writeMockError(response, "WORK_NOT_FOUND", "未找到该作品资料", 404);
      if (request.method === "POST") accountState.favoriteWorkID = workID;
      else if (request.method === "DELETE") accountState.favoriteWorkID = accountState.favoriteWorkID === workID ? null : accountState.favoriteWorkID;
      else return writeMockError(response, "METHOD_NOT_ALLOWED", "不支持的请求方法", 405);
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/follows" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      return writeMockJSON(response, { items: accountState.followed ? [MOCK_PERFORMER_ITEM] : [], next_cursor: null });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/followed-works" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      return writeMockJSON(response, {
        items: accountState.followed && accountState.hiddenWorkID !== MOCK_PREVIEW_WORK_ITEM.id ? [MOCK_PREVIEW_WORK_ITEM] : [],
        next_cursor: null,
      });
    }
    if (accountMode === "stateful" && url.pathname === `/api/v1/me/follows/${MOCK_PERFORMER_ITEM.id}`) {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      if (request.method === "POST") accountState.followed = true;
      else if (request.method === "DELETE") accountState.followed = false;
      else return writeMockError(response, "METHOD_NOT_ALLOWED", "不支持的请求方法", 405);
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/hidden-works" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const item = mockWorkItemByID(accountState.hiddenWorkID);
      return writeMockJSON(response, { items: item ? [item] : [], next_cursor: null });
    }
    if (accountMode === "stateful" && url.pathname.startsWith("/api/v1/me/hidden-works/")) {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const workID = decodeURIComponent(url.pathname.slice("/api/v1/me/hidden-works/".length));
      if (!mockWorkItemByID(workID)) return writeMockError(response, "WORK_NOT_FOUND", "未找到该作品资料", 404);
      if (request.method === "POST") accountState.hiddenWorkID = workID;
      else if (request.method === "DELETE") accountState.hiddenWorkID = accountState.hiddenWorkID === workID ? null : accountState.hiddenWorkID;
      else return writeMockError(response, "METHOD_NOT_ALLOWED", "不支持的请求方法", 405);
      return writeMockNoContent(response);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/history" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      return writeMockJSON(response, { items: accountState.history });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/history" && request.method === "DELETE") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const hiddenCount = accountState.history.length;
      accountState.history = [];
      return writeMockJSON(response, { hidden_count: hiddenCount });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/feedback" && request.method === "POST") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const body = await readMockJSON(request);
      if (typeof body?.message !== "string" || !body.message.trim()) {
        return writeMockError(response, "VALIDATION_ERROR", "请填写反馈说明", 400);
      }
      const item = {
        content_type: body.content_type ?? null,
        content_id: body.content_id ?? null,
        feedback_type: body.feedback_type ?? "other",
        message: body.message,
        evidence_url: body.evidence_url ?? null,
        id: "50000000-0000-4000-8000-000000000001",
        review_status: "pending",
        submitter: MOCK_USER.email,
        reviewer: null,
        reviewed_at: null,
        created_at: "2026-09-09T12:35:00Z",
      };
      accountState.feedback.unshift(item);
      return writeMockJSON(response, item, 201);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/me/feedback" && request.method === "GET") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      return writeMockJSON(response, { items: accountState.feedback });
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/account/close/code" && request.method === "POST") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      return writeMockJSON(response, { message: "关闭账号验证码已发送" }, 202);
    }
    if (accountMode === "stateful" && url.pathname === "/api/v1/account/close" && request.method === "POST") {
      if (mockSession(request) !== "browser-user") return writeMockError(response, "UNAUTHENTICATED", "请先登录", 401);
      const body = await readMockJSON(request);
      if (body?.code !== "123456" || body?.notice_version !== "2026-08-07") {
        return writeMockError(response, "INVALID_CODE", "验证码或关闭说明版本无效", 400);
      }
      return writeMockJSON(response, { message: "账号已关闭" }, 200, { "set-cookie": sessionCookie("", 0) });
    }
    if (accountMode === "stateful" && url.pathname.startsWith("/admin/")) {
      const session = mockSession(request);
      if (!["browser-operator", "browser-reviewer", "browser-owner", "browser-invited-admin", "browser-editor"].includes(session)) {
        return writeMockError(response, "FORBIDDEN", "无权访问运营后台", 403);
      }
      if (url.pathname.startsWith("/admin/v1/media/")) {
        if (session === "browser-editor") return writeMockError(response, "FORBIDDEN", "当前账号无权管理图片", 403);
        if (url.pathname === "/admin/v1/media/upload-control" && request.method === "GET") {
          if (uploadControl.mode === "read-failure") return writeMockError(response, "OPERATIONS_UNAVAILABLE", "上传状态不可读", 503);
          return writeMockJSON(response, { state: uploadControl.mode === "uninitialized" ? null : uploadControl.mode === "partial" ? { fresh: true } : uploadControl.state,
            commands: uploadControl.commands.map(({ input: _input, ...item }) => item) });
        }
        if (url.pathname === "/admin/v1/media/upload-control/commands" && request.method === "POST") {
          if (!operationsState.reauthenticated.has(session)) return writeMockError(response, "REAUTH_REQUIRED", "需要近期密码验证", 401);
          const body = await readMockJSON(request);
          uploadControl.requests.push(structuredClone(body));
          const actor = session === "browser-owner" ? MOCK_OWNER : session === "browser-reviewer" ? MOCK_REVIEWER : session === "browser-invited-admin" ? MOCK_INVITED_ADMIN : MOCK_OPERATOR;
          const prior = uploadControl.commands.find((c) => c.actor_id === actor.id && c.input.idempotency_key === body.idempotency_key);
          if (prior) {
            if (JSON.stringify(prior.input) !== JSON.stringify(body)) return writeMockError(response, "CONFLICT", "幂等载荷不一致", 409);
            const { input: _input, ...item } = prior;
            return writeMockJSON(response, { command: { ...item, idempotent_replay: true } });
          }
          if (uploadControl.mode === "conflict" || uploadControl.commands.some((c) => ["pending", "running", "uncertain"].includes(c.status)) ||
            !body.confirmed || body.target_ref !== uploadControl.state?.target_ref || body.expected_epoch !== uploadControl.state.receipt.epoch ||
            body.expected_generation !== uploadControl.state.receipt.generation || body.expected_observed_at !== uploadControl.state.last_success_at) return writeMockError(response, "CONFLICT", "快照或状态已变化", 409);
          const now = Date.now();
          const item = { command_id: randomUUID(), actor_id: actor.id, mode: body.mode, reason: body.reason, status: "pending", created_at: new Date(now).toISOString(),
            expires_at: new Date(Math.floor(now / 1000) * 1000 + 300000).toISOString(), first_dispatched_at: null, completed_at: null, dispatch_count: 0, error_code: null, receipt: null, idempotent_replay: false };
          uploadControl.commands.unshift({ ...item, input: structuredClone(body) });
          if (uploadControl.mode === "uncertain") return writeMockJSON(response, { command: { status: "pending" } }, 202);
          if (uploadControl.mode === "lost-response") { response.destroy(); return; }
          return writeMockJSON(response, { command: item }, 202);
        }
        if (url.pathname === "/admin/v1/media/usage" && request.method === "GET") {
          if (usageControl.mode === "read-failure") return writeMockError(response, "OPERATIONS_UNAVAILABLE", "用量状态暂时无法读取", 503);
          return writeMockJSON(response, { state: usageControl.mode === "uninitialized" ? null : usageControl.mode === "partial" ? { review_required: false } : usageControl.state,
            reviews: usageControl.reviews.map(({ input: _input, ...item }) => item), enforcement: "not_connected" });
        }
        if (url.pathname === "/admin/v1/media/usage/reviews" && request.method === "POST") {
          if (!operationsState.reauthenticated.has(session)) return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
          const body = await readMockJSON(request);
          usageControl.requests.push(structuredClone(body));
          if (usageControl.mode === "conflict") return writeMockError(response, "OPERATIONS_CONFLICT", "状态已变化", 409);
          const prior = usageControl.reviews.find((item) => item.input.idempotency_key === body.idempotency_key);
          if (prior) {
            if (JSON.stringify(prior.input) !== JSON.stringify(body)) return writeMockError(response, "OPERATIONS_CONFLICT", "请求编号不能复用不同内容", 409);
            const { input: _input, ...item } = prior;
            return writeMockJSON(response, { review: { ...item, idempotent_replay: true }, enforcement: "not_connected" });
          }
          const now = new Date();
          const item = { review_id: body.idempotency_key, action: body.action, status: "pending", created_at: now.toISOString(), expires_at: new Date(now.getTime() + 300000).toISOString(),
            completed_at: null, error_code: null, reason: body.reason, next_period_start: body.next_period_start ?? null, next_period_end: body.next_period_end ?? null, idempotent_replay: false };
          usageControl.reviews.unshift({ ...item, input: structuredClone(body) });
          if (usageControl.mode === "uncertain") return writeMockJSON(response, { review: { status: "pending" }, enforcement: "not_connected" }, 202);
          return writeMockJSON(response, { review: item, enforcement: "not_connected" }, 202);
        }
        if (url.pathname === "/admin/v1/media/primary" && request.method === "GET") {
          if (mediaControl.mode === "read-failure") return writeMockError(response, "OPERATIONS_UNAVAILABLE", "当前主图暂时无法读取", 503);
          return writeMockJSON(response, { entity_type: url.searchParams.get("entity_type"), entity_id: url.searchParams.get("entity_id"), entity_status: "published", primary: mediaControl.primary });
        }
        if (request.method === "POST" && ["/admin/v1/media/manifests", "/admin/v1/media/primary/replace"].includes(url.pathname)) {
          if (!operationsState.reauthenticated.has(session)) return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
          const body = await readMockJSON(request);
          const replacing = url.pathname.endsWith("/replace");
          const manifest = replacing ? body?.manifest : body;
          if (!manifest?.asset_id || manifest?.objects?.length !== 4 || (replacing && (!manifest.is_primary || body.expected_asset_id === manifest.asset_id))) return writeMockError(response, "INVALID_MEDIA_MANIFEST", "清单不完整", 400);
          mediaControl.lastBody = body;
          if (mediaControl.mode === "conflict") {
            mediaControl.primary = { asset_id: "22000000-0000-4000-8000-000000000099", entity_media_id: "33000000-0000-4000-8000-000000000099", purpose: "cover" };
            return writeMockError(response, "STATE_CONFLICT", "主图已变化", 409);
          }
          if (mediaControl.mode === "write-failure") return writeMockError(response, "OPERATIONS_UNAVAILABLE", "提交暂时无法确认", 503);
          if ((replacing && body.expected_asset_id !== mediaControl.primary?.asset_id) || (!replacing && manifest.is_primary && mediaControl.primary)) return writeMockError(response, "STATE_CONFLICT", "主图已变化", 409);
          const media = { asset_id: manifest.asset_id, entity_media_id: "33000000-0000-4000-8000-000000000001", version: 1, object_count: 4, status: "published", created_at: "2026-09-10T12:00:00Z" };
          if (manifest.is_primary) mediaControl.primary = { asset_id: manifest.asset_id, entity_media_id: media.entity_media_id, purpose: manifest.purpose };
          // Simulates a committed mutation with an incomplete response.
          if (mediaControl.mode === "uncertain") return writeMockJSON(response, { accepted: true }, 201);
          return writeMockJSON(response, replacing ? { media, previous_asset_id: body.expected_asset_id, old_asset_retired: !mediaControl.shared, private_retained_until: mediaControl.shared ? null : "2026-10-10T12:00:00Z" } : media, 201);
        }
      }
      if (url.pathname === "/admin/v1/audit-logs" && request.method === "GET") {
        if (session === "browser-editor") return writeMockError(response, "FORBIDDEN", "当前账号无权查询审计记录", 403);
        const requestID = url.searchParams.get("request_id") ?? "";
        if (!/^[A-Za-z0-9._:-]{1,128}$/.test(requestID)) {
          return writeMockError(response, "INVALID_REQUEST_ID", "请输入有效的请求编号", 400);
        }
        const items = requestID === "release-a:publish-42" ? [{
          id: "42",
          actor_type: "user",
          actor_id: MOCK_OPERATOR.id,
          actor: MOCK_OPERATOR.email,
          action: "entity.publish",
          object_type: "work",
          object_id: MOCK_OPERATION_ENTITY_ID,
          reason: "合成发布审计示例",
          request_id: requestID,
          occurred_at: "2026-09-10T14:30:00Z",
        }] : [];
        return writeMockJSON(response, { items, truncated: false });
      }
      if (url.pathname === "/admin/v1/feedback" && request.method === "GET") {
        return writeMockJSON(response, { items: accountState.feedback });
      }
      if (url.pathname === "/admin/v1/feedback/50000000-0000-4000-8000-000000000001/review" && request.method === "POST") {
        const item = accountState.feedback[0];
        if (!item) return writeMockError(response, "NOT_FOUND", "未找到反馈", 404);
        const body = await readMockJSON(request);
        if (typeof body?.reason !== "string" || body.reason.trim().length < 2) {
          return writeMockError(response, "VALIDATION_ERROR", "请填写反馈处理理由", 400);
        }
        if (session === "browser-editor") {
          if (item.review_status !== "pending" || body.status !== "reviewing") {
            return writeMockError(response, "FORBIDDEN", "当前角色不能完成反馈审核", 403);
          }
          item.review_status = "reviewing";
          item.reviewer = MOCK_EDITOR.email;
        } else {
          if (item.review_status !== "reviewing" || !["accepted", "rejected", "closed"].includes(body?.status)) {
            return writeMockError(response, "STATE_CONFLICT", "反馈状态已变化", 409);
          }
          item.review_status = body.status;
          item.reviewer = session === "browser-owner" ? MOCK_OWNER.email : session === "browser-reviewer" ? MOCK_REVIEWER.email : session === "browser-invited-admin" ? MOCK_INVITED_ADMIN.email : MOCK_OPERATOR.email;
        }
        item.reviewed_at = "2026-09-10T10:00:00Z";
        return writeMockJSON(response, item);
      }
      if (url.pathname === "/admin/v1/takedowns" && request.method === "GET") {
        return writeMockJSON(response, { items: takedownState.request ? [takedownState.request] : [] });
      }
      if (url.pathname === "/admin/v1/takedowns" && request.method === "POST") {
        if (session === "browser-editor") {
          return writeMockError(response, "FORBIDDEN", "当前角色不能登记权利下架", 403);
        }
        const body = await readMockJSON(request);
        if (typeof body?.requester_reference !== "string" || !body.requester_reference.trim()
          || !["work", "performer", "studio", "media"].includes(body?.entity_type)
          || typeof body?.entity_id !== "string" || !/^[0-9a-f-]{36}$/i.test(body.entity_id)) {
          return writeMockError(response, "VALIDATION_ERROR", "权利下架登记信息不符合要求", 400);
        }
        takedownState.request = {
          id: "91000000-0000-4000-8000-000000000001",
          requester_reference: body.requester_reference.trim(),
          entity_type: body.entity_type,
          entity_id: body.entity_id,
          evidence_reference: body.evidence_reference ?? null,
          status: "received",
          assigned_to: null,
          received_at: "2026-09-10T11:00:00Z",
          decided_at: null,
          completed_at: null,
          result_summary: null,
          updated_at: "2026-09-10T11:00:00Z",
        };
        return writeMockJSON(response, takedownState.request, 201);
      }
      if (url.pathname === "/admin/v1/takedowns/91000000-0000-4000-8000-000000000001/complete" && request.method === "POST") {
        if (session === "browser-editor") {
          return writeMockError(response, "FORBIDDEN", "当前角色不能执行权利下架", 403);
        }
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        if (!takedownState.request || takedownState.request.status === "completed") {
          return writeMockError(response, "STATE_CONFLICT", "权利下架请求状态已变化", 409);
        }
        if (typeof body?.result_summary !== "string" || body.result_summary.trim().length < 2) {
          return writeMockError(response, "VALIDATION_ERROR", "请填写处理结论", 400);
        }
        takedownState.request.status = "completed";
        takedownState.request.decided_at = "2026-09-10T11:10:00Z";
        takedownState.request.completed_at = "2026-09-10T11:10:00Z";
        takedownState.request.result_summary = body.result_summary.trim();
        takedownState.request.updated_at = "2026-09-10T11:10:00Z";
        if (takedownState.request.entity_type === "work" && takedownState.request.entity_id === MOCK_WORK_ITEM.id) {
          takedownState.workHidden = true;
        }
        return writeMockJSON(response, takedownState.request);
      }
      if (url.pathname === "/admin/v1/works" && request.method === "POST") {
        const body = await readMockJSON(request);
        if (session !== "browser-operator" || typeof body?.code !== "string" || typeof body?.title !== "string" || !body.sources?.length) {
          return writeMockError(response, "VALIDATION_ERROR", "作品资料或来源不完整", 400);
        }
        operationsState.work = body;
        operationsState.taskStatus = "pending";
        operationsState.assignee = null;
        operationsState.publishedSlug = null;
        operationsState.reauthenticated.clear();
        return writeMockJSON(response, {
          id: MOCK_OPERATION_ENTITY_ID,
          entity_type: "work",
          status: "reviewing",
          revision_id: MOCK_OPERATION_REVISION_ID,
          revision_status: "reviewing",
          created_at: "2026-09-09T13:00:00Z",
        }, 201);
      }
      if (url.pathname === "/admin/v1/review-tasks" && request.method === "GET") {
        const status = url.searchParams.get("status") ?? "open";
        const task = mockOperationTask(operationsState);
        const include = task && ((status === "open" && (task.status === "pending" || task.status === "claimed"))
          || (status === "approved" && task.status === "approved"));
        return writeMockJSON(response, { items: include ? [task] : [] });
      }
      if (url.pathname === `/admin/v1/review-tasks/${MOCK_OPERATION_TASK_ID}/claim` && request.method === "POST") {
        if (!operationsState.work || operationsState.taskStatus !== "pending") {
          return writeMockError(response, "STATE_CONFLICT", "审核任务状态已变化", 409);
        }
        operationsState.taskStatus = "claimed";
        operationsState.assignee = session;
        return writeMockJSON(response, mockOperationTask(operationsState));
      }
      if (url.pathname === `/admin/v1/review-tasks/${MOCK_OPERATION_TASK_ID}/approve` && request.method === "POST") {
        if (operationsState.assignee !== session || operationsState.taskStatus !== "claimed") {
          return writeMockError(response, "FORBIDDEN", "当前账号不能审核该任务", 403);
        }
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        if (typeof body?.reason !== "string" || body.reason.trim().length < 2) {
          return writeMockError(response, "VALIDATION_ERROR", "请填写审核理由", 400);
        }
        operationsState.taskStatus = "approved";
        return writeMockJSON(response, mockOperationTask(operationsState));
      }
      if (url.pathname === `/admin/v1/entities/${MOCK_OPERATION_ENTITY_ID}/revisions` && request.method === "GET") {
        return writeMockJSON(response, { items: operationsState.work ? [mockOperationRevision(operationsState)] : [] });
      }
      if (url.pathname === "/admin/v1/users" && request.method === "GET") {
        const managedUser = mockOperationsUser({ ...MOCK_USER, role: userManagementState.role }, userManagementState.status);
        const users = [mockOperationsUser(MOCK_OWNER), mockOperationsUser(MOCK_OPERATOR), mockOperationsUser(MOCK_REVIEWER), mockOperationsUser(MOCK_EDITOR), managedUser];
        if (invitationState.invitation?.status === "accepted") users.push(MOCK_INVITED_ADMIN);
        return writeMockJSON(response, { items: users.map((user) => "account_status" in user ? user : mockOperationsUser(user)) });
      }
      if (url.pathname === `/admin/v1/users/${MOCK_USER.id}/role` && request.method === "POST") {
        if (session !== "browser-owner") {
          return writeMockError(response, "FORBIDDEN", "只有最高管理员可以修改角色", 403);
        }
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        if (!["admin", "editor", "user"].includes(body?.role)) {
          return writeMockError(response, "VALIDATION_ERROR", "角色不符合要求", 400);
        }
        if (userManagementState.role === body.role) {
          return writeMockError(response, "STATE_CONFLICT", "对象状态已变化，请刷新后重试", 409);
        }
        userManagementState.role = body.role;
        return writeMockJSON(response, mockOperationsUser({ ...MOCK_USER, role: userManagementState.role }, userManagementState.status));
      }
      if (url.pathname === `/admin/v1/users/${MOCK_USER.id}/status` && request.method === "POST") {
        const canManage = session === "browser-owner" || (session === "browser-operator" && userManagementState.role === "user");
        if (!canManage) {
          return writeMockError(response, "FORBIDDEN", "当前账号不能管理该用户", 403);
        }
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        if (!["active", "locked", "suspended"].includes(body?.status)
          || typeof body?.reason !== "string" || body.reason.trim().length < 2) {
          return writeMockError(response, "VALIDATION_ERROR", "账号状态参数不符合要求", 400);
        }
        userManagementState.status = body.status;
        return writeMockJSON(response, mockOperationsUser({ ...MOCK_USER, role: userManagementState.role }, userManagementState.status));
      }
      if (url.pathname === "/admin/v1/invitations" && request.method === "GET") {
        return writeMockJSON(response, { items: invitationState.invitation ? [invitationState.invitation] : [] });
      }
      if (url.pathname === "/admin/v1/invitations" && request.method === "POST") {
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        const roleAllowed = session === "browser-owner" ? ["admin", "editor", "user"].includes(body?.role) : body?.role === "user";
        if (!roleAllowed || typeof body?.email !== "string" || typeof body?.reason !== "string" || body.reason.trim().length < 2) {
          return writeMockError(response, "FORBIDDEN", "当前账号不能创建该角色邀请", 403);
        }
        const invitationID = `90000000-0000-4000-8000-${String(invitationState.nextID).padStart(12, "0")}`;
        invitationState.nextID += 1;
        invitationState.invitation = {
          id: invitationID,
          email: body.email,
          role: body.role,
          status: "pending",
          invited_by: session === "browser-owner" ? MOCK_OWNER.id : MOCK_INVITED_ADMIN.id,
          inviter: session === "browser-owner" ? MOCK_OWNER.email : MOCK_INVITED_ADMIN.email,
          sent_at: "2026-09-10T09:00:00Z",
          expires_at: "2026-09-12T09:00:00Z",
          consumed_at: null,
          created_at: "2026-09-10T09:00:00Z",
        };
        return writeMockJSON(response, invitationState.invitation, 201);
      }
      const invitationRevokeMatch = url.pathname.match(/^\/admin\/v1\/invitations\/([^/]+)\/revoke$/);
      if (invitationRevokeMatch && request.method === "POST") {
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        const invitation = invitationState.invitation;
        if (!invitation || invitation.id !== invitationRevokeMatch[1]) {
          return writeMockError(response, "NOT_FOUND", "邀请不存在", 404);
        }
        if (invitation.status !== "pending") {
          return writeMockError(response, "STATE_CONFLICT", "邀请已处理，不能撤销", 409);
        }
        if (typeof body?.reason !== "string" || body.reason.trim().length < 2) {
          return writeMockError(response, "VALIDATION_ERROR", "请填写撤销邀请理由", 400);
        }
        invitation.status = "revoked";
        invitation.consumed_at = "2026-09-10T09:20:00Z";
        return writeMockJSON(response, invitation);
      }
      if (url.pathname === `/admin/v1/entities/${MOCK_OPERATION_ENTITY_ID}/publish` && request.method === "POST") {
        if (operationsState.taskStatus !== "approved") {
          return writeMockError(response, "STATE_CONFLICT", "资料尚未审核通过", 409);
        }
        if (!operationsState.reauthenticated.has(session)) {
          return writeMockError(response, "REAUTH_REQUIRED", "高风险操作需要近期密码确认", 401);
        }
        const body = await readMockJSON(request);
        if (body?.entity_type !== "work" || typeof body?.canonical_slug !== "string") {
          return writeMockError(response, "VALIDATION_ERROR", "发布参数无效", 400);
        }
        operationsState.publishedSlug = body.canonical_slug;
        operationsState.taskStatus = "published";
        return writeMockJSON(response, {
          id: MOCK_OPERATION_ENTITY_ID,
          entity_type: "work",
          canonical_slug: operationsState.publishedSlug,
          status: "published",
          revision_id: MOCK_OPERATION_REVISION_ID,
          revision_status: "approved",
          created_at: "2026-09-09T13:00:00Z",
        });
      }
      if (url.pathname === "/admin/v1/system/health") {
        if (inspectionControl.mode === "unavailable") return writeMockError(response, "OPERATIONS_UNAVAILABLE", "系统检查暂时不可用", 503);
        return writeMockJSON(response, {
          status: "ok", schema_version: 21, pending_review_tasks: 0, reviewing_revisions: 0,
          media_inspection_age_seconds: inspectionControl.mode === "never" ? null : 600,
          media_publication_issues: inspectionControl.mode === "issues" ? 3 : 0,
          default_image_failures: inspectionControl.mode === "issues" ? 2 : 0,
          failed_media_inspections: inspectionControl.mode === "issues" ? 1 : 0,
          pending_outbox_events: 0, failed_outbox_events: 0, media_bytes: 0,
          verified_backup_age_seconds: null, media_reconcile_age_seconds: null,
          media_reconcile_issues: 0, failed_media_reconciles: 0,
          search_requests_24h: 1, search_zero_results_24h: 0, checked_at: "2026-09-08T00:00:00Z",
        });
      }
    }
    return writeMockJSON(response, { error: { code: "NOT_FOUND", message: "mock route not found" } }, 404);
  });
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", resolve);
  });
  return server;
}

async function readMockJSON(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  if (chunks.length === 0) return null;
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    return null;
  }
}

function rawMockSession(request) {
  const match = (request.headers.cookie ?? "").match(/(?:^|;\s*)sd_session=([^;]+)/);
  return match?.[1] ?? "";
}

function sessionCookie(value, maxAge) {
  return `sd_session=${value}; Path=/; HttpOnly; SameSite=Lax${maxAge === undefined ? "" : `; Max-Age=${maxAge}`}`;
}

function writeMockError(response, code, message, status) {
  return writeMockJSON(response, { error: { code, message, request_id: "browser-e2e" } }, status);
}

function writeMockNoContent(response) {
  response.writeHead(204, { "cache-control": "no-store" });
  response.end();
  return undefined;
}

function mockPublishedWorkItem(operationsState) {
  if (!operationsState.publishedSlug || !operationsState.work) return null;
  return {
    id: MOCK_OPERATION_ENTITY_ID,
    type: "work",
    code: operationsState.work.code,
    title: operationsState.work.title,
    subtitle: `${operationsState.work.release_date ?? "待补充"} · 待补充厂牌`,
    href: `/works/${operationsState.publishedSlug}`,
    image_url: null,
    image: null,
  };
}

function mockOperationTask(operationsState) {
  if (!operationsState.work || !operationsState.taskStatus || operationsState.taskStatus === "published") return null;
  const assigneeUser = operationsState.assignee === "browser-reviewer" ? MOCK_REVIEWER
    : operationsState.assignee === "browser-operator" ? MOCK_OPERATOR : null;
  return {
    id: MOCK_OPERATION_TASK_ID,
    task_type: "new_entity",
    entity_type: "work",
    entity_id: MOCK_OPERATION_ENTITY_ID,
    priority: 100,
    status: operationsState.taskStatus,
    assignee_id: assigneeUser?.id ?? null,
    assignee: assigneeUser?.email ?? null,
    target_label: `${operationsState.work.code} / ${operationsState.work.title}`,
    due_at: null,
    claimed_at: operationsState.assignee ? "2026-09-09T13:05:00Z" : null,
    completed_at: operationsState.taskStatus === "approved" ? "2026-09-09T13:10:00Z" : null,
    created_at: "2026-09-09T13:00:00Z",
    updated_at: operationsState.taskStatus === "approved" ? "2026-09-09T13:10:00Z" : "2026-09-09T13:05:00Z",
  };
}

function mockOperationRevision(operationsState) {
  const approved = operationsState.taskStatus === "approved" || operationsState.taskStatus === "published";
  return {
    id: MOCK_OPERATION_REVISION_ID,
    entity_type: "work",
    entity_id: MOCK_OPERATION_ENTITY_ID,
    version: 1,
    payload: operationsState.work,
    status: approved ? "approved" : "reviewing",
    author: MOCK_OPERATOR.email,
    reviewer: approved ? MOCK_REVIEWER.email : null,
    reason: operationsState.work.reason,
    created_at: "2026-09-09T13:00:00Z",
    reviewed_at: approved ? "2026-09-09T13:10:00Z" : null,
    sources: [{
      field_name: "code,title",
      source_type: operationsState.work.sources[0].source_type,
      source_url: operationsState.work.sources[0].source_url,
      source_title: operationsState.work.sources[0].source_title,
      checked_at: operationsState.work.sources[0].checked_at,
      operator: MOCK_OPERATOR.email,
      confidence: 80,
      rights_status: "allowed",
    }],
    is_current: operationsState.taskStatus === "published",
    canonical_slug: operationsState.publishedSlug,
  };
}

function mockOperationsUser(user, accountStatus = "active") {
  return {
    ...user,
    account_status: accountStatus,
    created_at: "2026-09-01T00:00:00Z",
    last_login_at: "2026-09-09T12:00:00Z",
  };
}

function writeMockJSON(response, body, status = 200, headers = {}) {
  response.writeHead(status, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", ...headers });
  response.end(JSON.stringify(body));
}

function childLogs(entry) {
  return entry.logsSuppressed
    ? `${entry.name} server output is suppressed by the smoke runner.`
    : `${entry.name} server output was written to the current terminal.`;
}

export async function stopChild(entry) {
  const child = entry?.child;
  if (!child || child.exitCode !== null) return;
  try {
    child.kill("SIGTERM");
  } catch {
    // The process may have exited between the state check and the signal.
  }
  await waitForChildExit(child, 1_500);
  if (child.exitCode === null && process.platform === "win32") {
    spawnSync("taskkill.exe", ["/PID", String(child.pid), "/T", "/F"], {
      stdio: "ignore",
      windowsHide: true,
    });
    await waitForChildExit(child, 1_000);
  }
  if (child.exitCode === null) {
    try {
      child.kill("SIGKILL");
    } catch {
      // Best effort; a subsequent process-tree cleanup can handle it.
    }
  }
}

function waitForChildExit(child, timeoutMs) {
  if (child.exitCode !== null) return Promise.resolve();
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, timeoutMs);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

export function startChild({ name, server, port, apiURL, repositoryRoot, stdio = "ignore", extraEnv = {} }) {
  const entry = { name, child: null, port, spawnError: null, logsSuppressed: stdio === "ignore" };
  const child = spawn(
    process.execPath,
    [path.join(repositoryRoot, "scripts", "start-next-standalone.mjs"), server],
    {
      cwd: repositoryRoot,
      env: {
        ...process.env,
        ...extraEnv,
        NODE_ENV: "production",
        NEXT_TELEMETRY_DISABLED: "1",
        HOSTNAME: "127.0.0.1",
        PORT: String(port),
        PLATFORM_API_URL: apiURL,
      },
      // Do not retain IPC/stdout handles: Next may leave a descendant alive
      // briefly on Windows, and inherited pipes would keep this smoke runner
      // open after the HTTP assertions have completed.
      stdio,
      windowsHide: true,
    },
  );
  entry.child = child;
  child.once("error", (error) => {
    entry.spawnError = error;
  });
  return entry;
}

export async function waitForHTTP(entry, pathName, timeoutMs) {
  const url = `http://127.0.0.1:${entry.port}${pathName}`;
  const deadline = Date.now() + timeoutMs;
  let lastError = "连接尚未就绪";
  while (Date.now() < deadline) {
    if (entry.spawnError) {
      throw new SmokeFailure(`${entry.name} 无法启动：${entry.spawnError.message}`);
    }
    if (entry.child.exitCode !== null) {
      throw new SmokeFailure(`${entry.name} 在就绪前退出（${entry.child.exitCode}）`, childLogs(entry));
    }
    try {
      const response = await fetch(url, { redirect: "manual", signal: AbortSignal.timeout(REQUEST_TIMEOUT_MS) });
      const body = await response.text();
      if (entry.child.exitCode !== null) {
        throw new SmokeFailure(`${entry.name} 在返回就绪响应后退出（${entry.child.exitCode}）`, childLogs(entry));
      }
      return { status: response.status, headers: response.headers, body, url };
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
    }
  }
  throw new SmokeFailure(`${entry.name} ${pathName} 在 ${timeoutMs}ms 内没有响应：${lastError}`, childLogs(entry));
}

async function request(entry, pathName, timeoutMs) {
  const url = `http://127.0.0.1:${entry.port}${pathName}`;
  try {
    const response = await fetch(url, {
      redirect: "manual",
      signal: AbortSignal.timeout(Math.min(REQUEST_TIMEOUT_MS, timeoutMs)),
    });
    return { status: response.status, headers: response.headers, body: await response.text(), url };
  } catch (error) {
    throw new SmokeFailure(`${entry.name} ${pathName} 请求失败：${error instanceof Error ? error.message : String(error)}`);
  }
}

function throwFailures(failures) {
  if (failures.length > 0) throw new SmokeFailure("Release A 前端 HTTP 冒烟失败", failures.join("\n"));
}

export async function runSmoke({ build = false, timeoutMs = DEFAULT_TIMEOUT_MS, repositoryRoot = REPOSITORY_ROOT } = {}) {
  const paths = await ensureArtifacts({ build, repositoryRoot });
  const stagedAssets = [];
  let upstream;
  let display;
  let misconfiguredDisplay;
  let ops;
  let cleaned = false;
  const cleanup = async () => {
    if (cleaned) return;
    cleaned = true;
    if (upstream) await new Promise((resolve) => upstream.close(() => resolve()));
    await Promise.all([display, misconfiguredDisplay, ops].filter(Boolean).map((entry) => stopChild(entry)));
    await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
  };
  const handleSignal = (exitCode) => {
    void cleanup().finally(() => process.exit(exitCode));
  };
  const handleInterrupt = () => handleSignal(130);
  const handleTermination = () => handleSignal(143);
  process.once("SIGINT", handleInterrupt);
  process.once("SIGTERM", handleTermination);

  try {
    stagedAssets.push(...await prepareStandaloneAssets(paths.display));
    stagedAssets.push(...await prepareStandaloneAssets(paths.ops));
    const ports = new Set();
    const displayPort = await findFreePort({ avoid: ports });
    ports.add(displayPort);
    const opsPort = await findFreePort({ avoid: ports });
    ports.add(opsPort);
    const apiPort = await findFreePort({ avoid: ports });
    ports.add(apiPort);
    upstream = await startMockUpstream(apiPort);
    const apiURL = `http://127.0.0.1:${apiPort}`;
    const displayOrigin = `http://127.0.0.1:${displayPort}`;
    display = startChild({
      name: "display-web",
      server: paths.display.server,
      port: displayPort,
      apiURL,
      repositoryRoot,
      extraEnv: { SITE_URL: displayOrigin, NEXT_PUBLIC_SITE_URL: displayOrigin },
    });
    ops = startChild({ name: "ops-web", server: paths.ops.server, port: opsPort, apiURL, repositoryRoot });
    const [home, opsLogin] = await Promise.all([
      waitForHTTP(display, "/", timeoutMs),
      waitForHTTP(ops, "/login", timeoutMs),
    ]);
    const [login, loginRepeat, register, robots, sitemap, defaultImage, displayProxy, opsProxy, opsAdminProxy, workDetail, performerDetail, studioDetail, opsLoginRepeat] = await Promise.all([
      request(display, "/login", timeoutMs),
      request(display, "/login", timeoutMs),
      request(display, "/register", timeoutMs),
      request(display, "/robots.txt", timeoutMs),
      request(display, "/sitemap.xml", timeoutMs),
      request(display, "/default-work.svg", timeoutMs),
      request(display, "/api/v1/me", timeoutMs),
      request(ops, "/api/v1/me", timeoutMs),
      request(ops, "/admin/v1/review-tasks?status=open", timeoutMs),
      request(display, "/works/test-jsonld-00000001", timeoutMs),
      request(display, "/performers/test-jsonld-00000001", timeoutMs),
      request(display, "/studios/test-jsonld-00000001", timeoutMs),
      request(ops, "/login", timeoutMs),
    ]);

    const displayStylesheets = stylesheetPaths(home.body);
    const opsStylesheets = stylesheetPaths(opsLogin.body);
    const [displayStylesheet, opsStylesheet] = await Promise.all([
      displayStylesheets.length > 0 ? request(display, displayStylesheets[0], timeoutMs) : null,
      opsStylesheets.length > 0 ? request(ops, opsStylesheets[0], timeoutMs) : null,
    ]);

    const failures = [
      ...pageFailures("公开首页", home, {
        includes: ["按番号找到作品公开资料", "资料和图片来自经审核的公开来源", "仅限 18 岁以上用户", "示例作品资料：春日档案"],
      }),
      ...pageFailures("公开登录入口", login, { includes: ["登录", "/register"] }),
      ...pageFailures("公开注册入口", register, { includes: ["注册账号", "邮箱验证"] }),
      ...pageFailures("robots", robots, { includes: [`${displayOrigin}/sitemap.xml`] }),
      ...pageFailures("sitemap index", sitemap, { includes: [`${displayOrigin}/sitemaps/pages`] }),
      ...pageFailures("运营后台登录页", opsLogin, { includes: ["后台登录", "运营后台"] }),
      ...securityHeaderFailures("公开首页", home, { allowTurnstile: true }),
      ...securityHeaderFailures("公开登录入口", login, { allowTurnstile: true, requireScriptNonce: true }),
      ...securityHeaderFailures("公开注册入口", register, { allowTurnstile: true, requireScriptNonce: true }),
      ...nonceRotationFailures("公开登录入口", login, loginRepeat),
      ...securityHeaderFailures("运营后台登录页", opsLogin, { requireScriptNonce: true }),
      ...nonceRotationFailures("运营后台登录页", opsLogin, opsLoginRepeat),
      ...pageFailures("默认作品图", defaultImage, { includes: ["默认作品图", "作品资料默认占位图"] }),
      ...proxyFailures("公开站 API proxy", displayProxy),
      ...proxyFailures("运营站 API proxy", opsProxy),
      ...proxyFailures("运营站 admin proxy", opsAdminProxy),
      ...jsonLDFailures("作品详情 JSON-LD", workDetail, "CreativeWork"),
      ...jsonLDFailures("人物详情 JSON-LD", performerDetail, "Person"),
      ...jsonLDFailures("厂牌详情 JSON-LD", studioDetail, "Organization"),
    ];
    if (!displayStylesheet) failures.push("公开站: missing stylesheet link");
    else failures.push(...stylesheetFailures("公开站 CSS", displayStylesheet));
    if (!opsStylesheet) failures.push("运营站: missing stylesheet link");
    else failures.push(...stylesheetFailures("运营站 CSS", opsStylesheet));
    if (defaultImage.headers.get("content-type")?.toLowerCase().split(";", 1)[0] !== "image/svg+xml") {
      failures.push("默认作品图: expected image/svg+xml content type");
    }
    if (countOccurrences(home.body, 'data-ad-state="placeholder"') < 1) {
      failures.push("公开首页: missing ad placeholder signal");
    }
    if (countOccurrences(home.body, 'data-ad-placement="rail"') < 2) {
      failures.push("公开首页: expected left and right rail ad placeholders");
    }
    const forbiddenAdSignals = presentSignals(home.body, RELEASE_A_FORBIDDEN_AD_SIGNALS);
    if (forbiddenAdSignals.length > 0) {
      failures.push(`公开首页: Release A 禁止加载或启用广告行为: ${forbiddenAdSignals.join(", ")}`);
    }
    if (!home.body.includes("/default-work.svg")) failures.push("公开首页: missing default image signal");
    if (home.body.includes("当前展示本地合成数据") || home.body.includes("目录服务暂时不可用")) {
      failures.push("公开首页: healthy mock API must not render a build-time fallback state");
    }
    throwFailures(failures);

    const misconfiguredPort = await findFreePort({ avoid: ports });
    misconfiguredDisplay = startChild({
      name: "display-web-missing-site-url",
      server: paths.display.server,
      port: misconfiguredPort,
      apiURL,
      repositoryRoot,
      extraEnv: { SITE_URL: "", NEXT_PUBLIC_SITE_URL: "https://build-time.invalid" },
    });
    const misconfiguredRobots = await waitForHTTP(misconfiguredDisplay, "/robots.txt", timeoutMs);
    const configurationFailures = [];
    if (misconfiguredRobots.status !== 500) {
      configurationFailures.push(`robots misconfiguration: expected HTTP 500, got ${misconfiguredRobots.status}`);
    }
    if (misconfiguredRobots.body.includes("127.0.0.1:3000") || misconfiguredRobots.body.includes("build-time.invalid")) {
      configurationFailures.push("robots misconfiguration: response exposed a fallback or build-time site origin");
    }
    throwFailures(configurationFailures);

    return {
      status: "passed",
      displayURL: `http://127.0.0.1:${displayPort}/`,
      opsURL: `http://127.0.0.1:${opsPort}/login`,
      checks: ["首页", "登录/注册入口", "页脚免责声明与 18+", "广告占位、Release A 广告边界与默认图", "三类详情 JSON-LD 与脚本注入转义", "运行时 robots/sitemap 站点身份与误配置关闭", "两站安全响应头与 CSS 资源", "API proxy 503/no-store"],
    };
  } finally {
    process.off("SIGINT", handleInterrupt);
    process.off("SIGTERM", handleTermination);
    await cleanup();
  }
}

export async function main(argv = process.argv.slice(2)) {
  const options = parseArgs(argv);
  if (options.help) {
    console.log(helpText());
    return 0;
  }
  try {
    const result = await runSmoke({ ...options, timeoutMs: Number(process.env.RELEASE_A_SMOKE_TIMEOUT_MS) || options.timeoutMs });
    console.log(JSON.stringify(result, null, 2));
    return 0;
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    return 1;
  }
}

const entryPoint = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : "";
if (import.meta.url === entryPoint) process.exitCode = await main();
