# RD v1 可执行验收终检

批次：`RD-ACCEPT-20260917-01`

执行时间：2026-09-16 23:15 ～ 2026-09-17 01:50（Asia/Shanghai）

代码冻结 SHA：`c60dc6f5e82c63e7f82ea01e9b8499b9535752b9`

结论：**本地与隔离集成可交付；生产发布仍为 BLOCKED。**

本批在 `git archive` 干净快照中执行代码验收，原工作区已有采集演示相关修改未被纳入、覆盖或暂存。验收标准见[执行标准](../../redesign/ACCEPTANCE_EXECUTION_STANDARD.md)，机器摘要见[manifest](./manifest.json)。

## 1. 已通过门禁

| 范围 | 结果 |
| --- | --- |
| Markdown 链接 | 112 个文件、722 个本地链接、0 缺失 |
| OpenAPI / TypeScript / ESLint | 生成类型一致；3 个 workspace typecheck 通过；display/ops lint 通过 |
| Go | API/Worker 全包 `go test -count=1` 通过；全包 `go vet` 通过 |
| Python | Ruff 通过；MyPy 15 个源文件通过；pytest 317 passed、348 subtests passed |
| Node 合同 | 发布预检 77/77；前端安全/媒体/交接 smoke 44/44 |
| 依赖 | `npm audit --omit=dev --audit-level=high` 为 0 漏洞 |
| 构建 | display-web、ops-web production build 通过；standalone HTTP smoke 通过 |
| PostgreSQL | PostgreSQL 16 空库迁移到 Schema v22；重复迁移 0 pending；受保护合成夹具与受限账号通过 |
| API 数据库合同 | 在重建并加载规定夹具的专用库中全包通过，9.245 s；含发布 SEO 生命周期回归 |
| 核心 E2E | 注册、重置、关户、会话撤销、邀请、编辑邀请、创建→审核→发布→搜索→sitemap→隐藏全部通过 |
| 缓存合同 | 真实 Go 处理器 + production Next、合成目录，9/9 场景通过 |
| HTTP 延迟 | 首页 API P95 33.27 ms；搜索 API P95 10.9 ms，均低于本地门槛 |
| Compose / Nginx | 本地、日本（含 edge/monitoring/tools）、北京配置解析通过；日本 Nginx `-t` 通过 |
| 发布镜像 | 五个镜像构建、标签、revision、非 root、HEALTHCHECK 通过；API+前台最小真实栈、后台、媒体运行态 HTTP 200 |
| Playwright 发现 | desktop/mobile 共 98 tests in 2 files；仅发现，不算浏览器执行通过 |

## 2. 发布镜像

所有最终镜像版本为 `acceptance-c60dc6f`，revision 为实际冻结提交 `c60dc6f5e82c63e7f82ea01e9b8499b9535752b9`。

| 镜像 | image ID | 用户 | 运行态 |
| --- | --- | --- | --- |
| platform-api | `sha256:b99f14e95ebd419aa2a7b76f43fe50f1e7bd573df0fcd2184d51f247804904f4` | `nonroot:nonroot` | 最终最小栈 healthy/200 |
| platform-worker | `sha256:0a1a594fedc2e7f3376f2dc16a8a19ecf2218ad123e97a673ac84b7d0952d3d9` | `nonroot:nonroot` | 对最终 API 执行内置 health 命令通过 |
| display-web | `sha256:f96271f0d657b21f40e91d87d81975a74916131967f5511b7b66c29b7472442b` | `node` | 最终同版 API+专用 PG 最小栈 healthy/200 |
| ops-web | `sha256:035c2f104e45c23c40a67ec9508d46c1b733b2700d883571390ca6c5c0755370` | `node` | 最终最小栈 healthy，`/login` 200 |
| media-python | `sha256:3926bd5ba33aa6a6bd226859ac5ce261d3856022a608f1aa7be7fb2de7049a79` | `media` | 最终最小栈 healthy，`/health/live` 200 |

