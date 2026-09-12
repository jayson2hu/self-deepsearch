# Release A：当前代码全量本地回归

日期：2026-09-11（Asia/Shanghai）  
结论：当前工作区在不启动 Docker、不连接真实 PostgreSQL/SMTP/S3/R2/Cloudflare 的前提下，通过统一预检、production Next 构建、standalone HTTP、缓存失效合同、本地浏览器性能、HTTP P95 和桌面/移动 Playwright 回归。该结果提高本地 Release A 置信度，但不能替代目标环境验收，整体仍 no-go。

## 统一预检

执行：

```text
npm run preflight:release-a
```

通过项：

- Go API/Worker `go test`；
- Go API/Worker `go vet`；
- OpenAPI TypeScript 生成漂移检查；
- Markdown 本地链接门禁：62 份文件、392 个本地链接、0 缺失；
- 两站和 API contracts TypeScript；
- 两站 ESLint；
- production dependency audit：0 vulnerabilities；
- Python Ruff；
- Python mypy：18 个源文件无问题；
- Python pytest：298 passed、296 subtests passed、1 skipped；覆盖 Turnstile 生产 hostname、生产目录不含合成 fallback、CSV 错误导出安全合同及错误配置子场景，唯一 skip 仍是当前 Windows 账号不能创建 symlink；
- Release tooling contracts：62/62；新增 5 项链接收集、解析、编码、外部目标和失败路径合同；
- frontend smoke unit tests：40/40；除公开账号路径分类、128 位 nonce、严格策略和缓存目录边界外，新增 2 项 CSV 公式前缀中和与错误行导出合同；
- display-web 与 ops-web Next.js production build；
- standalone frontend HTTP smoke；
- 真实 Go cache processor → production Next 缓存失效合同，包括错误签名、发布刷新、媒体刷新、隐藏移除和重复事件。

一次中间全量复跑中，备份 shell 合同在 Windows Git shell 下触发 30 秒偶发超时；该用例随后单独 1/1 通过，最终无并行负载的全量复跑为 296 passed、296 subtests passed、1 skipped，未为消除超时修改备份实现或放宽断言。

默认预检按设计没有执行 Compose、浏览器性能和 Playwright；后两项已在下方独立补跑。其后另以 Docker CLI 静态解析三套 Compose 与五镜像 Bake 图，未启动容器，见[部署静态证据](./release-a-deployment-static-2026-09-11.md)。

## 浏览器性能与 HTTP P95

执行：

```text
npm run probe:performance:release-a
npm run probe:http-latency:release-a
```

性能报告：[release-a-performance-2026-09-11.json](./release-a-performance-2026-09-11.json)

| 页面 | 视口 | LCP 中位数 | 最大 CLS | 门槛 |
| --- | --- | ---: | ---: | --- |
| 首页 | desktop 1366×900 | 240 ms | 0.00156 | LCP < 2500 ms，通过 |
| 作品详情 | desktop 1366×900 | 168 ms | 0.00053 | LCP < 2500 ms，通过 |
| 首页 | mobile 390×844 | 208 ms | 0 | LCP < 2500 ms、CLS < 0.1，通过 |
| 作品详情 | mobile 390×844 | 148 ms | 0 | LCP < 2500 ms、CLS < 0.1，通过 |

HTTP 报告：[release-a-http-latency-2026-09-11.json](./release-a-http-latency-2026-09-11.json)

| 接口 | 样本 | P95 | 门槛 |
| --- | ---: | ---: | --- |
| `/api/v1/site/home` | 20，单并发，另有 3 次预热 | 15.4 ms | < 300 ms，通过 |
| `/api/v1/search/works` | 20，单并发，另有 3 次预热 | 8.17 ms | < 500 ms，通过 |

两份报告均明确是本机无网络限速、预热缓存、production standalone + 内存合成 API；不能外推日本 2C4G、真实 PostgreSQL、Cloudflare 或公网延迟。性能探针现于指标读取后、页面关闭前，对同源 document/fetch/xhr 流执行最多 2 秒的有界排空；排空时间不计入指标，也不使用 `networkidle`。专项单元 10/10 和完整 3 样本探针均通过，原先页面关闭时的 `The destination stream closed early` 诊断不再出现。

## Playwright 回归

执行：

```text
npm run test:e2e:release-a
```

最终结果：78/78 passed，1 worker，约 6.4 分钟。desktop 与 mobile 各覆盖 39 项，包括：

- 人工/动态默认图停止与恢复、缓存 HTML；
- 运行时首页在静态停图重启后直接投影默认图，且隔离 standalone 不继承来源副本的请求期 fetch cache；
- 匿名番号搜索和作品详情；
- 注册、邮箱验证码、登录、退出失败恢复；
- 忘记密码和关闭账号邮箱验证码；
- 收藏、关注、隐藏、历史和反馈；
- 主图首次登记、替换、冲突、失败和角色权限；
- 作品 CSV 上传预检后，浏览器下载的错误文件保留 BOM/引用并中和表格公式；
- 两人录入/审核/发布、owner 邀请、角色管理；
- 反馈领取/完成、权利下架；
- 用量复核、账期切换和编辑角色隔离；
- 上传暂停/恢复确认、回执丢失、旧快照、过期、未知/被取代回执和匿名/编辑权限。
- 运营后台及公开账号页每请求 CSP nonce 轮换、所有可执行脚本 nonce 一致、无 CSP 拒绝日志；公开目录框架脚本仍使用无 nonce 的独立 CSP 策略，后续首页改为运行时生成不改变这一 CSP 边界。

