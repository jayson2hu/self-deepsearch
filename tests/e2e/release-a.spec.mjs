import { expect, test } from "@playwright/test";
import { promises as fs } from "node:fs";
import { mediaManifest } from "./fixtures/media-manifest.mjs";
import { createIsolatedStandalone, removeIsolatedRuntime } from "../../scripts/release_a_cache_contract.mjs";

import {
  ensureArtifacts,
  findFreePort,
  prepareStandaloneAssets,
  REPOSITORY_ROOT,
  startChild,
  startMockUpstream,
  stopChild,
  waitForHTTP,
  mockUsageState,
  mockUploadState,
} from "../../scripts/release_a_frontend_smoke.mjs";

let display;
let displayRuntime;
let ops;
let upstream;
let displayURL;
let opsURL;
let apiURL;
const stagedAssets = [];
const logoutControl = { mode: "success", attempts: 0, redirects: 0 };
const mediaControl = { mode: "success", primary: null, shared: false, lastBody: null };
const inspectionControl = { mode: "healthy" };
const usageControl = { mode: "healthy", state: null, reviews: [], requests: [] };
const uploadControl = { mode: "healthy", state: null, commands: [], requests: [] };

test.beforeAll(async () => {
  process.env.APP_ENV = "test";
  process.env.TURNSTILE_BYPASS = "true";

  const paths = await ensureArtifacts({ repositoryRoot: REPOSITORY_ROOT });
  displayRuntime = await createIsolatedStandalone(paths.display);
  stagedAssets.push(...await prepareStandaloneAssets(paths.ops));

  const ports = new Set();
  const displayPort = await findFreePort({ avoid: ports });
  ports.add(displayPort);
  const opsPort = await findFreePort({ avoid: ports });
  ports.add(opsPort);
  const apiPort = await findFreePort({ avoid: ports });

  process.env.SITE_URL = `http://127.0.0.1:${displayPort}`;
  upstream = await startMockUpstream(apiPort, { accountMode: "stateful", logoutControl, mediaControl, inspectionControl, usageControl, uploadControl });
  apiURL = `http://127.0.0.1:${apiPort}`;
  display = startChild({ name: "display-web-e2e", server: displayRuntime.server, port: displayPort, apiURL, repositoryRoot: REPOSITORY_ROOT });
  ops = startChild({ name: "ops-web-e2e", server: paths.ops.server, port: opsPort, apiURL, repositoryRoot: REPOSITORY_ROOT });
  await Promise.all([waitForHTTP(display, "/", 30_000), waitForHTTP(ops, "/login", 30_000)]);
  displayURL = `http://127.0.0.1:${displayPort}`;
  opsURL = `http://127.0.0.1:${opsPort}`;
});

test.afterAll(async () => {
  await Promise.all([stopChild(display), stopChild(ops)]);
  if (upstream) {
    upstream.closeAllConnections?.();
    await new Promise((resolve) => upstream.close(() => resolve()));
  }
  await Promise.all(stagedAssets.map((target) => fs.rm(target, { recursive: true, force: true })));
  if (displayRuntime) await removeIsolatedRuntime(displayRuntime.runtimeRoot);
});

async function registerBrowserUser(page) {
  await page.goto(`${displayURL}/register`);
  await page.getByLabel("邮箱", { exact: true }).fill("browser-user@example.test");
  await page.getByRole("button", { name: "发送验证码" }).click();
  await expect(page.getByRole("status")).toContainText("验证码已发送");
  await page.getByLabel("邮箱验证码").fill("123456");
  await page.getByLabel("密码", { exact: true }).fill("Release-A-browser-pass");
  await page.getByLabel("确认密码").fill("Release-A-browser-pass");
  await page.getByRole("button", { name: "完成注册" }).click();
  await expect(page).toHaveURL(`${displayURL}/`);
}

test("每次进入详情页各上报一次，快速返回不会被时间窗口吞掉", async ({ page }) => {
  const reports = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin === displayURL && url.pathname === "/api/v1/metrics/page-view" && request.method() === "POST") {
      reports.push(request.postDataJSON());
    }
  });

  await page.goto(`${displayURL}/works/test-jsonld-00000001`);
  await expect(page.getByRole("heading", { name: /安全结构化数据/ })).toBeVisible();
  await expect.poll(() => reports.length).toBe(1);
  expect(reports[0]).toEqual({ content_type: "work", content_id: "10000000-0000-4000-8000-000000000001" });

  // Freeze the clock so a time-window deduper would deterministically drop
  // the real return visit even if the local machine or CI is slow.
  await page.evaluate(() => {
    const fixedNow = Date.now();
    Date.now = () => fixedNow;
  });
  await page.getByRole("link", { name: "测试人物", exact: true }).click();
  await expect(page).toHaveURL(`${displayURL}/performers/test-jsonld-00000001`);
  await expect.poll(() => reports.length).toBe(2);
  expect(reports[1]).toEqual({ content_type: "performer", content_id: "20000000-0000-4000-8000-000000000001" });

  await page.goBack();
  await expect(page).toHaveURL(`${displayURL}/works/test-jsonld-00000001`);
  await expect(page.getByRole("heading", { name: /安全结构化数据/ })).toBeVisible();
  await expect.poll(() => reports.length).toBe(3);
  expect(reports[2]).toEqual(reports[0]);
});

test("公开账号页面使用独立 CSP nonce，同时保留目录 ISR 策略", async ({ page }) => {
  const policyErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /content security policy|refused to (?:execute|load)/i.test(message.text())) {
      policyErrors.push(message.text());
    }
  });

  const firstResponse = await page.goto(`${displayURL}/login`);
  const firstPolicy = await firstResponse?.headerValue("content-security-policy");
  const firstNonce = firstPolicy?.match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1];
  expect(firstNonce).toBeTruthy();
  expect(firstPolicy).toContain("'strict-dynamic'");
  expect(firstPolicy).toContain("script-src-attr 'none'");
  expect(firstPolicy).not.toMatch(/script-src[^;]*'unsafe-inline'/);
  expect(await firstResponse?.headerValue("cache-control")).toContain("no-store");
  const scriptNonces = await page.locator("script").evaluateAll((nodes) => nodes
    .filter((node) => {
      const type = (node.getAttribute("type") ?? "").toLowerCase();
      return !type || type === "module" || type === "text/javascript" || type === "application/javascript";
    })
    .map((node) => node.nonce));
  expect(scriptNonces.length).toBeGreaterThan(0);
  expect(scriptNonces.every((nonce) => nonce === firstNonce)).toBe(true);

  const secondResponse = await page.reload();
  const secondPolicy = await secondResponse?.headerValue("content-security-policy");
  const secondNonce = secondPolicy?.match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1];
  expect(secondNonce).toBeTruthy();
  expect(secondNonce).not.toBe(firstNonce);
  await expect(page.getByLabel("邮箱")).toBeVisible();

  const homeResponse = await page.goto(displayURL);
  const homePolicy = await homeResponse?.headerValue("content-security-policy");
  expect(homePolicy).not.toContain("'nonce-");
  expect(homePolicy).toMatch(/script-src[^;]*'unsafe-inline'/);
  expect(homePolicy).toContain("script-src-attr 'none'");
  expect(policyErrors).toEqual([]);
});

test("运营后台每次响应使用独立 CSP nonce 且脚本正常执行", async ({ page }) => {
  const policyErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /content security policy|refused to (?:execute|load)/i.test(message.text())) {
      policyErrors.push(message.text());
    }
  });

  const firstResponse = await page.goto(`${opsURL}/login`);
  const firstPolicy = await firstResponse?.headerValue("content-security-policy");
  const firstNonce = firstPolicy?.match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1];
  expect(firstNonce).toBeTruthy();
  expect(firstPolicy).toContain("'strict-dynamic'");
  expect(firstPolicy).toContain("script-src-attr 'none'");
  expect(firstPolicy).not.toMatch(/script-src[^;]*'unsafe-inline'/);
  expect(await firstResponse?.headerValue("cache-control")).toContain("no-store");
  const scriptNonces = await page.locator("script").evaluateAll((nodes) => nodes
    .filter((node) => {
      const type = (node.getAttribute("type") ?? "").toLowerCase();
      return !type || type === "module" || type === "text/javascript" || type === "application/javascript";
    })
    .map((node) => node.nonce));
  expect(scriptNonces.length).toBeGreaterThan(0);
  expect(scriptNonces.every((nonce) => nonce === firstNonce)).toBe(true);

  const secondResponse = await page.reload();
  const secondPolicy = await secondResponse?.headerValue("content-security-policy");
  const secondNonce = secondPolicy?.match(/'nonce-([A-Za-z0-9_-]{22,128})'/)?.[1];
  expect(secondNonce).toBeTruthy();
  expect(secondNonce).not.toBe(firstNonce);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("wrong-password");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.locator(".auth-form .form-error")).toContainText("邮箱或密码错误");
  expect(policyErrors).toEqual([]);
});

