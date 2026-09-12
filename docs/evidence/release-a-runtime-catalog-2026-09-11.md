# Release A：生产镜像运行时目录隔离

日期：2026-09-11（Asia/Shanghai）  
结论：已修复公开站 production build 在目录 API 不可用时把本地合成作品和构建机站点配置写入部署镜像的 P1。公开首页、`robots.txt` 和 `sitemap.xml` 现在由运行环境生成；首页故障时只展示空目录与明确服务提示，不展示虚构作品。API 数据缓存、标签失效和公共入口 5 分钟缓存仍保留。

## 问题与影响

旧 `apps/display-web/lib/api.ts` 在首页 API 请求失败时返回 `TEST-001`～`TEST-004` 合成作品。Next production build 会预渲染首页，因此构建产物实际包含合成标题、番号和“当前展示本地合成数据”提示。相同构建期行为还可能把回环 `PLATFORM_API_URL` 或构建机 `SITE_URL` 影响带入首页、robots 与 sitemap。

这不是资源泄漏，但会污染生产 SEO、首屏资料和站点身份，也会让 API 故障被错误伪装成正常目录，故按 P1 修复。

## 修复

- `apps/display-web/lib/api.ts` 删除合成首页 fallback；故障值保留六个合法空 section，供页面稳定显示空状态。
- `apps/display-web/app/page.tsx` 使用 `dynamic = "force-dynamic"`，目录服务失败时显示“目录服务暂时不可用，当前不展示作品或人物资料”。
- `apps/display-web/app/robots.ts` 与 `apps/display-web/app/sitemap.xml/route.ts` 改为运行时生成，使用部署环境的 `SITE_URL`。
- `apps/display-web/lib/site.ts` 在生产只读取运行时 `SITE_URL`，要求无凭据、无路径/查询/片段的 HTTP(S) origin，非回环 HTTP 拒绝；缺失或非法时抛错，不退回 `NEXT_PUBLIC_SITE_URL` 或 `127.0.0.1`。开发环境仍保留回环 fallback。
- 根布局移除构建期 `metadataBase: siteURL()`；具体详情 canonical、robots 与 sitemap 在请求时解析站点 origin。
- 首页目录 fetch 继续使用 `next.revalidate = 300` 和 `public-catalog` 标签；Nginx/Cloudflare 的成功公共目录响应仍按 5 分钟策略缓存。运行时渲染不等于关闭数据缓存或边缘缓存。
- 前端 HTTP 冒烟要求健康 mock API 的真实标题出现，且禁止旧构建期 fallback 提示；产品合同禁止 `api.ts` 恢复合成番号，并锁定三条路由的动态边界。

搜索框中的 `TEST-001` 仅是测试环境示例 placeholder，不是目录记录，不进入标题、卡片、结构化数据或 sitemap。

## 验证

```text
npm run build
rg -n "城市记录|当前展示本地合成数据|code.:.TEST-" apps/display-web/.next/server/app
npm run smoke:frontend:release-a
npm run test:cache:release-a
```

结果：

- 两站 production build 通过；公开站路由表确认 `/`、`/robots.txt`、`/sitemap.xml` 均为动态 `ƒ`。
- 构建产物扫描无匹配，`rg` 按预期以无结果状态退出。
- standalone HTTP 冒烟通过：首页读取健康 mock API 内容，登录/注册、页脚、广告占位、默认图、JSON-LD、安全头、CSS 和 API 503/no-store 边界正常。
- 健康 standalone 的 robots/sitemap 精确包含本次随机运行 origin；第二个 production standalone 只提供构建期 `NEXT_PUBLIC_SITE_URL` 且清空 `SITE_URL`，`/robots.txt` 按预期返回 500，响应不含回环或构建期域名。
- 真实 Go cache processor → production Next 的 9 项合同全部通过：初次缓存、未失效、错误签名、过期签名、发布刷新、仅媒体变化保持缓存、媒体 purge、隐藏移除、重复事件幂等。
- 加入 CSV 错误导出加固后的完整默认预检仍退出 0：Go test/vet、OpenAPI、TypeScript/ESLint、依赖审计 0 高危、Ruff/mypy、Python 298 passed/296 subtests/1 skipped、62 项工具合同、40 项前端单元、两站构建、HTTP 和缓存合同全部通过。
- 当前 Markdown 门禁检查 62 份文件、392 个本地链接，0 缺失。

## 边界与发布判断

本轮没有启动 Docker，没有连接 PostgreSQL、SMTP、S3/R2 或 Cloudflare，也没有验证最终域名、多实例传播和真实爬虫行为。合成 mock 只用于测试运行态合同，不进入部署镜像目录数据。

本地代码与静态部署包保持 **conditional-go**；公网开放仍为 **no-go**。