首次镜像构建使用默认 `proxy.golang.org` 时下载 `pgpassfile` 超时；改用 `GOPROXY=https://goproxy.cn,direct` 后五镜像全部成功。media-python 的依赖下载较慢但最终完成。该问题归类为构建网络条件，不是代码失败。

## 3. AC-01～AC-30 逐项结论

| AC | 状态 | 本批已完成 | 未达原因 / 解锁条件 |
| --- | --- | --- | --- |
| 01 | PARTIAL | 首页/详情/search HTTP、production build、移动/桌面用例已发现 | 缺真实浏览器四宽度、焦点与视觉证据 |
| 02 | PASS | 三类详情合同、公开更新时间、厂牌对象、旧字段兼容和 HTML smoke 通过 | 产品负责人仍需体验复核 |
| 03 | PASS | 真实 PG 发布生命周期、草稿/批准/重发、公开时间与 revision provenance 通过 | 无新增技术阻塞 |
| 04 | PASS | API/HTML/JSON-LD 负例和公开字段边界通过 | 无新增技术阻塞 |
| 05 | PARTIAL | returnTo/意图安全负例、HTTP 与单元合同通过 | 缺真实浏览器跨登录/注册/重置操作 |
| 06 | PARTIAL | 关系 SQL、幂等、核心认证 E2E 通过 | 缺真实浏览器确认/取消/存储禁用矩阵 |
| 07 | PARTIAL | empty/error 不输出广告 DOM 的合同通过 | 缺真实浏览器截图和布局空白检查 |
| 08 | PARTIAL | ready/过期/错误/生产预览防伪配置合同通过 | 缺真实浏览器网络与布局证据；真实广告供应方未定 |
| 09 | PARTIAL | 推荐/目录语义、恢复和管理员隐藏优先级合同通过 | 缺浏览器逐格偏好矩阵 |
| 10 | PARTIAL | 公共缓存边界、缓存签名负例和隔离合同通过 | 缺两账号+匿名真实浏览器隔离矩阵 |
| 11 | PARTIAL | about/privacy 链接和事实文案合同通过 | 正式联系地址未由负责人核实；缺浏览器长文检查 |
| 12 | PASS | 标注查询、番号归一化、人物/厂牌关联查询真实 SQL 通过 | 生产代表性规模仍属目标环境项 |
| 13 | PARTIAL | 非法/零结果/5xx/重试 HTTP 状态合同通过 | 缺浏览器返回与重试操作证据 |
| 14 | PASS | 三实体搜索、别名/同名/隐藏过滤真实 SQL 与前端合同通过 | 无新增技术阻塞 |
| 15 | PASS | 作用域游标、末项消失、坏游标、静态集无重漏通过 | 并发变更限制已记录 |
| 16 | PARTIAL | 人物 101、厂牌 201 边界和 href/全集 SQL 对照通过 | 缺真实浏览器关闭 JS 链接检查 |
| 17 | PARTIAL | 我的入口、收藏/关注、空态/故障合同通过 | 缺真实账号浏览器闭环 |
| 18 | PASS | 本人/他人授权负例、内部备注隔离和中文状态合同通过 | 产品负责人仍需文案复核 |
| 19 | PASS | Q-01 未确认时没有虚假开关、期限或永久删除承诺 | 维持现行说明 |
| 20 | BLOCKED | 不执行未获批策略 | Q-01 必须先确定默认、迁移与关闭语义 |
| 21 | BLOCKED | 不执行未获批期限/清除策略 | Q-01 必须先确定期限、归档、恢复与回退规则 |
| 22 | PARTIAL | fail-closed 环境/开关、canonical、robots、分页合同通过 | 缺最终 HTTPS origin 和边缘入口复核 |
| 23 | PASS | 200/301/400/404/503、故障 no-store、正文一致性通过 | 无新增技术阻塞 |
| 24 | PASS | 50,001 分片、50 MB 边界、lastmod、隐藏移除和集合比较通过 | 目标数据总量需上线前再核对 |
| 25 | PARTIAL | 分类/许可三态、adult、OG/JSON-LD、缓存清除合同通过 | 缺真实对象存储/边缘媒体头与浏览器缓存复核 |
| 26 | BLOCKED | 用例发现 98 条 | 无 Chrome/Chromium；无法产出截图、交互和浏览器性能报告 |
| 27 | PARTIAL | 真实 Go API+PG+Mailpit 核心 E2E、真实 Go 缓存处理器+Next 均通过 | 两条证据边界分离，尚缺同一栈 PG→outbox→Worker→Next 全链 |
| 28 | PARTIAL | 固定代码 SHA、五镜像 ID、配置/迁移/回退和本地运行态通过 | 缺目标部署 digest、负责人签字和正式回退演练 |
| 29 | PASS | 审核→决策→设计→DEV→AC→证据可追踪；链接检查零缺失 | 无新增技术阻塞 |
| 30 | PARTIAL | 中文/Google 正式技术矩阵与 SEO 生命周期通过 | 缺生产回填统计、正式域名、Search Console 所有者和上线观测 |

