# RD v1 / DEV-13 SEO 技术基础开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，合同/真实 SQL/生产构建/HTTP 冒烟通过，真实浏览器与 DEV-14 正式内容策略待验收。

## 实际交付

- 新增 fail-closed SEO 发布门禁：只有 `APP_ENV=production`、`SEO_INDEXING_ENABLED=true`、`SEO_CONTENT_POLICY_READY=true` 和非回环 HTTPS `SITE_URL` 同时成立才允许索引。测试、demo、缺配置或 HTTP origin 均输出全站 `noindex`，robots 为 `Disallow: /`，sitemap 返回 404。
- 发布环境预检要求两个 SEO 开关均为显式布尔值，索引开启必须同时声明内容策略就绪；Japan Compose 默认保持关闭。没有在本次打开正式索引、连接搜索工具或提交 sitemap。
- 根 metadata 使用运行时 `SITE_URL` 作为 `metadataBase`。首页、about、四个目录和三类详情具有正式 canonical/robots；合法且有内容的游标页自引用，空页、坏游标与依赖故障不进入索引。搜索和账号页允许返回正文指令但始终 `noindex`。
- Next proxy 在正文开始流式输出前预检公开首页、目录和详情依赖，真实返回 200/301/400/404/503；503 响应 `no-store`、`Retry-After` 和 `X-Robots-Tag`，不再把上游故障包装成 200 的替代资料页。
- sitemap API 增加 `page/page_size/total_items/total_pages`，每片最多 50,000 URL，按 `entity_type, canonical_slug` 确定性分页。索引按真实总数列出全部分片；旧单片地址 301 到第一页；XML 同时检查 50,000 URL 与 50 MB 未压缩上限。
- sitemap 继续只读取公开视图，`lastmod` 使用 publication 的真实 `updated_at`；隐藏/下架自动退出。固定页面 sitemap 在依赖故障时返回 503，避免临时生成不完整地图；空目录不进入 HTML 索引或 XML。
- 三类详情保留 CreativeWork/Person/Organization，并新增与可见导航完全一致的 `BreadcrumbList`。未新增评分、视频、来源面板或图片 sitemap；内容分类、成人标记和媒体外部预览许可留 DEV-14 的统一策略合同处理。

## 验收结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node --test scripts/tests/redesign_seo_foundation.test.mjs ...` | 通过 | fail-closed 门禁、分页 canonical、50,001 分片、50 MB 边界、robots/proxy/sitemap/面包屑合同及 DEV-01/07 回归 |
| `.venv/bin/python -m unittest scripts.tests.test_release_a_env_check` | 20/20 通过 | SEO 部署开关、HTTPS 前置、广告可选配置和既有 Release A 环境合同 |
| `npm run check:api-types` | 通过 | sitemap OpenAPI 与生成 TypeScript 类型一致 |
| display-web typecheck / lint / production build | 通过 | 动态 metadata、状态预检、XML 路由和结构化数据；最终构建全部页面为运行时渲染 |
| platform-api + platform-worker 全量 Go 测试 | 通过 | 沙箱内第一次因禁止 `httptest` 监听失败；授予回环监听后全包通过 |
| 一次性 PostgreSQL 16 / Schema v21 / 受限账号 | 通过 | 26 条新增公开厂牌、隐藏/下架排除、17 条跨页无重漏、trigger 维护的真实 lastmod、隐藏后计数与集合同步移除 |
| production standalone HTTP smoke | 通过 | 受保护环境 fail-closed、正式放行 robots/canonical/sitemap、固定页与实体分片、旧址 301，以及正文 200/301/400/404/503 |
| Playwright 用例发现 | desktop 1 条 | 三类详情可见面包屑与 BreadcrumbList 一致 |
| Playwright desktop 定向执行 | 未进入浏览器步骤 | 服务器缺 `/opt/google/chrome/chrome`，报 `Chromium distribution 'chrome' is not found` |

真实 PG 首次断言曾假定可手写固定 `updated_at`，被数据库更新时间触发器正确改写而失败；测试修正为先读取公开 publication 的实际值，再与 sitemap `lastmod` 比较，随后通过。一次性容器和合成数据均已删除。

## AC 映射与未关闭门禁

- AC-22：环境/origin、robots/noindex、search/private、合法分页、空页与 HTML/XML 一致性已由合同和 HTTP 通过；最终真实 HTTPS 域名及边缘访问控制仍需 DEV-14/15 在目标入口复验。
- AC-23：200/301/400/404/503 与故障 `no-store` 已由 standalone 故障注入通过；缓存回源和最终边缘/CDN 行为留 DEV-15。
- AC-24：50,001 分片算法、50 MB 拒绝边界、真实 PG 无重漏/隐藏移除/lastmod 已通过；生产代表性总量与最终 URL 集合比较留 DEV-15。
- AC-25：实体 JSON-LD 与可见面包屑已完成，HTTP/源码合同通过；真实浏览器尚缺。成人分类、图片预览许可、资源级头和 OG/JSON-LD 图片允许清单属于 DEV-14，不能提前宣称完成。

因此 DEV-13 保持“待验收”，不计入已验收数。当前没有产品决策阻塞；剩余条件是可用 Chrome、最终 HTTPS/边缘环境，以及 DEV-14 的分类/媒体许可数据合同。

## 回退

紧急回退优先保持 `SEO_INDEXING_ENABLED=false`，该操作不删除页面但会让 robots 全站禁止且所有 HTML 带 noindex；不能声称代码回退会立即撤销搜索引擎已经取得的内容。若回退 sitemap 分片 API，必须同步回退 sitemap index/子路由，不能恢复到静默截断 50,000 条的旧行为。旧实体 URL 的 301 和真实 404/503 语义应保留，不得以 200 fallback 代替。
