import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const documentFiles = ["PRODUCT_REVIEW_2026-09-16.md", "PRODUCT_REDESIGN_BRIEF.md", "PRODUCT_REDESIGN_BACKLOG.md", "PRODUCT_SEO_PLAN.md"];
const documents = await Promise.all(documentFiles.map((file) => readFile(path.join(root, "docs", file), "utf8")));
const escape = (value) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");

function inline(value) {
  const tokens = [];
  const token = (html) => `\u0000${tokens.push(html) - 1}\u0000`;
  value = value.replace(/`([^`]+)`/g, (_, code) => token(`<code>${escape(code)}</code>`));
  value = value.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, href) => {
    const doc = documentFiles.findIndex((file) => href.split("#")[0].endsWith(file));
    if (doc >= 0) return token(`<button type="button" class="text-link" data-open-doc="${doc}">${escape(label)}</button>`);
    if (/^https:\/\//.test(href)) return token(`<a href="${escape(href)}" target="_blank" rel="noopener noreferrer">${escape(label)} ↗</a>`);
    if (/\.png$/.test(href)) return token(`<a href="#evidence" data-show-evidence>${escape(label)}</a>`);
    if (href.startsWith("#")) return token(escape(label));
    return token(`<span class="source-ref" title="仓库内参考：${escape(href)}">${escape(label)}</span>`);
  });
  return escape(value).replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>").replace(/\u0000(\d+)\u0000/g, (_, index) => tokens[Number(index)]);
}

// The renderer covers the checked-in reports' Markdown subset. It escapes all
// source text and exposes only explicit HTTPS links and in-page document actions.
function markdown(source) {
  const lines = source.trim().split("\n");
  const result = [];
  const cells = (line) => line.trim().replace(/^\||\|$/g, "").split("|").map((cell) => cell.trim());
  for (let i = 0; i < lines.length;) {
    const line = lines[i];
    if (!line.trim()) { i++; continue; }
    if (line.startsWith("```")) {
      const block = []; i++;
      while (i < lines.length && !lines[i].startsWith("```")) block.push(lines[i++]);
      i++; result.push(`<pre><code>${escape(block.join("\n"))}</code></pre>`); continue;
    }
    const heading = /^(#{1,6}) (.+)$/.exec(line);
    if (heading) { const level = Math.min(6, heading[1].length + 1); result.push(`<h${level}>${inline(heading[2])}</h${level}>`); i++; continue; }
    if (line.startsWith("|") && /^\|[\s:|-]+\|\s*$/.test(lines[i + 1] || "")) {
      const headers = cells(line); i += 2; const rows = [];
      while (i < lines.length && lines[i].startsWith("|")) rows.push(cells(lines[i++]));
      result.push(`<div class="table-scroll" tabindex="0" role="region" aria-label="资料表，可横向滚动"><table><thead><tr>${headers.map((cell) => `<th scope="col">${inline(cell)}</th>`).join("")}</tr></thead><tbody>${rows.map((row) => `<tr>${row.map((cell) => `<td>${inline(cell)}</td>`).join("")}</tr>`).join("")}</tbody></table></div>`); continue;
    }
    if (/^(?:- |\d+\. )/.test(line)) {
      const ordered = /^\d/.test(line); const items = []; const pattern = ordered ? /^\d+\. / : /^- /;
      while (i < lines.length && pattern.test(lines[i])) items.push(`<li>${inline(lines[i++].replace(pattern, ""))}</li>`);
      result.push(`<${ordered ? "ol" : "ul"}>${items.join("")}</${ordered ? "ol" : "ul"}>`); continue;
    }
    const paragraph = [];
    while (i < lines.length && lines[i].trim() && !/^(?:#{1,6} |```|\||- |\d+\. )/.test(lines[i])) paragraph.push(lines[i++].trim());
    if (paragraph.length) result.push(`<p>${inline(paragraph.join(" "))}</p>`);
    else { result.push(`<p>${inline(lines[i])}</p>`); i++; }
  }
  return result.join("\n");
}

function sections(source, expression) {
  return [...source.matchAll(expression)].map((match) => {
    const start = match.index + match[0].length;
    const rest = source.slice(start);
    const end = rest.search(/^#{1,3} /m);
    return { id: match[1], title: match[2], html: markdown(end < 0 ? rest : rest.slice(0, end)) };
  });
}

const priorities = { "UX-01": "P0", "UX-02": "P0", "UX-03": "P0", "UX-04": "P0", "UX-05": "P1", "UX-06": "P0", "UX-07": "P0", "UX-08": "P1", "UX-09": "P1", "UX-10": "P1", "UX-11": "P1", "UX-12": "P1", "OP-01": "P1", "OP-02": "P1", "OP-03": "P1", "PM-01": "P0", "PM-02": "P1", "PM-03": "P2", "SEO-01": "P0", "SEO-02": "P0", "SEO-03": "P1", "SEO-04": "P1", "SEO-05": "P0", "SEO-06": "P1" };
const summaries = {
  "UX-01": "手机看完一条资料后，不能直接开始下一次搜索；大图又推迟了身份确认。",
  "UX-02": "用户为收藏或关注完成登录，却回到首页，需要重新寻找目标。",
  "UX-03": "历史页提示底层记录长期保留，注册前却没有可访问的完整隐私说明。",
  "UX-04": "广告尚未接入，多个占位已经占据用户的首屏和阅读空间。",
  "UX-05": "搜索提示支持人物与厂牌，实际结果却只有作品，查找意图不能直接满足。",
  "UX-06": "内部有来源与核验记录，访客却很难判断一条公开资料为什么可信。",
  "UX-07": "栏目数量多、冷启动空区块多，用户需要理解分类却未必得到更多价值。",
  "UX-08": "作品厂牌不可直接点击；人物与厂牌的大量关联作品缺少继续查看入口。",
  "UX-09": "关注已经可用，但更新入口、空状态与再次回来时的价值还不够明确。",
  "UX-10": "人物资料缺少纠错入口，反馈结果仍使用内部枚举，修正进度难理解。",
  "UX-11": "隐藏在搜索等列表生效，却在人物详情不生效，范围与文案不一致。",
  "UX-12": "错误、下架与空状态需要统一；普通列表的序号容易被当成排名。",
  "OP-01": "手填人物和厂牌 ID、阅读 JSON、连续弹框，增加维护资料的成本。",
  "OP-02": "审核与发布待办排在多张系统指标之后，手机导航又只保留图标。",
  "OP-03": "异人审核需要真实人员与维护时间，扩大采集前要先测清供给能力。",
  "PM-01": "先用明确范围、可靠资料和任务样本证明价值，再扩大目录。",
  "PM-02": "零结果和浏览量已有聚合，但它们不能单独证明用户正确完成任务。",
  "PM-03": "还没有流量与单位成本实测，商业化需要进入条件与可回退的实验。",
  "SEO-01": "搜索页的 noindex 与 robots 禁抓叠加，测试与正式环境的索引边界需要明确。",
  "SEO-02": "厂牌分页沿用第一页 canonical；预览 robots 与作品地图返回 500，需定位并在最终入口验收。",
  "SEO-03": "围绕番号、人物与厂牌建立准确落地页，用来源内容和关联链接满足真实查找意图。",
  "SEO-04": "实体地图存在 50,000 条截断边界；图片、分页和结构化数据需要一致的发现规则。",
  "SEO-05": "按实际正文和图片评估成人分类及 SafeSearch，明确搜索与分享预览边界。",
  "SEO-06": "用实际收录、搜索点击与真实页面体验建立基线；现有站内统计不能代替自然搜索数据。",
};
const findings = sections(documents[0], /^### ((?:UX|OP|PM|SEO)-\d+)｜(.+)$/gm).map((item) => ({ ...item, priority: priorities[item.id], group: item.id.split("-")[0], summary: summaries[item.id] }));
const decisions = documents[1].split("\n").filter((line) => /^\| D-\d+ /.test(line)).map((line) => {
  const [id, title, recommendation, impact] = line.split("|").slice(1, -1).map((cell) => cell.trim());
  return { id, title, recommendation, impact };
});
const taskRows = documents[2].split("\n").filter((line) => /^\| R-\d+ /.test(line)).map((line) => line.split("|").slice(1, -1).map((cell) => cell.trim()));
const tasks = sections(documents[2], /^### (R-\d+)｜(.+)$/gm).map((item) => {
  const row = taskRows.find((entry) => entry[0] === item.id);
  return { ...item, priority: row[2], references: row[3], scope: row[4], dependency: row[5] };
});
if (findings.length !== 24 || decisions.length !== 9 || tasks.length !== 20) throw new Error("Report structure changed; review page mapping needs updating.");
if (findings.some((item) => !item.priority || !item.summary || !item.html) || tasks.some((item) => !item.html)) throw new Error("Incomplete finding or task mapping.");
for (const entries of [findings, decisions, tasks]) if (new Set(entries.map((item) => item.id)).size !== entries.length) throw new Error("Duplicate review IDs.");

const screenshots = [
  ["display-home-mobile.png", "手机首页", "多栏目与冷启动空状态", "UX-04 · UX-07"],
  ["work-detail-mobile.png", "手机作品详情", "搜索入口与首屏信息顺序", "UX-01 · UX-06"],
  ["work-detail-desktop.png", "桌面作品详情", "广告占位与内容空间", "UX-04 · UX-06"],
  ["ops-dashboard-mobile.png", "手机运营概览", "系统指标优先，待办靠后", "OP-02"],
];
const evidence = await Promise.all(screenshots.map(async ([file, title, subtitle, related]) => ({ title, subtitle, related, src: `data:image/png;base64,${(await readFile(path.join(root, "docs/evidence/release-a-visual-2026-09-12", file))).toString("base64")}` })));
const data = { date: "2026-09-16", revision: "d79b304", findings, decisions, tasks, evidence, documents: documents.map(markdown) };
const template = await readFile(path.join(root, "scripts/templates/product-review.html"), "utf8");
if (template.split("__REVIEW_DATA__").length !== 2) throw new Error("Expected one data placeholder");
const counts = { FINDING: findings.length, DECISION: decisions.length, TASK: tasks.length, DOCUMENT: documents.length, SEO: findings.filter((item) => item.group === "SEO").length };
const output = template.replace(/__(FINDING|DECISION|TASK|DOCUMENT|SEO)_COUNT__/g, (_, key) => counts[key]).replace("__REVIEW_DATA__", JSON.stringify(data).replaceAll("<", "\\u003c").replaceAll("\u2028", "\\u2028").replaceAll("\u2029", "\\u2029"));
const target = path.join(root, "docs/product-review/index.html");
await mkdir(path.dirname(target), { recursive: true });
await writeFile(target, output);
console.log(`Built ${path.relative(root, target)} (${Buffer.byteLength(output)} bytes): ${findings.length} findings, ${decisions.length} decisions, ${tasks.length} tasks; self-contained with four screenshots.`);
