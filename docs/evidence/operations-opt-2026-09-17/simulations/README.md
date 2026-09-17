# 真实运行与合成数据验收

本目录记录“产品链路真实执行、业务数据可控合成”的补充验收。真实部分包括 PostgreSQL 16 / Schema 26、Go API、Next.js SSR、Google Chrome、Docker 镜像和现有后台权限；合成部分仅包括 10,000 个测试账号、测试广告、演示 SEO 报表及内容分类场景。没有把演示报表冒充授权 Search Console 数据，也没有把测试栈冒充生产。

## 已完成

- Google Chrome 153 执行桌面/移动端全量 E2E，98/98 通过；该套件使用真实浏览器和本地 mock upstream。
- 真实审查栈执行公开首页和用户后台 6 档宽度矩阵，共 12 项，全部 HTTP 200、无横向溢出、无后台服务告警。匿名页面对 `/api/v1/me` 与 `/api/v1/me/hidden-works` 的 401 是预期登录态探测。
- 真实审查栈首页/作品详情的 5 次预热后浏览器性能见[独立报告](../../operations-opt-performance-2026-09-17.json)；用户后台桌面 LCP 中位数 336ms，移动端 292ms，CLS 均为 0。
- 隔离真实 PostgreSQL 写入 10,000 个普通测试账号，对统计、100 条列表和筛选列表各执行 200 次、并发 5 的 API 采样，P95 分别为 56.14ms、35.27ms、79.36ms，均低于 500ms 预算。
- 真实广告发布、公开 API、SSR、暂停和已开页面轮询均完成；已开页面 29.704 秒移除广告，低于 60 秒门槛。
- SEO 扫描真实执行 130 个发布实体，重复扫描保持 130 个问题行并累计 260 个发现事件；演示报表真实走 multipart 导入、幂等和公式拒绝链路。
- owner 通过 Mailpit 邀请合成管理员，两个不同管理员分别完成真实 UI 异人审核，owner 再从 UI 发布 TEST-001/TEST-002；SEO 中心“技术可索引”由 0 变为 2。持续审查环境保持全局 noindex；另用符合正式门槛的临时 production/HTTPS canonical 前台验证页面输出 `index, follow`，作品 sitemap 同时包含两条 URL。验收结束后 4 个 `seo-reviewer-*` 合成管理员均通过产品状态管理锁定，旧会话失效，审计关系保留。
- 新建 Schema 26 数据库曾因 bootstrap 使用 `exec` 导致初始化后容器退出，已由 `253b5d5` 修复并以全新容器验证持续运行。
- 浏览器发现两个站点缺少 favicon，已由 `90a0961` 修复；干净提交快照构建的新镜像已部署审查环境，两个 `/icon.svg` 均返回 200。

## 截图

- [广告生效](./ad-live-desktop.png) / [广告暂停后](./ad-paused-desktop.png)
- [公开首页 390px](./display-home-390.png) / [公开首页 1440px](./display-home-1440.png)
- [用户后台 390px](./ops-users-390.png) / [用户后台 1440px](./ops-users-1440.png)
- [SEO 修订审核发布后可索引](./seo-indexable-after.png)

机器可读明细见 [results.json](./results.json)。尚未完成的外部/生产条件见上级 [blockers.md](../blockers.md)。
