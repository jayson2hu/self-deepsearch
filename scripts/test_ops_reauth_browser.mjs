#!/usr/bin/env node
// Runs the actual password dialog and adminFetch in Chromium without a Next build.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import http from "node:http";
import { chromium, expect } from "@playwright/test";
import ts from "typescript";

const root = new URL("../", import.meta.url);
const compile = (name) => ts.transpileModule(readFileSync(new URL(`apps/ops-web/lib/${name}.ts`, root), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
}).outputText.replace('"./password-confirmation"', '"./password-confirmation.js"');
const assets = {
  "/reauth-client.js": ["text/javascript", compile("reauth-client")],
  "/password-confirmation.js": ["text/javascript", compile("password-confirmation")],
  "/globals.css": ["text/css", readFileSync(new URL("apps/ops-web/app/globals.css", root), "utf8")],
  "/password-confirmation.css": ["text/css", readFileSync(new URL("apps/ops-web/app/password-confirmation.css", root), "utf8")],
};
const password = "Synthetic-masked-password-43";
let state;
const html = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Password confirmation fixture</title><link rel="stylesheet" href="/globals.css"><link rel="stylesheet" href="/password-confirmation.css"><body><main><h1>合成操作验证</h1><button id="one">执行操作</button><button id="both">同时执行两个操作</button><p id="result" role="status"></p></main><script type="module" src="/fixture.js"></script></body></html>`;
const fixture = `import {adminFetch} from '/reauth-client.js';
window.prompt=()=>{throw new Error('Native password prompt must never be used')};
window.exercise=(count)=>{
  window.done=false;window.outcomes=[];window.controllers=Array.from({length:count},()=>new AbortController());
  const trigger=document.activeElement;
  const requests=window.controllers.map(async(controller,index)=>{
    try{const response=await adminFetch('/mutation/'+index,{method:'POST',headers:{'Content-Type':'application/json','Idempotency-Key':'request-'+index},body:JSON.stringify({reason:'synthetic',index}),signal:controller.signal});return{status:response.status,body:await response.json()}}
    catch(error){return{error:error.name}}
  });trigger.disabled=true;
  Promise.all(requests).then(results=>{window.outcomes=results;document.querySelector('#result').textContent=JSON.stringify(results)})
  .finally(()=>{trigger.disabled=false;window.done=true});
};
document.querySelector('#one').onclick=()=>window.exercise(1);
document.querySelector('#both').onclick=()=>window.exercise(2);`;
const server = http.createServer(async (request, response) => {
  if (assets[request.url]) {
    const [type, body] = assets[request.url]; response.writeHead(200, { "Content-Type": type }); return response.end(body);
  }
  if (request.url === "/fixture.js") { response.writeHead(200, { "Content-Type": "text/javascript" }); return response.end(fixture); }
  if (request.url === "/") { response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" }); return response.end(html); }
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  const body = Buffer.concat(chunks).toString();
  response.setHeader("Content-Type", "application/json");
  if (request.url === "/api/v1/auth/reauth") {
    state.confirmations++;
    state.correctPassword = JSON.parse(body).password === password;
    if (state.mode === "transport") return response.destroy();
    if (state.mode === "reject") { response.statusCode = 401; return response.end(JSON.stringify({ error: { code: "INVALID_CREDENTIALS" } })); }
    assert.equal(state.correctPassword, true);
    response.setHeader("Set-Cookie", "sd_reauth=confirmed; HttpOnly; SameSite=Lax; Path=/");
    return response.end(JSON.stringify({ ok: true }));
  }
  if (request.url.startsWith("/mutation/")) {
    state.attempts++;
    state.bodies.push({ path: request.url, body, key: request.headers["idempotency-key"] });
    if (!request.headers.cookie?.includes("sd_reauth=confirmed") || state.mode === "again") {
      response.statusCode = 401; return response.end(JSON.stringify({ error: { code: "REAUTH_REQUIRED" } }));
    }
    state.executed++;
    return response.end(JSON.stringify({ ok: true }));
  }
  response.statusCode = 404; response.end("{}");
});
await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
let browser;
let passed = 0;
try {
  browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || "chrome", headless: true });
  for (const scenario of ["success", "cancel", "escape", "reject", "transport", "again", "concurrent", "abort_one", "abort_all"]) {
    state = { mode: scenario, attempts: 0, confirmations: 0, executed: 0, bodies: [], correctPassword: false };
    const context = await browser.newContext({ viewport: { width: 390, height: 844 } });
    try {
      const page = await context.newPage();
      const errors = []; page.on("pageerror", (error) => errors.push(error.message));
      let confirmationRequests = 0;
      page.on("request", (request) => { if (new URL(request.url()).pathname === "/api/v1/auth/reauth") confirmationRequests++; });
      if (scenario === "transport") await page.route("**/api/v1/auth/reauth", (route) => route.abort("failed"));
      await page.goto(origin);
      const concurrent = scenario === "concurrent" || scenario === "abort_one";
      const trigger = page.getByRole("button", { name: concurrent ? "同时执行两个操作" : "执行操作", exact: true });
      await trigger.click();
      const dialog = page.getByRole("dialog", { name: "确认当前账号密码" });
      const input = dialog.getByLabel("当前账号密码", { exact: true });
      await expect(dialog).toHaveCount(1);
      await expect(input).toHaveAttribute("type", "password");
      await expect(input).toHaveAttribute("autocomplete", "current-password");
      await expect(input).toBeFocused();
      assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
      if (scenario === "success") {
        await page.keyboard.press("Shift+Tab");
        await expect(dialog.getByRole("button", { name: "确认密码并继续" })).toBeFocused();
        await page.keyboard.press("Tab"); await expect(input).toBeFocused();
        await page.keyboard.press("Enter");
        assert.equal(state.confirmations, 0, "empty password must not submit");
      }
      await input.fill(password);
      assert.equal(await dialog.innerText().then((text) => text.includes(password)), false);
      assert.equal(await page.evaluate(() => localStorage.length + sessionStorage.length), 0);
      if (scenario === "cancel") await dialog.getByRole("button", { name: "取消", exact: true }).click();
      else if (scenario === "escape") await page.keyboard.press("Escape");
      else if (scenario === "abort_all") await page.evaluate(() => window.controllers[0].abort());
      else {
        if (scenario === "abort_one") {
          await expect.poll(() => state.attempts).toBe(2);
          await page.evaluate(() => window.controllers[0].abort());
          await expect(dialog).toBeVisible();
        }
        await input.press("Enter");
      }
      await expect.poll(() => page.evaluate(() => window.done)).toBe(true);
      await expect(dialog).toHaveCount(0);
      await expect(trigger).toBeFocused();
      assert.equal(await page.locator('input[type="password"]').count(), 0);
      const outcomes = await page.evaluate(() => window.outcomes);
      const canceled = ["cancel", "escape", "abort_all"].includes(scenario);
      assert.equal(state.confirmations, canceled || scenario === "transport" ? 0 : 1);
      assert.equal(confirmationRequests, canceled ? 0 : 1);
      assert.equal(errors.length, 0);
      if (canceled || scenario === "reject" || scenario === "transport") {
        assert.equal(state.attempts, 1); assert.equal(state.executed, 0);
      } else if (scenario === "again") {
        assert.equal(state.attempts, 2); assert.equal(state.executed, 0); assert.equal(outcomes[0].status, 401);
      } else if (scenario === "concurrent") {
        assert.equal(state.attempts, 4); assert.equal(state.executed, 2);
        assert.deepEqual(outcomes.map((result) => result.status), [200, 200]);
      } else if (scenario === "abort_one") {
        assert.equal(state.attempts, 3); assert.equal(state.executed, 1);
        assert.equal(outcomes[0].error, "AbortError"); assert.equal(outcomes[1].status, 200);
      } else {
        assert.equal(state.attempts, 2); assert.equal(state.executed, 1);
        assert.deepEqual(state.bodies[0], state.bodies[1]);
      }
      passed++; console.log(`PASS password confirmation: ${scenario}`);
    } finally { await context.close(); }
  }
  console.log(`Password confirmation browser checks passed: ${passed}`);
} finally {
  await browser?.close();
  server.closeAllConnections?.();
  await new Promise((resolve) => server.close(resolve));
}
