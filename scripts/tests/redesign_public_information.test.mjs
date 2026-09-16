import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const read = (relativePath) => readFile(path.join(repositoryRoot, relativePath), "utf8");

test("public information pages state current behavior without deletion promises", async () => {
  const privacy = await read("apps/display-web/app/privacy/page.tsx");
  const about = await read("apps/display-web/app/about/page.tsx");

  for (const signal of ["匿名浏览不会写入某个账号的个人浏览历史", "当前没有“停止未来记录”的用户开关", "不会物理删除底层记录", "关闭账号不等于全量物理删除", "没有对用户提供“永久删除全部数据”的按钮"]) {
    assert.match(privacy, new RegExp(signal));
  }
  assert.match(privacy, /robots: \{ index: false, follow: true \}/);
  assert.match(about, /关于幕鉴/);
  assert.match(about, /不提供、存储或跳转到视频、音频、磁力、种子、网盘、下载或播放资源/);
  assert.doesNotMatch(`${privacy}\n${about}`, /已永久删除|固定保留 30 天|保证删除全部/);
});

test("registration, history, settings, account close, and footer link the explanation", async () => {
  const files = [
    "apps/display-web/app/register/page.tsx",
    "apps/display-web/app/account/history/page.tsx",
    "apps/display-web/app/account/settings/page.tsx",
    "apps/display-web/components/close-account-form.tsx",
    "apps/display-web/components/site-footer.tsx",
  ];
  for (const relativePath of files) assert.match(await read(relativePath), /\/privacy/);
  assert.match(await read("apps/display-web/components/site-footer.tsx"), /\/about/);
});

test("public contact fails visibly when a real address is not configured", async () => {
  const contact = await read("apps/display-web/lib/public-contact.ts");
  const privacy = await read("apps/display-web/app/privacy/page.tsx");
  assert.match(contact, /RIGHTS_CONTACT_EMAIL/);
  assert.match(contact, /endsWith\("\.invalid"\)/);
  assert.match(privacy, /尚未配置公开联系邮箱/);
  assert.doesNotMatch(privacy, /example\.invalid/);
});

test("about is discoverable but privacy is excluded from acquisition sitemap", async () => {
  const sitemap = await read("apps/display-web/app/sitemaps/[entity]/route.ts");
  assert.match(sitemap, /"\/about"/);
  assert.doesNotMatch(sitemap, /"\/privacy"/);
});