test("每日图片检查区分未检查、异常、不可用与恢复", async ({ page }, testInfo) => {
  const login = await page.request.post(`${opsURL}/api/v1/auth/login`, { headers: { Origin: opsURL }, data: { email: "operator@example.test", password: "Release-A-operator-pass" } });
  expect(login.status()).toBe(200);
  try {
    for (const mode of ["never", "issues", "unavailable", "healthy"]) {
      inspectionControl.mode = mode;
      await page.goto(opsURL);
      const card = page.locator(".metric-grid article").filter({ has: page.getByText("每日图片检查", { exact: true }) });
      await expect(card).toBeVisible();
      if (mode === "never") {
        await expect(card).toContainText("尚未完成检查，不代表图片正常");
        await expect(card.locator(".metric-icon")).toHaveClass(/red/);
      } else if (mode === "issues") {
        await expect(card).toContainText("发布状态问题 3 项；默认图失败 2/2");
        await expect(card).toContainText("24 小时内检查任务失败 1 次");
        await expect(card.locator(".metric-icon")).toHaveClass(/red/);
        await page.screenshot({ path: testInfo.outputPath("daily-image-inspection.png"), fullPage: true });
      } else if (mode === "unavailable") {
        await expect(card).toContainText("当前无法读取状态");
        await expect(card).not.toContainText("默认图失败 0/2");
      } else {
        await expect(card).toContainText("发布状态问题 0 项；默认图失败 0/2");
        await expect(card.locator(".metric-icon")).toHaveClass(/green/);
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
    }
  } finally {
    inspectionControl.mode = "healthy";
  }
});

test("匿名用户可以按番号搜索并安全查看作品详情", async ({ page }, testInfo) => {
  let dialogs = 0;
  page.on("dialog", async (dialog) => {
    dialogs += 1;
    await dialog.dismiss();
  });

  await page.goto(displayURL);
  await expect(page.getByRole("heading", { name: "按番号找到作品公开资料" })).toBeVisible();
  await expect(page.getByText("登录后查看关注人物的新作品")).toBeVisible();
  await expect(page.getByText("关注动态暂时无法加载")).toHaveCount(0);
  await expect(page.locator('[data-ad-state="placeholder"]')).toHaveCount(5);
  await expect(page.getByText("18+ · 仅展示公开资料，不提供资源")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const rails = page.locator(".ad-rail");
  if (testInfo.project.name === "mobile") {
    await expect(rails.first()).toBeHidden();
    await expect(rails.last()).toBeHidden();
  } else {
    await expect(rails.first()).toBeVisible();
    await expect(rails.last()).toBeVisible();
    const readableRailText = await rails.locator(".ad-slot").evaluateAll((slots) => slots.every((slot) => {
      const boxes = Array.from(slot.children, (child) => ({
        rect: child.getBoundingClientRect(),
        fontSize: Number.parseFloat(getComputedStyle(child).fontSize),
      }));
      return boxes.every(({ rect, fontSize }) => rect.width > 0 && rect.height >= fontSize)
        && boxes.every(({ rect }, index) => boxes.slice(index + 1).every(({ rect: other }) => (
          rect.right <= other.left || other.right <= rect.left || rect.bottom <= other.top || other.bottom <= rect.top
        )));
    }));
    expect(readableRailText).toBe(true);
  }

  await page.getByLabel("搜索番号、标题或人物").fill("TEST-001");
  await page.getByRole("button", { name: "查询" }).click();
  await expect(page).toHaveURL(/\/search\?q=TEST-001$/);
  await expect(page.getByRole("link", { name: "查看 TEST-001" })).toBeVisible();
  await page.getByRole("link", { name: "查看 TEST-001" }).click();
  await expect(page.getByRole("heading", { name: "示例作品资料：春日档案" })).toBeVisible();

  await page.goto(displayURL);
  await page.getByLabel("搜索番号、标题或人物").fill("TEST-JSONLD");
  await page.getByRole("button", { name: "查询" }).click();
  await expect(page).toHaveURL(/\/search\?q=TEST-JSONLD$/);
  await expect(page.getByRole("heading", { name: "“TEST-JSONLD” 的结果" })).toBeVisible();
  await page.getByRole("link", { name: "查看 TEST-JSONLD" }).click();

  await expect(page).toHaveURL(/\/works\/test-jsonld-00000001$/);
  await expect(page.getByRole("heading", { name: "安全结构化数据 </script><script>alert(1)</script>" })).toBeVisible();
  const jsonLD = await page.locator('script[type="application/ld+json"]').evaluate((node) => JSON.parse(node.textContent ?? "{}"));
  expect(jsonLD["@type"]).toBe("CreativeWork");
  expect(jsonLD.identifier).toBe("TEST-JSONLD");
  expect(jsonLD.mainEntityOfPage).toBe(`${displayURL}/works/test-jsonld-00000001`);
  await expect(page.locator('script:not([type="application/ld+json"])').filter({ hasText: /^alert\(1\)$/ })).toHaveCount(0);
  await expect.poll(() => dialogs).toBe(0);
  await expect(page.locator('img[alt="TEST-JSONLD 作品图"]')).toHaveJSProperty("complete", true);
});

test("注册验证码流程建立会话，并可退出登录", async ({ page }) => {
  await registerBrowserUser(page);
  await page.getByText("我的账号").click();
  await expect(page.getByText("browser-user@example.test")).toBeVisible();
  await page.getByRole("button", { name: "退出登录" }).click();
  await expect.poll(async () => (await page.context().cookies(displayURL)).some((cookie) => cookie.name === "sd_session")).toBe(false);
  await page.goto(displayURL);
  await expect(page.getByRole("banner").getByRole("link", { name: "登录" })).toBeVisible();
});

test("公开站和后台退出失败保留重试，确认后才清除会话", async ({ page }, testInfo) => {
  try {
    for (const site of ["display", "ops"]) {
      const base = site === "display" ? displayURL : opsURL;
      const email = site === "display" ? "browser-user@example.test" : "operator@example.test";
      const password = site === "display" ? "Release-A-browser-pass" : "Release-A-operator-pass";
      const login = await page.request.post(`${base}/api/v1/auth/login`, { headers: { Origin: base }, data: { email, password } });
      expect(login.status()).toBe(200);
      const token = (await page.context().cookies(base)).find((cookie) => cookie.name === "sd_session")?.value;
      expect(token).toBeTruthy();
      await page.context().addCookies([{ name: "sd_reauth", value: "logout-test", url: base, httpOnly: true, sameSite: "Lax" }]);
      await page.goto(base);
      if (site === "display") await page.getByText("我的账号").click();

      logoutControl.mode = "unavailable";
      await page.getByRole("button", { name: "退出登录", exact: true }).click();
      await expect(page).toHaveURL(`${base}/logout?error=unconfirmed`);
      await expect(page.getByRole("main").getByRole("alert")).toContainText("退出尚未确认");
      await expect(page.getByText("请重试退出；关闭页面不代表已退出登录。", { exact: true })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
      const retained = await page.context().cookies(base);
      expect(retained.find((cookie) => cookie.name === "sd_session")?.value).toBe(token);
      expect(retained.some((cookie) => cookie.name === "sd_reauth")).toBe(true);
      expect((await page.request.get(`${apiURL}/api/v1/me`, { headers: { Cookie: `sd_session=${token}` } })).status()).toBe(200);
      await page.screenshot({ path: testInfo.outputPath(`${site}-logout-unconfirmed.png`), fullPage: true });

      logoutControl.mode = "success";
      await page.getByRole("button", { name: "重试退出", exact: true }).click();
      await expect(page).toHaveURL(site === "display" ? `${base}/` : `${base}/login`);
      expect((await page.context().cookies(base)).filter((cookie) => ["sd_session", "sd_reauth"].includes(cookie.name))).toHaveLength(0);
      expect((await page.request.get(`${apiURL}/api/v1/me`, { headers: { Cookie: `sd_session=${token}` } })).status()).toBe(401);
    }
  } finally {
    logoutControl.mode = "success";
  }
});

test("两个退出入口拒绝假成功回执、重定向和无界等待", async ({ request }) => {
  try {
    for (const base of [displayURL, opsURL]) {
      for (const mode of ["unavailable", "html", "json", "accepted", "redirect", "disconnect", "timeout"]) {
        logoutControl.mode = mode;
        const redirectsBefore = logoutControl.redirects;
        const start = Date.now();
        const result = await request.post(`${base}/auth/logout`, {
          headers: { Origin: base, Cookie: "sd_session=logout-contract; sd_reauth=logout-test" },
          maxRedirects: 0, timeout: 6000,
        });
        expect(result.status(), mode).toBe(303);
        expect(result.headers().location, mode).toBe(`${base}/logout?error=unconfirmed`);
        expect(result.headers()["set-cookie"], mode).toBeUndefined();
        expect(result.headers()["cache-control"], mode).toContain("no-store");
        expect(logoutControl.redirects).toBe(redirectsBefore);
        if (mode === "timeout") expect(Date.now() - start).toBeLessThan(5500);
      }
    }
  } finally {
    logoutControl.mode = "success";
  }
});

test("两个退出入口拒绝跨站请求，无会话退出无需依赖 API", async ({ request }) => {
  for (const base of [displayURL, opsURL]) {
    for (const origin of [undefined, "null", "https://untrusted.example", `${base}/unexpected-path`]) {
      const before = logoutControl.attempts;
      const headers = { Cookie: "sd_session=logout-contract" };
      if (origin !== undefined) headers.Origin = origin;
      const response = await request.post(`${base}/auth/logout`, { headers, maxRedirects: 0 });
      expect(response.status()).toBe(403);
      expect(response.headers()["set-cookie"]).toBeUndefined();
      expect(response.headers()["cache-control"]).toContain("no-store");
      expect(logoutControl.attempts).toBe(before);
    }
    const before = logoutControl.attempts;
    const absent = await request.post(`${base}/auth/logout`, { headers: {
      Origin: base, Cookie: "sd_reauth=orphan", "X-Forwarded-Host": "untrusted.example",
    }, maxRedirects: 0 });
    expect(absent.status()).toBe(303);
    expect(absent.headers().location).toBe(base === displayURL ? `${base}/` : `${base}/login`);
    expect(absent.headers()["set-cookie"]).toContain("sd_session=;");
    expect(absent.headers()["set-cookie"]).toContain("sd_reauth=;");
    for (const site of ["cross-site", "same-site", "none"]) {
      const rejected = await request.post(`${base}/auth/logout`, { headers: {
        Origin: "null", "Sec-Fetch-Site": site, "Sec-Fetch-Mode": "navigate", Cookie: "sd_session=logout-contract",
      }, maxRedirects: 0 });
      expect(rejected.status()).toBe(403);
    }
    const privateForm = await request.post(`${base}/auth/logout`, { headers: {
      Origin: "null", "Sec-Fetch-Site": "same-origin", "Sec-Fetch-Mode": "navigate", Cookie: "sd_reauth=orphan",
    }, maxRedirects: 0 });
    expect(privateForm.status()).toBe(303);
    expect(privateForm.headers().location).toBe(base === displayURL ? `${base}/` : `${base}/login`);
    const tlsOrigin = "https://site.example.test";
    const proxied = await request.post(`${base}/auth/logout`, { headers: {
      Host: "site.example.test", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "untrusted.example",
      Origin: tlsOrigin, Cookie: "sd_reauth=orphan",
    }, maxRedirects: 0 });
    expect(proxied.status()).toBe(303);
    expect(proxied.headers().location).toBe(base === displayURL ? `${tlsOrigin}/` : `${tlsOrigin}/login`);
    expect(logoutControl.attempts).toBe(before);
    expect((await request.get(`${base}/auth/logout`)).status()).toBe(405);
    const retryPage = await request.get(`${base}/logout`);
    expect(retryPage.status()).toBe(200);
    expect(retryPage.headers()["cache-control"]).toContain("no-store");
  }
});

test("普通账号误入后台时退出失败也必须明确提示", async ({ page }) => {
  try {
    logoutControl.mode = "unavailable";
    await page.goto(`${opsURL}/login`);
    await page.getByLabel("邮箱").fill("browser-user@example.test");
    await page.getByLabel("密码").fill("Release-A-browser-pass");
    await page.getByRole("button", { name: "登录", exact: true }).click();
    await expect(page).toHaveURL(`${opsURL}/logout?error=unconfirmed`);
    await expect(page.getByRole("main").getByRole("alert")).toContainText("退出尚未确认");
    logoutControl.mode = "success";
    await page.getByRole("button", { name: "重试退出", exact: true }).click();
    await expect(page).toHaveURL(`${opsURL}/login`);
    expect((await page.context().cookies(opsURL)).some((cookie) => cookie.name === "sd_session")).toBe(false);
  } finally {
    logoutControl.mode = "success";
  }
});

test("忘记密码必须完成邮箱验证码后才能重置", async ({ page }) => {
  await page.goto(`${displayURL}/forgot-password`);
  await page.getByLabel("邮箱", { exact: true }).fill("browser-user@example.test");
  await page.getByRole("button", { name: "发送验证码" }).click();
  await expect(page.getByRole("status")).toContainText("验证码已发送");

  await page.getByLabel("邮箱验证码").fill("123456");
  await page.getByLabel("新密码", { exact: true }).fill("Release-A-new-browser-pass");
  await page.getByLabel("确认新密码").fill("Release-A-new-browser-pass");
  await page.getByRole("button", { name: "重置密码" }).click();

  await expect(page.getByRole("status")).toContainText("密码已重置");
  await expect(page.getByRole("button", { name: "重置密码" })).toHaveCount(0);
});

test("关闭账号展示保留说明并要求邮箱验证码", async ({ page }) => {
  await registerBrowserUser(page);
  await page.goto(`${displayURL}/account/settings`);
  await expect(page.getByRole("heading", { name: "关闭账号" })).toBeVisible();
  await expect(page.getByText("部分操作记录、审计记录和隐私说明约定的信息会继续保存。", { exact: false })).toBeVisible();
  await page.getByRole("button", { name: "发送关闭验证码" }).click();
  await expect(page.getByRole("status")).toContainText("关闭账号验证码已发送");

  await page.getByLabel("邮箱验证码").fill("123456");
  await page.getByLabel("我已阅读并理解上述保留说明").check();
  await page.getByRole("button", { name: "关闭账号" }).click();

  await expect(page).toHaveURL(`${displayURL}/`);
  await expect.poll(async () => (await page.context().cookies(displayURL)).some((cookie) => cookie.name === "sd_session")).toBe(false);
  await expect(page.getByRole("banner").getByRole("link", { name: "登录" })).toBeVisible();
});

test("注册用户可以收藏关注隐藏、查看历史并提交反馈", async ({ page }) => {
  await registerBrowserUser(page);
  await page.goto(`${displayURL}/works/test-jsonld-00000001`);

  const favorite = page.getByRole("button", { name: "收藏作品" });
  await expect(favorite).toBeEnabled();
  await favorite.click();
  await expect(page.getByRole("button", { name: "已收藏" })).toBeVisible();

  await page.getByRole("button", { name: "纠错与反馈" }).click();
  await page.getByLabel("说明").fill("浏览器端合成资料纠错测试");
  await page.getByRole("button", { name: "提交反馈" }).click();
  await expect(page.getByRole("status")).toContainText("管理员审核后会更新状态");

  await page.getByRole("button", { name: "不感兴趣" }).click();
  await expect(page.getByRole("button", { name: "恢复展示" })).toBeVisible();

  await page.goto(`${displayURL}/performers/test-jsonld-00000001`);
  const follow = page.getByRole("button", { name: "关注人物" });
  await expect(follow).toBeEnabled();
  await follow.click();
  await expect(page.getByRole("button", { name: "已关注" })).toBeVisible();

  await page.goto(`${displayURL}/account/favorites`);
  await expect(page.getByRole("heading", { name: "收藏作品" })).toBeVisible();
  await expect(page.getByRole("link", { name: "查看 TEST-JSONLD" })).toBeVisible();

  await page.goto(`${displayURL}/account/follows`);
  await expect(page.getByRole("heading", { name: "关注人物" })).toBeVisible();
  await expect(page.getByRole("link", { name: "查看 测试人物 的人物资料" })).toBeVisible();

  await page.goto(`${displayURL}/account/hidden`);
  await expect(page.getByRole("heading", { name: "已隐藏作品" })).toBeVisible();
  await expect(page.getByRole("button", { name: "恢复展示" })).toBeVisible();

  await page.goto(`${displayURL}/account/history`);
  await expect(page.getByRole("heading", { name: "浏览历史" })).toBeVisible();
  await expect(page.getByText("清除后这些记录不再向你展示；底层记录会按隐私说明继续保留并可进入长期归档。")).toBeVisible();
  await expect(page.getByRole("link", { name: "查看 TEST-JSONLD" })).toBeVisible();
  await page.getByRole("button", { name: "清除可见历史" }).click();
  await expect(page.getByText("暂无可见浏览历史")).toBeVisible();

  await page.goto(`${displayURL}/account/feedback`);
  await expect(page.getByRole("heading", { name: "我的反馈" })).toBeVisible();
  await expect(page.getByText("浏览器端合成资料纠错测试")).toBeVisible();
  await expect(page.getByText("pending")).toBeVisible();
});

async function openMediaForm(page, { primary = true, mode = "success", shared = false, editor = false } = {}) {
  Object.assign(mediaControl, { mode, shared, lastBody: null, primary: primary ? { asset_id: "22000000-0000-4000-8000-000000000002", entity_media_id: "33000000-0000-4000-8000-000000000002", purpose: "cover" } : null });
  await page.request.post(`${apiURL}/__test/reset-operations`);
  page.on("dialog", (dialog) => dialog.accept(editor ? "Release-A-editor-pass" : "Release-A-operator-pass"));
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill(editor ? "editor@example.test" : "operator@example.test");
  await page.getByLabel("密码").fill(editor ? "Release-A-editor-pass" : "Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/media`);
}

async function loadMediaFile(page) {
  await page.getByLabel("处理器生成的 manifest").setInputFiles({ name: "manifest.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(mediaManifest())) });
  await expect(page.getByRole("heading", { name: "清单核对" })).toBeVisible();
}

test("后台在提交前拒绝主图、用途和展示位置不一致的清单", async ({ page }) => {
  await openMediaForm(page);
  const invalid = mediaManifest();
  Object.assign(invalid, { purpose: "gallery", position: 0, is_primary: false });
  await page.getByLabel("处理器生成的 manifest").setInputFiles({
    name: "invalid-manifest.json",
    mimeType: "application/json",
    buffer: Buffer.from(JSON.stringify(invalid)),
  });
  await expect(page.getByText("主图、用途和展示位置不符合 1 张主图 + 最多 3 张精选图的规则")).toBeVisible();
  await expect(page.getByRole("heading", { name: "清单核对" })).toHaveCount(0);
  expect(mediaControl.lastBody).toBeNull();
});

test("主图替换需要独立确认，成功后准确展示异步删除与母版保留", async ({ page }, testInfo) => {
  await openMediaForm(page);
  await loadMediaFile(page);
  await expect(page.getByText("当前资产：", { exact: false })).toBeVisible();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await expect(page.getByRole("button", { name: "确认替换主图" })).toBeDisabled();
  await page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则").check();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const mediaTable = page.getByRole("region", { name: "图片对象清单，可横向滚动" });
  await expect(mediaTable.locator("th")).toHaveCount(6);
  for (const cell of await mediaTable.locator("th").all()) await expect(cell).toBeVisible();
  expect((await mediaTable.locator("tbody tr").first().boundingBox()).height).toBeLessThan(100);
  await page.screenshot({ path: testInfo.outputPath("media-primary-confirm.png"), fullPage: true });
  await page.getByRole("button", { name: "确认替换主图" }).click();
  await expect(page.getByText("主图已切换", { exact: false })).toBeVisible();
  await expect(page.getByText("实际删除尚未确认。", { exact: false })).toBeVisible();
  expect(mediaControl.lastBody).toEqual({ expected_asset_id: "22000000-0000-4000-8000-000000000002", manifest: mediaManifest() });
  await expect(page.getByRole("heading", { name: "清单核对" })).toHaveCount(0);
});

test("无主图可首次登记，共享旧图替换不宣称删除", async ({ page }) => {
  await openMediaForm(page, { primary: false });
  await loadMediaFile(page);
  await expect(page.getByText("当前没有主图，将登记为第一张主图。", { exact: false })).toBeVisible();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await page.getByRole("button", { name: "核对并发布图片" }).click();
  await expect(page.getByText("图片资产已发布", { exact: false })).toBeVisible();
  mediaControl.shared = true;
  mediaControl.primary.asset_id = "22000000-0000-4000-8000-000000000002";
  await loadMediaFile(page);
  await page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则").check();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await page.getByRole("button", { name: "确认替换主图" }).click();
  await expect(page.getByText("旧图仍有其他公开关联，旧资产及其对象已保留，未安排删除。")).toBeVisible();
});

test("主图冲突保留清单，刷新后必须重新确认，提交失败不误报成功", async ({ page }) => {
  await openMediaForm(page, { mode: "conflict" });
  await loadMediaFile(page);
  await page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则").check();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await page.getByRole("button", { name: "确认替换主图" }).click();
  await expect(page.getByText("不会自动覆盖。", { exact: false })).toBeVisible();
  await expect(page.getByRole("heading", { name: "清单核对" })).toBeVisible();
  mediaControl.mode = "write-failure";
  await page.getByRole("button", { name: "刷新当前主图" }).click();
  await expect(page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则")).not.toBeChecked();
  await expect(page.getByRole("button", { name: "确认替换主图" })).toBeDisabled();
  await page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则").check();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await page.getByRole("button", { name: "确认替换主图" }).click();
  await expect(page.getByText("提交暂时无法确认 清单仍保留。")).toBeVisible();
  await expect(page.getByText("主图已切换", { exact: false })).toHaveCount(0);
});

test("主图读取失败禁止提交，不完整成功响应保留清单并阻止重复写入", async ({ page }) => {
  await openMediaForm(page, { mode: "read-failure" });
  await loadMediaFile(page);
  await expect(page.getByText("当前主图暂时无法读取")).toBeVisible();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await expect(page.getByRole("button", { name: "核对并发布图片" })).toBeDisabled();
  mediaControl.mode = "uncertain";
  await page.getByRole("button", { name: "刷新当前主图" }).click();
  await page.getByLabel("确认替换以上旧主图，并了解旧图保留与删除规则").check();
  await page.getByLabel("已核对来源、展示权利、实体关联和北京副本").check();
  await page.getByRole("button", { name: "确认替换主图" }).click();
  await expect(page.getByText("提交结果未能确认", { exact: false })).toBeVisible();
  await expect(page.getByText("主图已切换", { exact: false })).toHaveCount(0);
  await page.getByRole("button", { name: "刷新当前主图" }).click();
  await expect(page.getByText("该资产已是当前主图", { exact: false })).toBeVisible();
  await expect(page.getByRole("button", { name: "确认替换主图" })).toBeDisabled();
});

test("编辑角色不能打开图片管理或调用主图接口", async ({ page }) => {
  await openMediaForm(page, { editor: true });
  await expect(page).toHaveURL(`${opsURL}/`);
  const result = await page.request.get(`${opsURL}/admin/v1/media/primary?entity_type=work&entity_id=${mediaManifest().entity_id}`);
  expect(result.status()).toBe(403);
});

test("运营账号错误密码被拒绝，正确密码进入后台", async ({ page }) => {
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-wrong-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByText("邮箱或密码错误")).toBeVisible();

  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await expect(page.getByRole("heading", { name: "运营概览" })).toBeVisible();
  await expect(page.getByText("当前支持人工录入、审核和发布。自动采集、广告联盟与头像识别均未启用。")).toBeVisible();
});

test("作品 CSV 错误下载在真实页面链路中中和表格公式", async ({ page }) => {
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);

  await page.route("**/admin/v1/imports/works/preflight", async (route) => {
    expect(route.request().method()).toBe("POST");
    expect(route.request().headers()["content-type"]).toContain("multipart/form-data");
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        total_count: 2,
        valid_count: 1,
        invalid_count: 1,
        file_issues: [{ field: "header", code: "UNKNOWN_HEADER", message: "未知,列" }],
        rows: [
          { row_number: 2, code: "SAFE-001", title: "通过作品", release_date: "2026-09-11", issues: [] },
          {
            row_number: 3,
            code: "=HYPERLINK(\"https://invalid.test\")",
            title: "错误作品",
            release_date: "2026-09-11",
            issues: [{ field: "code", code: "INVALID_CODE", message: "番号\"错误" }],
          },
        ],
      }),
    });
  });

  await page.goto(`${opsURL}/imports`);
  await page.getByLabel("CSV 文件").setInputFiles({
    name: "unsafe-work-import.csv",
    mimeType: "text/csv",
    buffer: Buffer.from("code,title\n=HYPERLINK(\"https://invalid.test\"),错误作品\n", "utf8"),
  });
  await page.getByLabel("导入理由").fill("验证错误文件下载安全边界");
  await page.getByRole("button", { name: "上传并预检" }).click();
  await expect(page.getByRole("heading", { name: "预检结果" })).toBeVisible();

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "下载错误 CSV" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("work-import-errors.csv");
  const downloadPath = await download.path();
  expect(downloadPath).toBeTruthy();
  const csv = await fs.readFile(downloadPath, "utf8");
  expect(csv.startsWith('\ufeff"row_number","code","field","error_code","message"\r\n')).toBe(true);
  expect(csv).toContain('"3","\'=HYPERLINK(""https://invalid.test"")","code","INVALID_CODE","番号""错误"');
  expect(csv).not.toContain("SAFE-001");
});

test("admin 可以按请求编号查询最小披露的审计时间线", async ({ page }) => {
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);

  await page.getByRole("link", { name: "审计查询" }).click();
  await expect(page.getByRole("heading", { name: "审计查询" })).toBeVisible();
  await page.getByLabel("请求编号").fill("release-a:publish-42");
  await page.getByRole("button", { name: "查询审计链路" }).click();
  const auditEntry = page.locator(".audit-timeline article").filter({ hasText: "entity.publish" });
  await expect(auditEntry.getByText("entity.publish", { exact: true })).toBeVisible();
  await expect(auditEntry.getByText("operator@example.test", { exact: true })).toBeVisible();
  await expect(auditEntry.getByText("合成发布审计示例", { exact: true })).toBeVisible();
  await expect(page.getByText("before_version")).toHaveCount(0);
  await expect(page.getByText("after_version")).toHaveCount(0);
});