首次完整复跑时，旧媒体用例仍要求静态停图重启后的首页 HTML 保留远程图片 URL；这与当前首页 `force-dynamic` 和服务端默认图投影不一致。修正断言后，媒体专项 desktop/mobile 4/4、完整浏览器 78/78 和真实 Go→Next 缓存合同 9/9 均通过。隔离运行时现在还会删除复制来源中的 `.next/cache` 请求期数据，并由工具合同验证源缓存不被修改、隔离副本不继承污染项；主浏览器套件同样使用独立 display 副本，定向后台用例不会再写共享公开站缓存。

## 对象存储任务清单补验

新增机器可读任务清单、北京资源限制和共 7 项静态合同后，重新执行上述默认统一预检并完整通过。任务清单覆盖北京受控上传、日本到北京全量对账、必要权利删除和 Cloudflare 公开读取，确认当前唯一非必要自动对象任务是全量对账；资源合同将北京常驻预算限制在 1.45 CPU/2240 MiB。相关边界见[任务清单证据](./release-a-media-task-inventory-2026-09-11.md)和[北京资源证据](./release-a-beijing-resource-budget-2026-09-11.md)。加入[Turnstile 绑定证据](./release-a-turnstile-binding-2026-09-11.md)时曾检查 60 份 Markdown、380 个本地链接；加入运行时目录证据时为 61 份/388 个链接；加入 CSV 错误导出证据并把其引用纳入就绪文档后的当前门禁为 62 份 Markdown、392 个本地链接，均无缺失。该检查由独立 Node 脚本执行，进入默认预检和 CI；缺文件、无效百分号编码或越出仓库的本地目标都会阻断。

## Turnstile 绑定补验

公开站注册与密码重置组件现在分别签发 `signup_code` 和 `password_code`；Go API 路由传递固定 action，Siteverify 结果必须同时满足 success、预期 hostname 和相同 action。外部调用增加 2 KiB token、16 KiB JSON 响应、HTTP 200、Content-Type、5 秒超时及拒绝重定向边界；生产配置和部署检查要求 `TURNSTILE_EXPECTED_HOSTNAME` 为纯 hostname 且与 `SITE_URL` 一致。Go 全包 test/vet、前端 typecheck/lint、38/38 smoke 和环境/产品合同均通过。该补验未连接真实 Cloudflare，最终域名验收仍是公网 no-go 条件。

## 生产镜像目录隔离补验

回归审计发现旧首页在构建期 API 不可用时会使用 `TEST-001`～`TEST-004` 合成 fallback，production HTML 因而包含虚构目录与旧故障提示。现已删除该 fallback，将首页、`robots.txt` 和 `sitemap.xml` 改为运行时生成；故障首页只返回六个空 section 和明确不可用提示。API fetch 仍保留 300 秒 `public-catalog` 数据缓存，公共入口仍可按 5 分钟策略缓存成功响应。

重新执行两站 production build，三条路由均为动态 `ƒ`；扫描 `apps/display-web/.next/server/app` 未发现“城市记录”“当前展示本地合成数据”或目录形式的 `TEST-` 编号。standalone HTTP 冒烟通过，真实 Go→Next 9 项缓存合同也全部通过，包括错误/过期签名负向对照、发布、媒体 purge、隐藏和重复事件。详见[运行时目录证据](./release-a-runtime-catalog-2026-09-11.md)。

横向复核随后把生产站点 origin 也改为 fail-closed：根布局不再在构建期调用 `siteURL()`；生产只接受运行时 `SITE_URL`，不使用 `NEXT_PUBLIC_SITE_URL` 或回环 fallback。健康 standalone 的 robots/sitemap 输出随机测试 origin；缺失 `SITE_URL`、只提供构建期公开变量的第二实例返回 500 且不泄漏两个 fallback origin。公开站 typecheck/ESLint/build、产品合同 45 passed/7 subtests、前端单元 38/38 和 9 项缓存合同通过。

## 未覆盖与发布判断

- 没有启动 Docker Compose；根级、日本和北京配置及五镜像 Bake 图仅完成静态解析；
- 没有真实 PostgreSQL migrations/权限/并发合同；
- 没有 Mailpit/SMTP、真实 R2/S3、Cloudflare Worker、最终域名和两地服务器；
- 没有目标环境资源、网络、缓存和恢复验证；
- 浏览器使用合成资料和内存状态，不能证明真实持久事务。

因此本轮建议仍为 **no-go for public release / local synthetic regression passed**。
