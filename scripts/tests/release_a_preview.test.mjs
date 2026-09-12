import assert from "node:assert/strict";
import net from "node:net";
import test from "node:test";

import {
  assertLocalPortsAvailable,
  DEFAULT_PREVIEW_PORTS,
  parsePreviewArgs,
  previewHelp,
  previewSummary,
} from "../release_a_preview.mjs";

test("preview defaults to stable localhost ports without rebuilding", () => {
  const options = parsePreviewArgs([]);
  assert.equal(options.build, false);
  assert.equal(options.displayPort, DEFAULT_PREVIEW_PORTS.display);
  assert.equal(options.opsPort, DEFAULT_PREVIEW_PORTS.ops);
  assert.equal(options.apiPort, DEFAULT_PREVIEW_PORTS.api);
});

test("preview accepts explicit build, timeout, and distinct ports", () => {
  const options = parsePreviewArgs([
    "--build",
    "--display-port", "5101",
    "--ops-port", "5102",
    "--api-port", "5103",
    "--timeout", "9000",
  ]);
  assert.equal(options.build, true);
  assert.equal(options.timeoutMs, 9000);
  assert.deepEqual([options.displayPort, options.opsPort, options.apiPort], [5101, 5102, 5103]);
});

test("preview rejects unsafe, missing, or duplicate port values", () => {
  assert.throws(() => parsePreviewArgs(["--display-port", "80"]), /1024-65535/);
  assert.throws(() => parsePreviewArgs(["--ops-port"]), /1024-65535/);
  assert.throws(() => parsePreviewArgs(["--api-port", "4173"]), /端口不能重复/);
});

test("preview summary exposes only synthetic local credentials and boundary warning", () => {
  const summary = previewSummary({ displayPort: 5101, opsPort: 5102, apiPort: 5103 });
  assert.equal(summary.status, "ready");
  assert.equal(summary.displayURL, "http://127.0.0.1:5101/");
  assert.equal(summary.opsURL, "http://127.0.0.1:5102/login");
  assert.equal(summary.verificationCode, "123456");
  assert.ok(summary.accounts.every((account) => account.email.endsWith(".test")));
  assert.match(summary.warning, /合成数据预览/);
});

test("preview help states local-only persistence and disabled dependencies", () => {
  const help = previewHelp();
  assert.match(help, /127\.0\.0\.1/);
  assert.match(help, /所有状态在进程退出后丢弃/);
  assert.match(help, /不会启动 Docker、PostgreSQL、邮件、对象存储、自动采集或广告联盟/);
});

test("preview keeps child output visible for startup diagnosis", async () => {
  const source = await import("node:fs/promises").then(({ readFile }) => readFile(new URL("../release_a_preview.mjs", import.meta.url), "utf8"));
  assert.equal(source.match(/stdio: "inherit"/g)?.length, 2);
});

test("preview rejects a port already owned by another local service", async (context) => {
  const server = net.createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  context.after(() => new Promise((resolve) => server.close(() => resolve())));
  const address = server.address();
  assert.equal(typeof address, "object");
  await assert.rejects(
    () => assertLocalPortsAvailable([address.port]),
    /本地预览端口 127\.0\.0\.1:\d+ 不可用/,
  );
});