test("两个运营账号完成人工录入、审核、发布并公开搜索", async ({ page }) => {
  const reset = await page.request.post(`${apiURL}/__test/reset-operations`);
  expect(reset.status()).toBe(204);
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);

  await page.getByRole("main").getByRole("link", { name: "新建作品" }).click();
  await page.getByLabel("作品番号").fill("OPS-100");
  await page.getByLabel("实际发行日期").fill("2026-09-09");
  await page.getByLabel("作品标题").fill("运营录入作品");
  await page.getByLabel("来源标题（可选）").fill("浏览器运营 E2E 合成来源");
  await page.getByLabel("录入理由").fill("验证人工录入审核发布闭环");
  await page.getByRole("button", { name: "保存并提交审核" }).click();
  await expect(page.getByText("作品已进入审核队列")).toBeVisible();
  const createdState = await (await page.request.get(`${apiURL}/__test/operations`)).json();
  expect(createdState).toMatchObject({ has_work: true, task_status: "pending", assignee: null });

  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/login`);
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("reviewer@example.test");
  await page.getByLabel("密码").fill("Release-A-reviewer-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await expect(page.getByText("OPS-100 / 运营录入作品")).toBeVisible();
  const reviewState = await (await page.request.get(`${apiURL}/__test/operations`)).json();
  expect(reviewState).toMatchObject({ has_work: true, task_status: "pending", assignee: null });

  await page.getByRole("button", { name: "领取任务" }).click();
  await expect(page.getByText("审核中", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "查看版本和来源" }).click();
  await expect(page.getByRole("heading", { name: "版本历史" })).toBeVisible();
  await expect(page.getByText("浏览器运营 E2E 合成来源", { exact: true })).toBeVisible();

  const approvalPrompts = ["来源与字段已复核", "Release-A-reviewer-pass"];
  const approvalHandler = async (dialog) => dialog.accept(approvalPrompts.shift() ?? "");
  page.on("dialog", approvalHandler);
  await page.getByRole("button", { name: "审核通过" }).click();
  await expect(page.getByText("已有修订等待发布")).toBeVisible();
  page.off("dialog", approvalHandler);

  await page.goto(opsURL);
  const publishPrompts = ["ops-100-00000001", "审核通过后发布"];
  const publishHandler = async (dialog) => dialog.accept(publishPrompts.shift() ?? "");
  page.on("dialog", publishHandler);
  await page.getByRole("button", { name: "发布" }).click();
  await expect(page.getByText("当前没有已通过待发布资料")).toBeVisible();
  page.off("dialog", publishHandler);

  await page.goto(`${displayURL}/search?q=OPS-100`);
  await expect(page.getByRole("heading", { name: "“OPS-100” 的结果" })).toBeVisible();
  await page.getByRole("link", { name: "查看 OPS-100" }).click();
  await expect(page).toHaveURL(`${displayURL}/works/ops-100-00000001`);
  await expect(page.getByRole("heading", { name: "运营录入作品" })).toBeVisible();
});

test("owner 邀请管理员并由受邀邮箱验证码建立后台账号", async ({ page }) => {
  const reset = await page.request.post(`${apiURL}/__test/reset-invitations`);
  expect(reset.status()).toBe(204);

  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("owner@example.test");
  await page.getByLabel("密码").fill("Release-A-owner-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/users`);
  await expect(page.getByRole("heading", { name: "用户与权限" })).toBeVisible();

  await page.getByLabel("邮箱").fill("invited-admin@example.test");
  await page.locator('select[name="role"]').selectOption("admin");
  await page.getByLabel("邀请理由").fill("邀请新的资料审核管理员");
  const reauthHandler = async (dialog) => dialog.accept("Release-A-owner-pass");
  page.on("dialog", reauthHandler);
  await page.getByRole("button", { name: "发送邀请" }).click();
  await expect(page.getByRole("status")).toContainText("验证码已发送到对应邮箱");
  page.off("dialog", reauthHandler);
  await expect(page.getByText("invited-admin@example.test")).toBeVisible();
  await expect(page.getByText("待接受")).toBeVisible();

  await page.goto(`${displayURL}/invite`);
  await page.getByLabel("邀请邮箱").fill("invited-admin@example.test");
  await page.getByLabel("邮箱验证码").fill("123456");
  await page.getByLabel("设置密码").fill("Release-A-invited-pass");
  await page.getByLabel("确认密码").fill("Release-A-invited-pass");
  await page.getByRole("button", { name: "接受邀请并登录" }).click();
  await expect(page).toHaveURL(`${displayURL}/`);
  await page.getByText("我的账号").click();
  await expect(page.getByText("invited-admin@example.test")).toBeVisible();

  await page.goto(opsURL);
  await expect(page.getByRole("heading", { name: "运营概览" })).toBeVisible();
  await page.goto(`${opsURL}/users`);
  await expect(page.locator('select[name="role"] option[value="admin"]')).toHaveCount(0);
  await expect(page.locator('select[name="role"] option[value="editor"]')).toHaveCount(0);
  await expect(page.locator('select[name="role"] option[value="user"]')).toHaveCount(1);

  await page.getByLabel("邮箱").fill("invited-user@example.test");
  await page.locator('select[name="role"]').selectOption("user");
  await page.getByLabel("邀请理由").fill("验证管理员只能邀请普通用户并可撤销");
  const adminReauthHandler = async (dialog) => dialog.accept("Release-A-invited-pass");
  page.on("dialog", adminReauthHandler);
  await page.getByRole("button", { name: "发送邀请" }).click();
  await expect(page.getByRole("status")).toContainText("验证码已发送到对应邮箱");
  page.off("dialog", adminReauthHandler);
  await expect(page.getByText("invited-user@example.test")).toBeVisible();

  const revokeHandler = async (dialog) => dialog.accept("撤销浏览器测试邀请");
  page.on("dialog", revokeHandler);
  await page.getByRole("button", { name: "撤销" }).click();
  await expect(page.getByRole("status")).toContainText("邀请已撤销");
  page.off("dialog", revokeHandler);
  await expect(page.getByText("已撤销", { exact: true })).toBeVisible();

  await page.goto(`${displayURL}/invite`);
  await page.getByLabel("邀请邮箱").fill("invited-user@example.test");
  await page.getByLabel("邮箱验证码").fill("123456");
  await page.getByLabel("设置密码").fill("Release-A-revoked-pass");
  await page.getByLabel("确认密码").fill("Release-A-revoked-pass");
  await page.getByRole("button", { name: "接受邀请并登录" }).click();
  await expect(page.getByText("邀请验证码无效或已失效", { exact: true })).toBeVisible();
  await expect(page).toHaveURL(`${displayURL}/invite`);
});