## 4. 未达到项与明确原因

1. **真实浏览器 BLOCKED**：服务器无 Chrome/Chromium。`npx playwright install chromium` 的约 187 MB 下载长期停在 0%；拉取官方 Playwright 镜像时大层下载长期无进展，均中止以避免无限占用。解锁条件是提供可用浏览器缓存/镜像或允许稳定外网下载。
2. **目标环境 BLOCKED**：没有本批授权的最终 HTTPS 域名、目标日本/北京主机部署、CDN/边缘配置和发布凭据。解锁后按 DEV-15 顺序重验。
3. **外部供应商 BLOCKED**：未使用真实 S3/R2、Cloudflare、Turnstile、正式 SMTP、Search Console 或广告供应方；本地 Mailpit 只证明邮件合同。
4. **产品决策 BLOCKED**：Q-01 历史默认/期限/迁移/恢复未签署，AC-20/21 不存在可合法实现的目标；Q-05 真实广告和正式联系地址也需负责人确认。
5. **线上性能 BLOCKED**：本地 HTTP 延迟通过，但不能替代浏览器 LCP/CLS/INP 或真实用户第 75 百分位数据。

## 5. 失败尝试保留

- 未授权网络沙箱内 Node 回环测试出现 `listen EPERM`；在允许回环网络后预检 77/77、smoke 44/44 通过。
- 首次 API 数据库全包复用了已被前序验收写入的库，搜索集合出现重复；重建空库但未加载开发夹具时，夹具依赖用例失败；按标准“空库迁移→受保护夹具→受限账号”重跑后全包通过。两次前置错误不删除，也不计产品失败。
- 前台镜像首次连接旧 review API 时按设计返回 503；改用同一 `c60dc6f` API 镜像和专用 PG 后，API/前台均 healthy 且 HTTP 200。
- media-python 首次使用过短合成 HMAC/Cloudflare 参数被 fail-closed 拒绝；使用满足校验的合成配置后 healthy/200，证明配置保护生效。
- 初次镜像标签使用了不对应当前提交的 revision 值；终检发现后以实际 SHA 重打五镜像并更新 image ID，随后用全新 PostgreSQL 16、Schema v22、合成夹具和最终五镜像重跑最小栈，全部 healthy/HTTP 200。首次新库连接碰到 PostgreSQL 正在启动，加入显式 SQL 就绪探针后通过。

## 6. 原始证据与边界

- [缓存合同 JSON](./cache-contract.json)：真实 Go 处理器 + production Next；目录为内存合成替身。
- [核心 E2E JSON](./core-e2e.json)：真实 Go API + PostgreSQL + Mailpit；不含 Worker/前端/供应商。
- [HTTP 延迟 JSON](./http-latency.json)：本地 standalone + 内存 API；不含外网、PG、CDN 延迟。
- [机器摘要](./manifest.json)：门禁、镜像、阻塞和清理结果。

本批只清理明确创建的临时运行容器/网络和 Nginx 验收网络/卷；最终 PostgreSQL/Mailpit 专用验收容器已删除，数据不可恢复。未触碰用户已有 review、opera、ibooks 容器。五个验收镜像保留，供后续复核。
