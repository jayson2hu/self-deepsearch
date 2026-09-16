import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const source = (relativePath) => readFile(path.join(root, relativePath), "utf8");

test("我的入口覆盖现有账号能力且不制造未读计数", async () => {
  const [accountPage, nav, menu] = await Promise.all([
    source("apps/display-web/app/account/page.tsx"),
    source("apps/display-web/components/main-nav.tsx"),
    source("apps/display-web/components/account-actions.tsx"),
  ]);
  for (const href of ["/account/favorites", "/account/follows", "/account/hidden", "/account/history", "/account/feedback", "/account/settings"]) {
    assert.match(accountPage, new RegExp(href));
  }
  assert.match(accountPage, /不提供未读数量或新消息角标/);
  assert.match(nav, /href: "\/account", label: "我的"/);
  assert.match(menu, /href="\/account">账号概览/);
  assert.doesNotMatch(accountPage, /badge|unread_count|未读\s*\{/i);
});

test("关注作品只陈述发行日期并明确不等同于新收录或未读", async () => {
  const [page, homeSection, api] = await Promise.all([
    source("apps/display-web/app/account/follows/works/page.tsx"),
    source("apps/display-web/components/followed-works-section.tsx"),
    source("apps/display-web/lib/api.ts"),
  ]);
  assert.match(api, /privateGet<ItemListResponse>\("\/api\/v1\/me\/followed-works"\)/);
  assert.match(page, /卡片日期是作品发行日期/);
  assert.match(page, /最多展示按发行日期排序的 12 条作品/);
  assert.match(page, /不会标记“新收录”、未读或新消息数量/);
  assert.match(homeSection, /关注人物的已发布作品/);
  assert.doesNotMatch(homeSection, /关注人物的新作品/);
});

test("反馈页面映射真实枚举且公共响应不投影内部审核信息", async () => {
  const [labels, page, handlers, store] = await Promise.all([
    source("apps/display-web/lib/feedback-labels.ts"),
    source("apps/display-web/app/account/feedback/page.tsx"),
    source("services/platform-api/internal/httpapi/account_handlers.go"),
    source("services/platform-api/internal/database/account_store.go"),
  ]);
  for (const [status, label] of [["pending", "已收到"], ["reviewing", "处理中"], ["accepted", "已采纳"], ["rejected", "未采纳"], ["closed", "已结束"]]) {
    assert.match(labels, new RegExp(`${status}: "${label}"`));
  }
  assert.match(page, /内部审核备注和其他用户反馈不会下发/);
  const publicProjection = handlers.match(/func feedbackItem[\s\S]*?\n}/)?.[0] ?? "";
  assert.doesNotMatch(publicProjection, /Reviewer|ReviewedAt|Reason/);
  const ownQuery = store.match(/func \(store \*Store\) ListFeedback[\s\S]*?func \(store \*Store\) ListFeedbackForReview/)?.[0] ?? "";
  assert.match(ownQuery, /WHERE user_id = \$1::uuid/);
  assert.doesNotMatch(ownQuery, /review_reason|audit_logs/);
});

test("反馈提交只提供状态去向，不创建登录后自动提交意图", async () => {
  const [form, favorite, follow] = await Promise.all([
    source("apps/display-web/components/feedback-form.tsx"),
    source("apps/display-web/components/favorite-button.tsx"),
    source("apps/display-web/components/follow-button.tsx"),
  ]);
  assert.match(form, /查看我的反馈状态/);
  assert.doesNotMatch(form, /auth-intent|sessionStorage|returnTo/);
  assert.match(favorite, /继续收藏这部作品/);
  assert.match(follow, /继续关注这位人物/);
});