test("owner 修改普通用户角色，admin 只能管理普通用户状态", async ({ page }) => {
  const reset = await page.request.post(`${apiURL}/__test/reset-user-management`);
  expect(reset.status()).toBe(204);

  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("owner@example.test");
  await page.getByLabel("密码").fill("Release-A-owner-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/users`);
  await expect(page.getByRole("heading", { name: "用户与权限" })).toBeVisible();

  const managedRow = page.getByRole("row").filter({ hasText: "browser-user@example.test" });
  const roleSelect = managedRow.getByLabel("修改 browser-user@example.test 的角色");
  await expect(page.getByText("修改角色会使该账号在所有设备上的旧登录会话失效，需重新登录后使用新权限。", { exact: true })).toBeVisible();
  const roleRoute = "**/admin/v1/users/*/role";
  await page.route(roleRoute, (route) => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: { code: "OPERATIONS_UNAVAILABLE", message: "运营服务暂时不可用" } }) }));
  await roleSelect.selectOption("editor");
  await expect(page.getByText("运营服务暂时不可用", { exact: true })).toBeVisible();
  await expect(roleSelect).toHaveValue("user");
  await expect(page.getByText("角色已更新，该账号的旧登录会话已失效，需要重新登录。", { exact: true })).toHaveCount(0);
  await page.unroute(roleRoute);
  const ownerReauthHandler = async (dialog) => dialog.accept("Release-A-owner-pass");
  page.on("dialog", ownerReauthHandler);
  await roleSelect.selectOption("editor");
  await expect(roleSelect).toHaveValue("editor");
  await expect(page.getByText("角色已更新，该账号的旧登录会话已失效，需要重新登录。", { exact: true })).toBeVisible();
  const unchanged = await page.request.post(`${opsURL}/admin/v1/users/40000000-0000-4000-8000-000000000001/role`, { data: { role: "editor" } });
  expect(unchanged.status()).toBe(409);
  page.off("dialog", ownerReauthHandler);
  await roleSelect.selectOption("user");
  await expect(roleSelect).toHaveValue("user");

  await page.getByRole("button", { name: "退出登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/login`);
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/users`);
  await expect(page.getByRole("heading", { name: "用户与权限" })).toBeVisible();

  const adminManagedRow = page.getByRole("row").filter({ hasText: "browser-user@example.test" });
  await expect(adminManagedRow.getByLabel("修改 browser-user@example.test 的角色")).toHaveCount(0);
  await expect(page.getByLabel("修改 reviewer@example.test 的账号状态")).toHaveCount(0);

  const statusSelect = adminManagedRow.getByLabel("修改 browser-user@example.test 的账号状态");
  const lockPrompts = ["安全测试锁定", "Release-A-operator-pass"];
  const lockHandler = async (dialog) => dialog.accept(lockPrompts.shift() ?? "");
  page.on("dialog", lockHandler);
  await statusSelect.selectOption("locked");
  await expect(statusSelect).toHaveValue("locked");
  page.off("dialog", lockHandler);

  const restoreHandler = async (dialog) => dialog.accept("恢复测试账号");
  page.on("dialog", restoreHandler);
  await statusSelect.selectOption("active");
  await expect(statusSelect).toHaveValue("active");
  page.off("dialog", restoreHandler);
});

test("用户反馈由 editor 领取并由 admin 完成审核", async ({ page }) => {
  const reset = await page.request.post(`${apiURL}/__test/reset-feedback`);
  expect(reset.status()).toBe(204);

  await registerBrowserUser(page);
  await page.goto(`${displayURL}/works/test-jsonld-00000001`);
  await page.getByRole("button", { name: "纠错与反馈" }).click();
  await page.getByLabel("说明").fill("需要运营审核的浏览器反馈");
  await page.getByRole("button", { name: "提交反馈" }).click();
  await expect(page.getByRole("status")).toContainText("管理员审核后会更新状态");
  await page.getByText("我的账号").click();
  await page.getByRole("button", { name: "退出登录" }).click();

  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("editor@example.test");
  await page.getByLabel("密码").fill("Release-A-editor-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/feedback`);
  await expect(page.getByRole("heading", { name: "反馈审核" })).toBeVisible();
  await expect(page.getByText("需要运营审核的浏览器反馈")).toBeVisible();
  const editorFeedbackCard = page.locator(".feedback-review-list article").filter({ hasText: "需要运营审核的浏览器反馈" });

  const claimHandler = async (dialog) => dialog.accept("开始核对用户反馈");
  page.on("dialog", claimHandler);
  await page.getByRole("button", { name: "开始处理" }).click();
  await expect(page.getByText("处理中", { exact: true })).toBeVisible();
  page.off("dialog", claimHandler);
  await expect(editorFeedbackCard.getByText("处理人", { exact: true }).locator("..").locator("dd")).toContainText("editor@example.test");
  await expect(page.getByRole("button", { name: "接受反馈" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "驳回反馈" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "关闭反馈" })).toHaveCount(0);

  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/feedback`);
  const adminFeedbackCard = page.locator(".feedback-review-list article").filter({ hasText: "需要运营审核的浏览器反馈" });

  const acceptHandler = async (dialog) => dialog.accept("反馈内容属实并纳入资料修订");
  page.on("dialog", acceptHandler);
  await page.getByRole("button", { name: "接受反馈" }).click();
  await expect(page.getByText("已接受", { exact: true })).toBeVisible();
  page.off("dialog", acceptHandler);
  await expect(adminFeedbackCard.getByText("处理人", { exact: true }).locator("..").locator("dd")).toContainText("operator@example.test");

  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto(`${displayURL}/login`);
  await page.getByLabel("邮箱").fill("browser-user@example.test");
  await page.getByLabel("密码").fill("Release-A-browser-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${displayURL}/`);
  await page.goto(`${displayURL}/account/feedback`);
  await expect(page.getByText("需要运营审核的浏览器反馈")).toBeVisible();
  await expect(page.getByText("accepted", { exact: true })).toBeVisible();
});

