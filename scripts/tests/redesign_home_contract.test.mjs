import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const root = new URL("../../", import.meta.url);
const read = (path) => readFile(new URL(path, root), "utf8");

test("DEV-02 home makes query and one primary directory the first tasks", async () => {
  const source = await read("apps/display-web/app/page.tsx");
  assert.match(source, /查询番号或名称，确认公开资料/);
  assert.match(source, /查询作品、人物与厂牌资料/);
  assert.match(source, /主要目录 · 按实际发行日期/);
  assert.match(source, /继续查找资料/);
  assert.doesNotMatch(source, /近期热门|浏览最多|编辑推荐|最新作品与人物混排/);
  assert.doesNotMatch(source, /审核|核验|运营人工维护/);
  assert.ok(source.indexOf("latest-title") < source.indexOf("browse-title"));
});

test("DEV-02 header exposes query, browse, people, account, and a mobile search target", async () => {
  const [header, navigation, css] = await Promise.all([
    read("apps/display-web/components/site-header.tsx"),
    read("apps/display-web/components/main-nav.tsx"),
    read("apps/display-web/app/globals.css"),
  ]);
  for (const label of ["查询", "浏览", "人物", "我的"]) assert.ok(navigation.includes(`label: "${label}"`));
  assert.match(header, /aria-label="打开搜索"/);
  assert.match(header, /页头搜索番号、标题、人物或厂牌/);
  assert.match(css, /\.header-search-link \{[^}]*display: none/);
  assert.match(css, /@media \(max-width: 760px\)[\s\S]*\.header-search-link \{ display: inline-flex/);
  assert.match(css, /\.browse-paths \{ grid-template-columns: 1fr; \}/);
});

test("DEV-02 public home and footer do not explain internal maintenance workflow", async () => {
  const [home, footer] = await Promise.all([
    read("apps/display-web/app/page.tsx"),
    read("apps/display-web/components/site-footer.tsx"),
  ]);
  const publicCopy = `${home}\n${footer}`;
  for (const term of ["审核队列", "核验中", "运营人工维护", "经审核的公开来源"]) {
    assert.ok(!publicCopy.includes(term), `public copy exposes ${term}`);
  }
  assert.match(footer, /公开可访问的文字和图片资料/);
});

test("DEV-02 production build keeps TypeScript checking on the compiler API path", async () => {
  const config = await read("apps/display-web/next.config.ts");
  assert.match(config, /useTypeScriptCli: false/);
  assert.doesNotMatch(config, /ignoreBuildErrors:\s*true/);
});
