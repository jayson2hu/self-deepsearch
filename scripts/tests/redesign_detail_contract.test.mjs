import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

test("DEV-03 three public details render the optional publication time", async () => {
  const [work, performer, studio, component] = await Promise.all([
    read("apps/display-web/app/works/[slug]/page.tsx"),
    read("apps/display-web/app/performers/[slug]/page.tsx"),
    read("apps/display-web/app/studios/[slug]/page.tsx"),
    read("apps/display-web/components/public-updated-at.tsx"),
  ]);
  for (const source of [work, performer, studio]) assert.match(source, /<PublicUpdatedAt value=/);
  assert.match(component, /资料更新：<time dateTime=\{value\}>/);
  assert.match(component, /if \(!value\) return null/);
});

test("DEV-03 work detail uses a real studio href and keeps the legacy name fallback", async () => {
  const work = await read("apps/display-web/app/works/[slug]/page.tsx");
  assert.match(work, /work\.studio \? <Link href=\{work\.studio\.href\}>/);
  assert.match(work, /work\.studio_name \|\| "待补充"/);
  assert.doesNotMatch(work, /`\/studios\/\$\{.*studio_name/);
});

test("DEV-03 public date is formatted in Asia\/Shanghai as YYYY-MM-DD", async () => {
  const formatter = await read("apps/display-web/lib/date-time.ts");
  assert.match(formatter, /timeZone: "Asia\/Shanghai"/);
  assert.match(formatter, /`\$\{parts\.year\}-\$\{parts\.month\}-\$\{parts\.day\}`/);
});

test("UX2 public details expose aggregate view counts without claiming unique visitors", async () => {
  const [openapi, work, performer, studio, component] = await Promise.all([
    read("packages/api-contracts/openapi.yaml"),
    read("apps/display-web/app/works/[slug]/page.tsx"),
    read("apps/display-web/app/performers/[slug]/page.tsx"),
    read("apps/display-web/app/studios/[slug]/page.tsx"),
    read("apps/display-web/components/view-stats.tsx"),
  ]);
  assert.match(openapi, /view_count_30d/);
  assert.match(openapi, /view_count_total/);
  for (const source of [work, performer, studio]) assert.match(source, /<ViewStats last30Days=/);
  assert.match(component, /近 30 天/);
  assert.match(component, /次详情浏览/);
  assert.doesNotMatch(component, /独立用户|访客人数/);
});