test("权利下架限制 editor 操作并从公开搜索撤下作品", async ({ page }) => {
  const reset = await page.request.post(`${apiURL}/__test/reset-takedowns`);
  expect(reset.status()).toBe(204);

  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/rights`);
  await expect(page.getByRole("heading", { name: "权利下架" })).toBeVisible();

  await page.getByLabel("请求人/邮件工单标识").fill("rights-ticket@example.test / CASE-100");
  await page.getByLabel("资料或图片资产 ID").fill("10000000-0000-4000-8000-000000000001");
  await page.getByLabel("证据引用（可选，不会自动访问）").fill("https://rights.example.test/case/CASE-100");
  await page.getByRole("button", { name: "登记权利请求" }).click();
  await expect(page.getByText("10000000-0000-4000-8000-000000000001", { exact: true })).toBeVisible();
  await expect(page.getByText("待核验", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("editor@example.test");
  await page.getByLabel("密码").fill("Release-A-editor-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/rights`);
  await expect(page.getByText("10000000-0000-4000-8000-000000000001", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "登记权利请求" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "执行下架" })).toHaveCount(0);

  await page.getByRole("button", { name: "退出登录" }).click();
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill("operator@example.test");
  await page.getByLabel("密码").fill("Release-A-operator-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/rights`);

  const completePrompts = ["权利请求已核验并完成公开撤下", "Release-A-operator-pass"];
  const completeHandler = async (dialog) => dialog.accept(completePrompts.shift() ?? "");
  page.on("dialog", completeHandler);
  await page.getByRole("button", { name: "执行下架" }).click();
  await expect(page.getByText("已完成", { exact: true })).toBeVisible();
  page.off("dialog", completeHandler);

  await page.goto(`${displayURL}/search?q=TEST-JSONLD`);
  await expect(page.getByRole("heading", { name: "“TEST-JSONLD” 的结果" })).toBeVisible();
  await expect(page.getByText("没有找到已发布资料")).toBeVisible();
  await expect(page.getByRole("link", { name: "查看 TEST-JSONLD" })).toHaveCount(0);

  const cleanup = await page.request.post(`${apiURL}/__test/reset-takedowns`);
  expect(cleanup.status()).toBe(204);
});
async function openUsageManager(page, mode = "healthy", editor = false) {
  Object.assign(usageControl, { mode, state: mockUsageState(), reviews: [], requests: [] });
  await page.request.post(`${apiURL}/__test/reset-operations`);
  page.on("dialog", (dialog) => dialog.accept(editor ? "Release-A-editor-pass" : "Release-A-owner-pass"));
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill(editor ? "editor@example.test" : "owner@example.test");
  await page.getByLabel("密码").fill(editor ? "Release-A-editor-pass" : "Release-A-owner-pass");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/media/usage`);
  if (!editor) await expect(page.getByRole("heading", { name: "图片用量复核" })).toBeVisible();
}

test("用量复核请求明确区分待处理和已处理，展示不越界", async ({ page }, testInfo) => {
  await openUsageManager(page);
  await expect(page.getByText("需要人工复核", { exact: false })).toBeVisible();
  await page.getByLabel("复核原因", { exact: true }).fill("核对统计恢复正常 <img src=x onerror=alert(1)>");
  await page.getByLabel("已核对账户与统计", { exact: false }).check();
  await page.getByRole("button", { name: "提交复核请求" }).click();
  await expect(page.getByRole("status")).toContainText("尚未处理，更不代表已恢复图片");
  await expect(page.getByRole("region", { name: "最近复核记录" }).getByText("待处理", { exact: true })).toBeVisible();
  expect(usageControl.requests).toHaveLength(1);
  expect(usageControl.requests[0].expected_updated_at).toBe(usageControl.state.updated_at);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("usage-review-pending.png"), fullPage: true });
  usageControl.reviews[0].status = "applied";
  usageControl.reviews[0].completed_at = new Date().toISOString();
  usageControl.state.review_required = false;
  usageControl.state.review_reason = null;
  usageControl.state.last_review_id = usageControl.reviews[0].review_id;
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await expect(page.getByText("已处理（仅观察状态）", { exact: true })).toBeVisible();
  await expect(page.getByRole("status")).not.toContainText("尚未处理");
  await expect(page.getByRole("button", { name: "提交复核请求" })).toBeDisabled();
});

test("用量复核缺失响应拒绝提交，冲突刷新且未知结果复用同一请求", async ({ page }) => {
  await openUsageManager(page, "partial");
  await expect(page.getByRole("status")).toContainText("响应不完整");
  await expect(page.getByRole("button", { name: "提交复核请求" })).toHaveCount(0);
  usageControl.mode = "healthy";
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await page.getByLabel("复核原因", { exact: true }).fill("复核失败后的重试测试");
  await page.getByLabel("已核对账户与统计", { exact: false }).check();
  usageControl.mode = "conflict";
  await page.getByRole("button", { name: "提交复核请求" }).click();
  await expect(page.getByRole("status")).toContainText("状态已变化");
  usageControl.mode = "healthy";
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await expect(page.getByLabel("已核对账户与统计", { exact: false })).not.toBeChecked();
  await page.getByLabel("已核对账户与统计", { exact: false }).check();
  usageControl.mode = "uncertain";
  await page.getByRole("button", { name: "提交复核请求" }).click();
  await expect(page.getByRole("button", { name: "重试同一请求" })).toBeVisible();
  await expect(page.getByRole("status")).toContainText("提交结果不完整");
  const original = structuredClone(usageControl.requests.at(-1));
  usageControl.mode = "healthy";
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await page.getByRole("button", { name: "重试同一请求" }).click();
  await expect(page.getByRole("status")).toContainText("等待 Worker 校验");
  expect(usageControl.requests.at(-1)).toEqual(original);
  expect(usageControl.reviews).toHaveLength(1);
  await expect(page.getByText("已处理（仅观察状态）", { exact: true })).toHaveCount(0);
});

test("用量复核新账期必须明确填写，处理后仍保持保护", async ({ page }, testInfo) => {
  await openUsageManager(page, "uninitialized");
  await expect(page.getByText("尚未初始化观察状态", { exact: false })).toBeVisible();
  usageControl.mode = "healthy";
  const now = Math.floor(Date.now() / 1000) * 1000;
  Object.assign(usageControl.state, { period_start: new Date(now - 172800000).toISOString(), period_end: new Date(now - 3600000).toISOString(),
    stop_recommended: true, observation_fresh: false, last_status: "stop_recommended", class_a_highwater: "950000" });
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await expect(page.getByRole("button", { name: "提交复核请求" })).toBeDisabled();
  await page.getByLabel("处理类型").selectOption("rotate_period");
  await page.getByLabel("新账期结束", { exact: false }).fill(new Date(now + 82800000).toISOString());
  await page.getByLabel("复核原因", { exact: true }).fill("已核对日本 Worker 账期，保持图片保护");
  await page.getByLabel("已核对账户与统计", { exact: false }).check();
  await page.getByRole("button", { name: "提交复核请求" }).click();
  await expect(page.getByRole("status")).toContainText("等待 Worker 校验");
  const submitted = usageControl.requests[0];
  expect(submitted.action).toBe("rotate_period");
  expect(submitted.next_period_start).toBe(usageControl.state.period_end);
  usageControl.reviews[0].status = "applied";
  usageControl.reviews[0].completed_at = new Date().toISOString();
  Object.assign(usageControl.state, { period_start: submitted.next_period_start, period_end: submitted.next_period_end, config_digest: "b".repeat(64),
    review_required: true, review_reason: "usage_period_rotated", stop_recommended: false, last_status: "unknown", last_success_at: null, last_until: null,
    class_a_highwater: "0", class_b_highwater: "0", last_review_id: usageControl.reviews[0].review_id });
  await page.getByRole("button", { name: "刷新用量状态" }).click();
  await page.getByLabel("处理类型").selectOption("acknowledge");
  await expect(page.getByText("尚无有效统计，不按 0 计算")).toBeVisible();
  await expect(page.getByRole("button", { name: "提交复核请求" })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.getByRole("status")).not.toContainText("尚未处理");
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("usage-period-held.png"), fullPage: true });
});

test("用量复核入口和 API 对编辑角色关闭", async ({ page }) => {
  await openUsageManager(page, "healthy", true);
  await expect(page).toHaveURL(`${opsURL}/`);
  await expect(page.getByRole("link", { name: "用量复核" })).toHaveCount(0);
  const response = await page.request.get(`${apiURL}/admin/v1/media/usage`, { headers: { Cookie: "sd_session=browser-editor" } });
  expect(response.status()).toBe(403);
});

async function openUploadManager(page, mode = "healthy", role = "owner") {
  Object.assign(uploadControl, { mode, state: mockUploadState(), commands: [], requests: [] });
  await page.request.post(`${apiURL}/__test/reset-operations`);
  const password = role === "editor" ? "Release-A-editor-pass" : role === "admin" ? "Release-A-operator-pass" : "Release-A-owner-pass";
  const email = role === "editor" ? "editor@example.test" : role === "admin" ? "operator@example.test" : "owner@example.test";
  page.on("dialog", (dialog) => dialog.accept(password));
  await page.goto(`${opsURL}/login`);
  await page.getByLabel("邮箱").fill(email);
  await page.getByLabel("密码").fill(password);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(`${opsURL}/`);
  await page.goto(`${opsURL}/media/upload-control`);
  if (role !== "editor") await expect(page.getByRole("heading", { name: "图片上传控制" })).toBeVisible();
}

async function confirmUpload(page, mode = "paused", reason = "人工核对后申请暂停") {
  await page.getByLabel("操作类型", { exact: true }).selectOption(mode);
  await page.getByLabel("操作原因", { exact: true }).fill(reason);
  await page.getByRole("checkbox").check();
}

function finishUpload(status = "applied", errorCode = null) {
  const command = uploadControl.commands[0];
  const at = new Date(Math.max(Date.now(), Date.parse(command.created_at) + 1)).toISOString();
  Object.assign(command, { status, completed_at: at, error_code: errorCode });
  if (status === "applied") {
    Object.assign(command, { first_dispatched_at: at, dispatch_count: 1, receipt: { status: "applied", epoch: command.input.expected_epoch || command.command_id,
      generation: command.input.expected_generation + 1, mode: command.mode, command_id: command.command_id, applied_at: at.replace(/\.\d{3}Z$/, "Z") } });
    Object.assign(uploadControl.state, { receipt: { ...command.receipt, status: "observed" }, last_success_at: at, updated_at: at, fresh: true, error_code: null });
  }
}

test("上传控制：暂停受理和真实回执在页面上分开显示", async ({ page }, testInfo) => {
  await openUploadManager(page);
  uploadControl.state.receipt.mode = "enabled";
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("heading", { name: "最近确认：新上传已允许" })).toBeVisible();
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  await confirmUpload(page, "paused", "核对存储容量 <img src=x onerror=alert(1)>");
  const held = Promise.withResolvers();
  const intercepted = Promise.withResolvers();
  await page.route("**/admin/v1/media/upload-control/commands", async (route) => {
    intercepted.resolve();
    await held.promise;
    await route.continue();
  });
  await page.getByRole("button", { name: "提交暂停请求" }).click();
  await intercepted.promise;
  try {
    await expect(page.getByRole("button", { name: "读取或提交中…" })).toBeDisabled();
    await expect(page.getByRole("heading", { name: "保留原请求，先确认结果" })).toHaveCount(0);
    await expect(page.getByRole("status", { includeHidden: true })).toHaveText("");
  } finally { held.resolve(); }
  await expect(page.getByRole("status")).toContainText("请求已受理，尚未确认完成");
  await expect(page.getByText("待处理", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "最近确认：新上传已允许" })).toBeVisible();
  expect(uploadControl.requests).toHaveLength(1);
  expect(uploadControl.requests[0].expected_observed_at).toBe(uploadControl.state.last_success_at);
  expect(uploadControl.requests[0].expected_observed_at).toMatch(/\.123456Z$/);
  expect(uploadControl.commands[0].command_id).not.toBe(uploadControl.requests[0].idempotency_key);
  await expect(page.locator('img[src="x"]')).toHaveCount(0);
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("upload-pending.png"), fullPage: true });
  finishUpload();
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByText("已应用 · 回执确认", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "最近确认：新上传已暂停" })).toBeVisible();
  await expect(page.getByRole("status", { includeHidden: true })).toHaveText("");
  await page.getByText("查看指令与操作者标识", { exact: true }).click();
  await expect(page.getByText(uploadControl.commands[0].actor_id, { exact: true })).toBeVisible();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("upload-confirmed.png"), fullPage: true });
});

test("上传控制：恢复需独立重新确认，拒绝不能显示为已允许", async ({ page }, testInfo) => {
  await openUploadManager(page, "healthy", "admin");
  await confirmUpload(page);
  await page.getByLabel("操作类型", { exact: true }).selectOption("enabled");
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await expect(page.getByRole("button", { name: "提交恢复请求" })).toBeDisabled();
  await expect(page.getByRole("heading", { name: "恢复前请再次核对" })).toBeVisible();
  await page.getByLabel("操作原因", { exact: true }).fill("人工核对用量和未确认上传后申请恢复");
  await page.getByRole("checkbox").focus();
  await page.keyboard.press("Space");
  await expect(page.getByRole("checkbox")).toBeChecked();
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("upload-resume-confirmation.png"), fullPage: true });
  await page.getByRole("button", { name: "提交恢复请求" }).click();
  await expect(page.getByRole("status")).toContainText("请求已受理");
  expect(uploadControl.requests[0].mode).toBe("enabled");
  finishUpload("rejected", "control_resume_guard_denied");
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByText("已拒绝", { exact: true })).toBeVisible();
  await expect(page.getByText("恢复未通过用量准入", { exact: false })).toBeVisible();
  await expect(page.getByRole("heading", { name: "最近确认：新上传已暂停" })).toBeVisible();
  await expect(page.getByText("已应用 · 回执确认", { exact: true })).toHaveCount(0);
});

test("上传控制：丢失响应或不完整回执后保留原请求重试", async ({ page }, testInfo) => {
  await openUploadManager(page, "lost-response");
  await confirmUpload(page);
  await page.getByRole("button", { name: "提交暂停请求" }).click();
  await expect(page.getByRole("heading", { name: "保留原请求，先确认结果" })).toBeVisible();
  await expect(page.getByRole("button", { name: "重试同一上传请求" })).toBeEnabled();
  const original = structuredClone(uploadControl.requests[0]);
  expect(uploadControl.commands).toHaveLength(1);
  uploadControl.mode = "read-failure";
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("alert", { name: "上传控制错误" })).toBeVisible();
  await expect(page.getByText(original.idempotency_key, { exact: true })).toBeVisible();
  uploadControl.mode = "healthy";
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: testInfo.outputPath("upload-unconfirmed.png"), fullPage: true });
  finishUpload();
  await page.getByRole("button", { name: "重试同一上传请求" }).click();
  await expect(page.getByRole("status")).toContainText("已有有效执行回执");
  expect(uploadControl.requests.at(-1)).toEqual(original);
  expect(uploadControl.commands).toHaveLength(1);
  await expect(page.getByRole("button", { name: "重试同一上传请求" })).toHaveCount(0);
  await expect(page.getByText("已应用 · 回执确认", { exact: true })).toBeVisible();
});

test("上传控制：旧快照冲突后必须刷新和重新确认", async ({ page }) => {
  await openUploadManager(page, "conflict");
  await confirmUpload(page);
  await page.getByRole("button", { name: "提交暂停请求" }).click();
  await expect(page.getByRole("alert", { name: "上传控制错误" })).toContainText("状态已变化");
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toHaveCount(0);
  expect(uploadControl.commands).toHaveLength(0);
  uploadControl.mode = "healthy";
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await page.getByRole("checkbox").check();
  await page.getByLabel("操作原因", { exact: true }).fill("重新核对后的暂停");
  await expect(page.getByRole("checkbox")).not.toBeChecked();
  await page.getByRole("checkbox").check();
  uploadControl.mode = "uncertain";
  await page.getByRole("button", { name: "提交暂停请求" }).click();
  await expect(page.getByRole("alert", { name: "上传控制错误" })).toContainText("提交回执不完整或不匹配");
  await expect(page.getByRole("button", { name: "重试同一上传请求" })).toBeVisible();
  uploadControl.mode = "healthy";
  await page.getByRole("button", { name: "重试同一上传请求" }).click();
  await expect(page.getByRole("status")).toContainText("请求已受理，尚未确认完成");
  expect(uploadControl.commands).toHaveLength(1);
});

test("上传控制：未连接、缺失回执和过期状态都不能恢复", async ({ page }) => {
  await openUploadManager(page, "uninitialized");
  await expect(page.getByRole("heading", { name: "尚未连接上传执行器" })).toBeVisible();
  await expect(page.getByRole("button", { name: "提交恢复请求" })).toHaveCount(0);
  uploadControl.mode = "partial";
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("alert", { name: "上传控制错误" })).toContainText("响应不完整");
  uploadControl.mode = "healthy";
  uploadControl.state.receipt = { status: "observed", epoch: "", generation: 0, mode: "paused", command_id: "", applied_at: "" };
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("option", { name: "恢复新上传（需用量准入）" })).toHaveJSProperty("disabled", true);
  await expect(page.getByRole("heading", { name: "控制链尚未初始化" })).toBeVisible();
  for (const delta of [-121000, 60000]) {
    uploadControl.state = mockUploadState();
    uploadControl.state.last_success_at = new Date(Date.now() + delta).toISOString();
    uploadControl.state.updated_at = uploadControl.state.last_success_at;
    await page.getByRole("button", { name: "刷新上传状态" }).click();
    await expect(page.getByText("状态缺失、异常或已过期 · 禁止新操作", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  }
  expect(uploadControl.requests).toHaveLength(0);
});

test("上传控制：停留页面时快照自然到期会禁用提交", async ({ page }) => {
  await openUploadManager(page);
  await page.clock.install();
  const observed = new Date(Date.now() - 115000).toISOString();
  uploadControl.state.last_success_at = observed;
  uploadControl.state.updated_at = observed;
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await confirmUpload(page);
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeEnabled();
  await page.clock.fastForward(8000);
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  expect(uploadControl.requests).toHaveLength(0);
});

test("上传控制：未知和被取代的回执不显示为执行成功", async ({ page }) => {
  await openUploadManager(page);
  await confirmUpload(page);
  await page.getByRole("button", { name: "提交暂停请求" }).click();
  await expect(page.getByRole("status")).toContainText("请求已受理");
  const c = uploadControl.commands[0];
  c.status = "uncertain";
  c.first_dispatched_at = new Date().toISOString();
  c.dispatch_count = 1;
  c.error_code = "control_apply_unconfirmed";
  c.receipt = structuredClone(uploadControl.state.receipt);
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByText("结果待确认", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toBeDisabled();
  c.status = "superseded"; c.completed_at = new Date().toISOString(); c.error_code = "control_snapshot_changed";
  c.receipt.generation += 2;
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByText("已被更新状态取代", { exact: true })).toBeVisible();
  await expect(page.getByText("已应用 · 回执确认", { exact: true })).toHaveCount(0);
  c.status = "applied"; c.error_code = null; c.receipt = null;
  await page.getByRole("button", { name: "刷新上传状态" }).click();
  await expect(page.getByRole("alert", { name: "上传控制错误" })).toContainText("响应不完整");
  await expect(page.getByRole("button", { name: "提交暂停请求" })).toHaveCount(0);
});

test("上传控制：匿名和编辑角色看不到后台控制入口", async ({ page }) => {
  await page.goto(`${opsURL}/media/upload-control`);
  await expect(page).toHaveURL(`${opsURL}/login`);
  await openUploadManager(page, "healthy", "editor");
  await expect(page).toHaveURL(`${opsURL}/`);
  await expect(page.getByRole("link", { name: "上传控制", exact: true })).toHaveCount(0);
  for (const method of ["get", "post"]) {
    const response = await page.request[method](`${apiURL}/admin/v1/media/upload-control${method === "post" ? "/commands" : ""}`, { headers: { Cookie: "sd_session=browser-editor" }, ...(method === "post" ? { data: {} } : {}) });
    expect(response.status()).toBe(403);
  }
});
