# Release A 发布就绪审计

更新时间：2026-09-12（Asia/Shanghai）  
范围：测试网站 Release A；自动采集 Release B、广告联盟 Release C、头像识别均不在本次发布范围。

逐项产品初衷、实现入口和真实环境剩余事项以[Release A 需求与验收矩阵](./RELEASE_A_REQUIREMENTS_MATRIX.md)为权威索引；本文件继续负责 go/no-go 证据和发布结论。

2026-09-12 数据库测试补充：主图读取修复新增三组可执行 PostgreSQL 仓储用例，含合法/异常槽位前后对照、旧资产/对象/删除队列快照和精选图顺序/缺封面合同；已由现有 CI 数据库包运行入口覆盖。仅测试代码的该次增量完成 database 包编译、单元测试与 vet，以及独立只读评审；本机真实 SQL 子场景全部 SKIP。以下统一预检是该增量之前的记录，不替代待执行的 PostgreSQL 验收。

2026-09-12 主图读取增量：作品/人物摘要、关注列表、账号浏览历史、后台当前主图和替换事务重读已统一校验合法用途、主图标识和位置。历史异常媒体行不再被误投影为封面或头像；缺少合法主图时仍返回空图片并使用前端默认图。受影响 Go 测试、API 全量 test/vet 和产品合同 46/46 通过；随后统一预检再次通过 Go test/vet、OpenAPI、Markdown 65/408/0、TypeScript/ESLint、production audit 0 vulnerabilities、Ruff、18 文件 mypy、Python 299 passed/303 subtests/1 skipped、工具合同 62/62、前端单元 40/40、standalone HTTP 和 Go→Next 9/9。本轮显式跳过 build、性能、Playwright 与 Compose，沿用 9 月 11 日对应独立证据；没有新增 API 或迁移。见[主图读取防御证据](./evidence/release-a-media-primary-read-guards-2026-09-12.md)。真实 PostgreSQL 异常行和并发替换仍待目标环境验证。

2026-09-11 CSV 导出增量：运营后台既有错误 CSV 下载现在对前置控制空白以及 `= + - @` 公式前缀统一加文本标记，同时保留 BOM、RFC 风格引号转义、行号和服务端校验信息。导出只包含文件级错误与错误行，不包含通过行，不访问 URL 或执行原 CSV。新增 2 个纯函数行为单测并进入默认前端门槛；ops typecheck、ESLint、production build 和产品合同通过，见[CSV 错误导出证据](./evidence/release-a-csv-error-export-2026-09-11.md)。真实 PostgreSQL 批次提交仍待目标环境验收。

2026-09-11 运行时目录增量：已移除公开首页 API 故障时的 `TEST-001`～`TEST-004` 合成 fallback，并将首页、`robots.txt`、`sitemap.xml` 改为运行时生成，避免回环 API、构建机 `SITE_URL` 或合成目录进入部署镜像。生产站点身份只接受运行时 `SITE_URL` 的合法 HTTP(S) origin，最终公网要求 HTTPS；缺失/非法配置和只存在构建期 `NEXT_PUBLIC_SITE_URL` 时 robots/sitemap 返回 500，不生成错误 canonical。首页 API fetch 继续使用 300 秒 `public-catalog` 数据缓存，公共入口仍按 5 分钟策略缓存成功目录响应。两站 production build、构建产物扫描、standalone HTTP 正负向站点身份验证与真实 Go→Next 9 项缓存合同通过，见[运行时目录证据](./evidence/release-a-runtime-catalog-2026-09-11.md)。该修复不替代真实 API、最终域名和 Cloudflare 验收，公网仍 no-go。

## 当前结论

上一轮文档复核：64 份 Markdown、404 个本地链接、0 缺失。下段统一预检中的 403 个链接是补入 9 月 11 日图片槽位证据链接之前的实际运行结果；9 月 12 日文档增量以本轮链接检查为准。

最新验证（截至 2026-09-12）：9 月 11 日当前代码本地统一预检全部通过，包括 Go API/Worker test+vet、OpenAPI、64 份 Markdown/403 个本地链接、TypeScript/ESLint、生产依赖 0 高危、Ruff、mypy 18 文件、Python 299 passed/303 subtests/1 skipped、62 项工具合同、40 项前端单元、standalone HTTP 和真实 Go→Next 9 项缓存合同；两站 production build 已单独重建通过，Playwright desktop/mobile 为 78/78。图片槽位合同覆盖处理器、HTTP、仓储、公开详情和浏览器，详情页快速返回再次上报有桌面/移动证据；9 月 12 日继续补齐目录卡片、关注列表、账号浏览历史和后台主图读取防御，并通过受影响 Go 测试、API 全量 test/vet 和产品合同 46/46。新增 CSV 错误导出防公式注入及真实页面下载回归、对象存储任务清单、两地资源预算、Turnstile hostname/action 绑定和生产镜像目录隔离均进入门槛；隔离 standalone 还会丢弃来源副本的请求期 fetch cache，避免合成目录污染跨测试重启。根级、日本 edge+monitoring、北京三套 Compose 和五镜像 Bake 图此前已静态解析通过。补跑性能：首页/详情 desktop LCP 240/168 ms、mobile 208/148 ms，mobile CLS 0；首页 API/搜索 P95 15.4/8.17 ms。运营后台及公开账号页面请求级 CSP nonce 已通过 production HTTP、Nginx 静态合同及 Playwright；公开目录框架脚本 CSP 风险仍保留。全部是本机 production build + 合成内存 API 或静态部署解析，无网络限速；不替代真实数据库、邮件、R2/Cloudflare 或目标服务器。详见[9 月 11 日本地回归](./evidence/release-a-local-regression-2026-09-11.md)、[图片槽位与详情重入证据](./evidence/release-a-media-display-slots-2026-09-11.md)、[主图读取防御证据](./evidence/release-a-media-primary-read-guards-2026-09-12.md)、[CSV 错误导出证据](./evidence/release-a-csv-error-export-2026-09-11.md)、[运行时目录证据](./evidence/release-a-runtime-catalog-2026-09-11.md)、[Turnstile 证据](./evidence/release-a-turnstile-binding-2026-09-11.md)、[任务清单证据](./evidence/release-a-media-task-inventory-2026-09-11.md)、[北京资源证据](./evidence/release-a-beijing-resource-budget-2026-09-11.md)、[部署静态证据](./evidence/release-a-deployment-static-2026-09-11.md)与[账号页面 CSP 证据](./evidence/release-a-ops-csp-nonce-2026-09-11.md)，公网 no-go。

上一轮进展（2026-09-11）：一次性 `media-usage` 报告已增加账户 Storage Analytics 观测，按 `bucketName + datetime` 同时点跨桶求和并取 UTC 日峰值，以整数/十进制定点估算 30 日制 GB-month；缺 bucket/日期、重复、partial、截断或溢出均 fail-closed。Standard 免费额度比较必须由操作者显式 `--standard-only-confirmed`，且报告仍固定不是账单、不是供应商水位、不接执行或自动恢复。本轮未改 Schema/定时器；`mediausage` 44 个顶层/219 个子测试及 platform-worker test/vet/build 通过。真实 Analytics、类别、版本历史、共享应用和账单比对未验收，详见[该轮证据](./evidence/release-a-media-storage-analytics-2026-09-11.md)，公网 no-go。

上一轮进展（2026-09-11）：[上传容量保护](./MEDIA_UPLOAD_CAPACITY.md)现统计两个专用桶的全部当前对象与未完成 multipart 已上传部件；对象/upload/part 分页共享单次 256 个列表请求预算，缺权限、重复/矛盾证据、竞态、溢出或预算耗尽均 fail-closed。专项 53 passed/101 subtests，media-python 77 passed/118 subtests/1 skipped，最终仓库 Python 288 passed/288 subtests/1 skipped，全范围 Ruff、相关 mypy 和 50 份 Markdown/331 个本地链接检查通过；完整计数见[该轮证据](./evidence/release-a-media-multipart-capacity-2026-09-11.md)。真实 multipart、Linux 卷/网络、GB-month 账单、其他 bucket/共享应用的供应商完整性仍未验收，公网 no-go。

上一轮边缘进展：[动态默认图](./MEDIA_DELIVERY_MODE.md)和默认 off 的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)均已接入离线代码。人工 `default_only` 最高优先，动态 `enforce` 要求 observe；Go API 去除目录图片，Next Proxy 用 CSP 保护旧 ISR HTML，Worker 在读取对象缓存/R2 前读取独立鉴权策略，使 Release A 计划内的已知 URL和旧边缘对象也能 fail-closed 到默认图。内部路径不进 OpenAPI，生产 Nginx 只代理精确 edge policy。网关 8/8、前端/工具 31/31，Go API/Worker test/vet/build、lint/typecheck 和 OpenAPI 漂移均通过；边缘计数见[该轮证据](./evidence/release-a-media-edge-gateway-2026-09-11.md)。真实 bucket 私有化、Worker route/R2 binding/Cache API/purge 和 5 秒传播仍未完成。

上一轮页面进展：后台 `/media/upload-control` 已接[授权队列](./MEDIA_UPLOAD_QUEUE.md)，支持最近观察、独立确认、回执及原键重试；异常/过期状态禁止新操作。完整浏览器 70 项与截图见[历史页面证据](./evidence/release-a-media-upload-ui-2026-09-11.md)，本轮前端没有修改或重跑。Schema 保持 v21、62 表/5 视图；以下旧轮次记录不代表历史测试全部重跑。

2026-09-11 额度进展：手动/动态默认图、一次性操作/存储 Analytics 查询、持久操作量观察/可选定时器及[管理员复核与账期切换](./MEDIA_USAGE_REVIEWS.md)已开发，当前 Schema v21、62 表/5 公开视图。一次性存储估算不入库、不接恢复；日本持久观察默认 off。复核与上传执行分别保存请求/回执，账期切换保留旧证据并保持保护，普通低用量新样本不自动恢复。默认 off 的[定时全量对账用量准入](./MEDIA_TASK_ADMISSION.md)不发起被拒绝的跨区扫描，不阻断权利删除。**逐操作上传自动暂停、API/页面动态执行和 Cloudflare 私有 R2 全入口网关代码现已实现；可信账单/存储类别/版本历史/共享应用完整性及真实 bucket 私有化、Worker route/binding/purge 尚未完成验收。** Analytics 不是账单，M5 和公网仍 no-go。

本轮定时扫描准入验证：Go API/Worker vet/test、Worker 重建、4 组/30 子场景准入专项及实际本地 Go→Python 准入/必要删除通过；Python 228 项/141 subtests（1 skip）、ruff/15 文件 mypy、OpenAPI 漂移通过。扩展后的用量状态与对账调度两项 SQL 合同仍明确 SKIP。本轮前端未修改，没有重建或重跑浏览器；详见[本轮验证](./evidence/release-a-media-task-admission-2026-09-11.md)。

上一轮复核验证：Go API/Worker vet/test 和二进制重建、Python 225 项/138 subtests（1 skip）、ruff/15 文件 mypy、OpenAPI 漂移、两站类型/lint、运营站构建、15 前端烟测/56 工具合同、standalone HTTP 及桌面手机 54/54 浏览器回归通过。修正 API 状态行锁权限和刷新后旧待处理提示。用量 API 仓储/Worker 的三项真实 SQL 合同明确 SKIP；详见[该轮验证](./evidence/release-a-media-usage-reviews-2026-09-11.md)。

上一轮持久观察验证：Go API/Worker vet/test、重建/离线命令通过；mediausage 31 个顶层 + 121 个子测试、覆盖率 91.8%，真实 SQL 合同 SKIP。Python 全量 222 项 + 136 subtests（1 skip）、ruff/15 文件 mypy、OpenAPI、15 前端单元/56 工具合同及 standalone HTTP 通过。未重建 Next 或重跑浏览器/真实依赖，见[该轮验证](./evidence/release-a-media-usage-state-2026-09-11.md)。

此前主动停图轮次的 Go/类型/lint、公开站构建、Python 216 项 + 123 subtests（1 skip）、桌面手机 46/46、15 前端单元/56 工具合同和 standalone HTTP 均通过；本地正常模式 LCP/CLS 探针通过。API 只编译未运行，未启动真实依赖。证据见[主动停图验证](./evidence/release-a-media-delivery-2026-09-11.md)，不替代 Nginx/Cloudflare/源站或数据库验收。

2026-09-11 每日检查本地实现和验证见[新证据](./evidence/release-a-media-inspection-2026-09-11.md)。该轮引入 Schema v18、API 合同新增四个聚合字段（当前整体 v21）；M5 和公网仍 no-go，不以本机模拟测试替代真实环境。

最新对账复核：已复现并修复缺计数报告被当作全 0 完成，补完整/有界响应、计数与样本核验、Python 样本预算和拿调度锁后新快照。实际 Go→Python 对账合同通过，新并发调度 SQL 合同本机 SKIP；每日注册公开状态与两张固定默认图 HTTP 内容核验、独立审计与告警现已接入；新 SQL 合同和目标验证仍待完成。见[对账证据](./evidence/release-a-reconciliation-2026-09-10.md)。

最新主图进展：并发校验替换 API、后台独立确认、非共享旧公开图立即删除排队、私有母版 30 天保留以及共享旧资产保护已实现，接入既有逐对象删除与权利下架提前机制。接口成功只确认事务/排队，未确认物理删除。本地 Go/前端回归已开展，新增三组替换 SQL 与此前五组退出 SQL 均未在本机真实数据库执行；M5 整体验收和公网 no-go 不变。见[主图替换证据](./evidence/release-a-media-primary-2026-09-10.md)。

此前图片登记与逐对象退出的实现、问题和执行计数保留在[登记证据](./evidence/release-a-media-publication-2026-09-10.md)与[退出证据](./evidence/release-a-media-retirement-2026-09-10.md)。当时记录的替换开发缺口本轮已补，本地 conditional-go 仍仅适用于合成资料隔离测试，不代表真实图片生命周期已验收。以下较旧轮次计数是历史记录。

此前角色会话加固：真实角色更新、旧 session 撤销与审计同事务；相同角色 409 且不再撤销，新登录保留 user 30 天/运营 8 小时。Go vet/test、事务/HTTP 专项、运营站 build、两站类型/lint、32 项桌面/手机浏览器回归通过。两组新增 PostgreSQL 合同仍明确 SKIP，真实撤销/行锁保证待验收，公网仍 no-go。详见[角色会话证据](./evidence/release-a-role-sessions-2026-09-10.md)。以下为先前阶段记录。

此前密码护栏：已实现单进程 1 计算/2 等待/500ms，所有昂贵认证路径共用限制，过载返回 503 AUTH_BUSY 并保留重试条件；邀请先预检、后计算、最终事务重验，新增私有聚合指标。该轮 Go vet/test、专项、OpenAPI 漂移和 Python 174 项 + 78 个 subtests 通过；真实 Argon2 突发计算峰值为 1。邀请 SQL 合同实际用例因缺数据库而 SKIP；目标 384 MiB/0.40 CPU 下资源验收仍未完成，角色策略已在最新一轮实现、SQL 待验证。详见[密码护栏证据](./evidence/release-a-password-work-2026-09-10.md)。

- **本地代码与静态部署包：conditional-go。** 本轮范围与最终计数见[multipart 容量保护验证](./evidence/release-a-media-multipart-capacity-2026-09-11.md)。下面旧轮次的 SQL、视觉、性能及依赖审计证据保留原适用范围，不冒充本轮真实环境验收。
- **真实环境完整验收：未完成。** 可以使用合成资料进入隔离测试环境，但尚未在目标 PostgreSQL、邮件、S3/R2、北京副本、Cloudflare 和最终域名上完成端到端验收。
- **公网开放：no-go。** 后台网络层限制、最终权利邮箱，以及最终域名/真实目标数据下的桌面、移动视觉与性能门槛尚未验收；本地合成构建视觉与实验室性能证据已通过。

“conditional-go”只表示代码可以进入真实测试环境，不表示已经达到公网开放条件。

此前退出补充证据：已复现并修复 API 撤销错误仍返回 204、两站错误吞噬和 localhost 跳转；8 个 Go HTTP 子场景及四组新增跨站浏览器场景通过，完整套件 32/32。保持后台 no-referrer，以严格 Fetch Metadata 同源导航例外兼容原生表单 null Origin；一般空/跨站请求仍拒绝。失败状态的四张桌面/移动截图已复核。真实 SQL 新增幂等退出合同，目前 5 组仍 skip；需入口规则、全部 API 和两站配套升级。见[退出确认验证](./evidence/release-a-logout-confirmation-2026-09-10.md)。当时登记的密码计算护栏与角色会话策略均已在后续轮次完成本地实现，实际数据库/资源验收仍未完成。

上一轮账号补充证据：登录快照、通用 HTTP 401 和测试库守卫专项通过，Go API/Worker vet/test、全套 Python 171 项 + 73 个 subtests、11 项 CI 安全合同和相关 ruff 通过。登录签发锁账号并核对密码/邮箱/角色/状态，重置挑战绑定原用户 UUID，所有会话撤销路径保护创建/撤销时间约束；该轮风险来自代码审查，最初 4 组真实库合同已接现有 CI migrations，但本机明确 skip、远程未触发。该轮没有迁移或公开 API 变更。见[账号生命周期证据](./evidence/release-a-identity-lifecycle-2026-09-10.md)；其中登记的退出错误传播已在本轮修复，其他待办仍未完成。

最新租约补充证据：已复现并修复 Dispatcher 过期后仍完成并继续执行同批下一条的问题，8 项专项、Go API/Worker vet/test、Go→Next 7 场景、全套 Python 170 项 + 73 个 subtests 和 10 项 CI 安全合同通过。数据库落库增加原领取次数与截止时间校验，媒体完成先锁 outbox 行，耗尽次数不再递增越过约束；这部分真实 PostgreSQL 合同已接 CI，但本机明确 skip，不能据此放行目标数据库。见[Worker 租约证据](./evidence/release-a-worker-lease-2026-09-10.md)。

最新缓存补充证据：真实 Go→Next production build 的 7 个运行态场景通过，覆盖首页/详情/sitemap、未失效与错误/过期签名负向对照、有效发布、隐藏及重复事件。测试现使用独立临时 standalone 副本；缓存合同前后 HTTP 冒烟均通过，源首页缓存 SHA-256 不变。Go API/Worker vet/test、全套 Python 169 项 + 73 个 subtests、54 项工具合同、13 项前端烟测、公开站 build 和全范围 ruff 同轮通过；不把合成目录 API 替身当作 PostgreSQL/真实业务 API 联调。见[缓存运行态证据](./evidence/release-a-cache-contract-2026-09-10.md)。以下较小测试计数保留为先前执行的历史记录。

最新补充证据：Worker 完成回执修复后，Go API/Worker vet 与测试（显式启用 Go→Python 实际 HTTP 合同）、全套 Python 168 项与 73 个 subtests、相关 ruff 和媒体代码 mypy 均通过。此前的 46 项工具合同、核心 CI 运行器和编译证据继续保留；上文 144 项是早先统一预检的历史计数。此次只运行本机测试 HTTP 服务和临时文件，真实 PostgreSQL retention 合同及核心服务 CI 作业仍待执行，也不代表 S3/Cloudflare 或 Next 缓存的目标联调已经通过。最新命令与证据边界见[Worker 完成回执验证](./evidence/release-a-worker-ack-2026-09-10.md)，历史记录见[核心 CI 门槛本地验证](./evidence/release-a-core-e2e-preflight-2026-09-10.md)和[备份验证记录](./evidence/release-a-backup-2026-09-10.md)。

## 证据矩阵

新增 `core-e2e` 真实服务 CI 门槛已接入镜像交付依赖，覆盖 PostgreSQL 16/Mailpit/Go API 的账号与异人审核发布闭环；同时修复邀请 E2E 创建邀请前遗漏近期密码确认的问题。当前只有运行器、CI 配置和离线合同，本机未启动真实服务、也未触发远程 CI，因此下表的真实联调项目仍为条件通过/待执行。该作业不覆盖 Worker、前端真实 API 集成、Turnstile、S3/R2 或 Cloudflare。

| 范围 | 当前证据 | 结论 | 仍需完成 |
| --- | --- | --- | --- |
| 产品边界 | 公开站页脚、广告占位、默认图及安全产品合同通过；Go Worker 对 `COLLECTION_ENABLED=true` 启动失败；HTTP 冒烟拒绝常见联盟脚本、active/floating/countdown/popunder 信号 | 通过 | 最终权利与 18+ 文案确认 |
| Go API/Worker | `go test`、`go vet` 全部通过；关键权限、状态机、outbox、归档、媒体对账均有测试；87 个实际路由与 OpenAPI 操作双向一致，路径参数和 operationId 合同完整 | 通过 | CGO 环境执行 race test |
| 审核发布 | 录入、revision、领取、显式改派、批准/驳回、发布、隐藏、回滚、合并、下架代码完成；核心作品链已有参数化 E2E 脚本且离线调用合同通过 | 条件通过 | 在目标 PostgreSQL 用两个不同运营账号执行 E2E 脚本 |
| 审核改派 | admin/owner、active reviewer、事务锁、旧/新审核人审计、近期密码和后台选择器已实现 | 条件通过 | CI/目标库执行 PostgreSQL repository 合同 |
| 账号与邮件 | 注册、重置、关闭、邀请、验证码限流和枚举防护有本地合同；登录快照原子签发、重置 UUID 绑定、撤销时间保护及通用 401 回归通过；含幂等退出的 5 组 SQL 生命周期合同纳入受限 API 的 CI migrations，本机 skip | 条件通过 | 密码计算护栏与角色撤销已实现，目标资源边界待验；执行真实 PostgreSQL 行锁/邮箱复用/撤销合同和 Mailpit/SMTP E2E |
| 退出登录 | API 8 个子场景通过；两站只认 204、失败保留 Cookie 与重试页、3 秒截止时间、拒绝重定向、Host/Origin 与 no-referrer 同源导航兼容均有实际浏览器/HTTP 证据；GET 不退出、无会话不依赖 API | 条件通过 | 入口规则 → 全部 API → 两站配套升级；在真实 Go/PostgreSQL 与最终 Nginx/Cloudflare 复验失败/恢复、旧 token 失效、首次撤销证据与缓存绕过 |
| 搜索与 SEO | 番号归一、最新/热门、详情、canonical、robots、拆分 sitemap 已实现；核心 E2E 会验证发布后番号搜索/详情/sitemap 及隐藏后不可见；Nginx 公开目录 HTML 缓存 5 分钟，账号/搜索/API no-store | 条件通过 | 目标库执行 E2E、验证发布缓存失效和最终 Cloudflare 规则 |
| Go→Next 缓存运行态 | 真实 Go 处理器签名、Next 生产接收器、首页/详情 HTML 与 sitemap 的 9 个场景（含仅新增图片、不改文字版本）通过；负向对照不回源，有效刷新后回源，TTL 到期前更新；独立可写副本避免测试污染共享 Next 数据缓存，统一预检/CI web 强制执行并保存脱敏证据 | 条件通过 | 真实 API/数据库 outbox、Nginx/Cloudflare 和多实例传播验收；本机只验证单实例 Next 与合成目录 |
| 图片 | 处理/派生/default/容量/登记、v17 公开隔离、逐对象退出、主图替换/共享/30 天保留、严格对账确认、拿锁后快照和每日注册状态/默认图独立检查均有本地代码；Go→Python 对账/删除合同通过 | 未完成整体验收 | 执行新增检查状态矩阵/并发调度、存储对账调度并发、三组替换、五组退出、登记/隔离 SQL，以及真实双 bucket/Cloudflare/北京副本生命周期 |
| 图片额度/降级 | 双桶当前对象与未完成 multipart 部件准入、256 列表请求预算、中断保护、人工及动态 API/Next 默认图、edge no-store、账户操作量观察/复核/定时器/告警与账期切换已实现；一次性报告另有全账户已观测 bucket 的 UTC 日峰值/GB-month 整数估算，Standard 比较需显式操作者确认且不持久化。已接定时全量对账与逐操作上传准入/系统暂停，手动 Go→Python、v21 授权队列/操作者审计/可信回执及后台独立确认页面均已实现。Cloudflare Worker + 私有 R2 binding 网关及鉴权 Go policy endpoint 已完成离线代码，策略在对象缓存之前，全部执行器默认 off；[任务清单](./MEDIA_STORAGE_TASK_POLICY.md)确认唯一非必要自动对象任务是全量对账 | 部分实现，no-go | 验证真实账单、存储类别、供应商版本历史、未返回 bucket/共享应用完整性；执行真实 SQL、Analytics、Linux 卷/SDK/multipart，并完成派生 bucket 私有化、关闭匿名 R2 入口、Worker route/Cache API/purge、Nginx/Cloudflare 与 5 秒传播验收，不承诺零费用 |
| Worker 完成回执 | 已复现并修复 HTTP 200 HTML 被误判完成；30 个回执场景、缺少处理器拒绝和真实 Go→Python HTTP 失败/恢复合同通过；事件及 scope/key 精确绑定，4 KiB/UTF-8/JSON 限制，CI Go/race 启用跨语言合同 | 条件通过 | 先北京媒体服务后日本 Worker 配对升级；目标 PostgreSQL outbox、真实 S3/Cloudflare 与 Next 缓存失效复验 |
| Worker 租约与重试 | 过期仍结算/继续执行已用可控时钟复现并修复；8 项本地专项通过。SQL 绑定原始 attempt_count 和有效租约，媒体先锁任务行，10 次耗尽转 dead；5 类真实库合同进入 CI migrations | 条件通过 | 独立 PostgreSQL 16 执行旧领取拒绝、退避、崩溃耗尽、并发 Claim 和媒体行锁竞态；本机没有实际 SQL 证据，升级不能混跑旧消费者 |
| 前端与交付工具 | 76 个 OpenAPI path、106 个组件 Schema 确定性生成 TypeScript；上传控制状态/独立确认/回执页面与 CSV 错误文件安全下载已实现；当前前端单元 40 项、工具合同 62 项通过，39 场景 × desktop/mobile 共 78 项浏览器通过；两站 typecheck/lint/production build、standalone HTTP、缓存合同、本地性能/P95 及 Markdown 本地链接门槛均通过 | 条件通过 | 真实 API/邮件/数据库下复验写入与异步结果，在最终 HTTPS origin 重跑浏览器与性能探针；模拟数据不替代真实联调 |
| PostgreSQL | 21 个迁移、62 张表/5 个公开视图的数据模型清单、权限和 shell roundtrip 脚本存在；静态合同防止迁移与清单漂移；CI 已接 repository 合同；本地/日本模板记录 500ms 慢语句但禁止参数值写日志，运行账号有 statement/lock timeout；合成 seed 需要 Schema v21、双确认、精确内部标志和空/fixture-only 数据守卫，加载时以 5 秒超时锁住 13 张写入表并忽略用户 `psqlrc`；roundtrip 已配置验证双次幂等及脏库拒绝，固定审计账号关闭且不可登录 | 条件通过 | 在 CI/日本 PostgreSQL 16 实际执行 v21 fresh/upgrade/down 与公开图片父资料隔离合同、受控 fixture loader、运行账号、慢查询日志检查和恢复演练 |
| 部署 | 本地、日本、北京 Compose `config --quiet` 全部通过；日本完整 runtime 限制在 2 CPU/约 2.9 GiB，北京两个常驻服务限制在 1.45 CPU/2240 MiB并统一日志轮转；镜像使用非 root 运行用户并声明本地健康检查；API 镜像内置非交互 `/create-owner`，日本 `tools` profile 可在迁移和运行账号配置后一次性创建首个 owner，无需宿主机 Go/源码；`.dockerignore` 排除密钥/缓存/运行数据；Bake 统一 5 个镜像和 OCI version/revision，CI 在全部质量门槛后构建并保留 image inspect 清单；env 校验器检查两地配置漂移；部署探针只读核对入口、版本、ready、安全头、缓存和广告边界，两者均不输出凭据 | 条件通过 | CI/目标主机完成一次真实镜像构建，对服务器私有 env 执行 core/full 检查，并在真实入口运行部署探针、最大图片/并发任务和资源占用观察 |
| 备份 | 复制/恢复证据状态机、目录锁、同秒防覆盖和安全轮转已实现；正常保留最新 4 组，最后已验证恢复点与未验证/异常文件受保护，超额标记 deferred；完整计划通过本地与数据库证据校验后才删除 | 条件通过 | CI/目标 PostgreSQL 执行新增 retention SQL 合同；目标每 7 天调度、北京副本、补验/轮转、遗留锁恢复和恢复演练 |
| 监控 | API/Worker 指标、Schema v21、每日图片检查、账户用量复核/停用建议、动态展示策略可用性、edge policy enabled、outbox、邮件、媒体、备份规则已实现；API 延迟使用有界标签的固定桶 histogram，公开目录与搜索 P95 告警含低流量门槛；pgxpool 导出连接数/利用率/获取计数；日本 Worker 导出 poll 心跳、最后成功、当前租约和连续失败，连续 3 次 poll 失败会 not-ready 并触发 critical；最小化 node-exporter 提供日本 CPU/内存/根磁盘指标；Prometheus 已接 Alertmanager，critical 与 warning/info 分流，SMTP 密码使用独立私有文件，本地配置不外发 | 条件通过 | 目标 Linux/PostgreSQL 主机验证全部 scrape 与规则加载；启用网关时确认 `self_deepsearch_media_edge_policy_enabled = 1`，并验证策略 normal/default_only/依赖错误与 5 秒传播；受控触发 Worker poll 连续失败及恢复、API/搜索 P95、5xx 与连接池告警，验证用量状态缺失/过期/停用建议三条观察告警及动态策略不可用告警、磁盘/内存/CPU 阈值表达式及 warning/critical/resolved 邮件实际送达 |
| 安全 | 角色、20 个高风险操作近期密码合同、Origin、no-store、日志脱敏与安全头合同通过；Turnstile 注册/重置 action、响应 hostname、大小/类型/超时和拒绝重定向合同已完成本地验证；运营后台和公开账号页面使用请求级 nonce + `strict-dynamic`，全站显式禁止脚本属性事件处理器；公开目录框架脚本仍保留 `unsafe-inline`，该风险与首页运行时渲染分开评估。此前生产依赖 high/critical 为 0 且 CI 阻断回归；退出、密码资源护栏和角色会话撤销已有本地实现 | 条件通过 | 在最终 HTTPS 域名完成真实 Turnstile site key/hostname/action 验证；完成公开目录 CSP 与认证 P1 的最终入口验证，以及 Cloudflare、固定 IP/VPN/Access、安全联系人和真实环境复核 |

## 已验证命令

```powershell
$env:PYTHON='C:\path\to\python.exe'
npm run preflight:release-a
npm run preflight:release-a -- --with-e2e
npm run audit:production

docker compose config --quiet
docker compose --env-file infra/compose/japan/.env.example `
  -f infra/compose/japan/compose.yaml --profile edge --profile monitoring config --quiet
docker compose --env-file infra/compose/beijing/.env.example `
  -f infra/compose/beijing/compose.yaml config --quiet
```

统一预检默认不启动 Docker，覆盖 Go test/vet、OpenAPI TypeScript 生成漂移、TypeScript、ESLint、Python ruff/mypy/pytest、两站 production build、40 项前端单元烟测、standalone HTTP 冒烟和真实 Go→Next 9 项缓存合同。`requirements-test.txt` 固定 ruff `0.16.6`，`ruff.toml` 固定 `E4/E7/E9/F/I` 和 120 列宽，避免不同 ruff 版本或默认规则扩张导致结果漂移。运行态会从两站 HTML 提取实际 stylesheet URL，并验证 HTTP 200、`text/css`、非空响应及 CSP/反嵌入等浏览器安全头；本地 mock API 还提供三类详情，验证 JSON-LD 合法、canonical 存在和脚本闭合片段被转义。生产依赖审计需要 npm registry，因此由独立命令和 CI 门槛执行。

2026-09-09 本机补充复核使用干净 `.venv` Python 3.12.14 完成统一预检，Go/Node/前端/依赖审计、固定规则集 ruff、mypy、130 tests + 54 subtests、production build 和 standalone HTTP 冒烟均通过；三套 Compose `config --quiet` 此前也已通过。Go race test 仍因本机缺少 `gcc` 未能执行，应在 CI 或目标服务器复跑，但不再影响“统一预检已通过”的事实记录。

同一 Python 3.12.14 环境已一次性通过 collector 5、media-python 16、db 17、backup 4、reverse-proxy 13、security 52、scripts 23，共 130 项；ruff 同轮通过。

2026-09-08 浏览统计补充复核：作品、人物和厂牌详情仍按每次进入上报；React Strict Mode 重挂在 1 秒窗口内去重，去重键随后按原时间戳安全清理，稍后再次进入仍可统计。HTTP 回归确认游客不写用户历史、有效登录写入精确 UTC 时间历史。受影响的 TypeScript、ESLint、两站 production build、52 项安全/产品合同、13 项前端单元烟测和 standalone HTTP 冒烟均通过；真实数据库落库语义仍列入目标环境 E2E。

2026-09-08 架构一致性复核：文档已从未采用的 `pgx + sqlc` 修正为代码实际使用的 `pgx + 类型化 repository + 参数化 SQL`，保留未来逐模块引入 sqlc 的演进点。生产部署由仓库内 `psql` 迁移器读取 SQL 的 goose Up/Down 分段，不依赖 goose CLI；迁移器持有 advisory lock，要求连续版本、逐版本事务并校验 Schema 版本写入。本地和日本 PostgreSQL Compose 新增 500ms 慢语句记录，且固定 `log_parameter_max_length=0`，不记录邮箱、搜索词、标题等绑定参数值；三套 Compose 静态渲染、17 项数据库合同和 52 项安全合同通过。

2026-09-08 数据模型交付复核：新增 [DATA_MODEL.md](./DATA_MODEL.md)，覆盖 v1-v16 迁移中的三 Schema、56 张表和 5 个公开视图，并用数据库合同自动检查清单漂移；数据库迁移与数据模型离线合同现为 17 项并通过。产品目标中的搜索结果点击率尚未定义匿名去重、分页和缓存返回口径，Release A 当前只提供不含查询词/标识的搜索请求与零结果聚合，不能据此宣称 CTR 已可用。

2026-09-08 OpenAPI 类型复核：移除手工维护的前端合同文件，新增本地确定性生成器；当前从 70 个 paths、91 个组件 Schema 生成 TypeScript，包含反馈列表、冲突裁决枚举与审计查询响应命名 Schema。生成器 `--check` 在统一预检的 TypeScript 之前执行，并进入 CI；三套 TypeScript、ESLint、两站 production build 和 standalone HTTP 冒烟通过。

2026-09-08 部署变量复核：新增合同要求日本、北京 Compose 的全部变量引用都出现在对应 `.env.example`，并拒绝重复键；加入一次性 owner、fixture 与 Alertmanager 私有文件变量后，当前分别覆盖 60 和 20 个引用，避免部署时才发现参数清单遗漏。TypeScript 增量构建缓存已加入 `.gitignore`，不进入交付物。

2026-09-08 核心目录 E2E 工具复核：新增不含内置凭据的参数化脚本，自动创建唯一合成作品，并使用不同账号完成审核、近期密码确认、发布、番号搜索、详情/sitemap 与隐藏后不可见验证；异常时尽力隐藏已发布记录。离线合同已通过，真实 PostgreSQL/API 执行仍是发布门槛。

2026-09-08 E2E 凭据复核：邮件邀请流程不再接受 owner 或邀请账号密码作为命令行参数，改用本机环境变量；参数缩写已关闭，默认临时注册密码每次随机生成，减少测试凭据出现在进程列表或被重复使用的风险。

2026-09-08 客户端 IP 信任链复核：修复 Cloudflare 客户端地址在 Nginx → Next.js 同源代理 → Go API 之间丢失的问题。Nginx 覆盖三个入口的 `CF-Connecting-IP`，前端代理只继续传递该受控头，Go 仅在生产可信代理开关开启时采用；新增反向代理合同和 Go HTTP 回归后，TypeScript、ESLint、production build、standalone HTTP 冒烟均通过。生产仍必须以防火墙限制源站只接受 Cloudflare 出口。

2026-09-08 URL 与部署区域一致性复核：发布队列和核心目录 E2E 默认使用 `{可搜索名称}-{实体末 8 位}` 的稳定 slug，作品以番号为首段；保留人工覆盖和现有测试 URL 兼容性。架构文档同时修正为日本 `ops-web` 同源代理内部 API、北京仅承担 Worker/媒体职责，与当前 Compose 和代码一致。

2026-09-08 结构化数据复核：作品、人物、厂牌详情新增 `CreativeWork`、`Person`、`Organization` JSON-LD，使用绝对 canonical 和已发布字段；统一组件转义 `<`、U+2028、U+2029，防止资料内容打断脚本。13 项单元烟测会解析目标 JSON 文档并拒绝未转义 `<`；standalone HTTP 冒烟使用包含 `</script>` 攻击片段的合成标题验证实际构建产物。类型检查、ESLint、两站 production build、安全合同与 HTTP 冒烟通过；仍需真实数据页面使用搜索引擎测试工具复验。

2026-09-08 浏览器 E2E 复核：新增 Playwright 与有状态本地 mock API，真实 Chrome 下将匿名番号搜索→详情、5 个广告占位、JSON-LD/XSS、注册验证码→session→退出和后台错误密码→管理员登录 3 个场景分别运行在 1366×900 与 390×844，共 6 项通过；同时断言无横向溢出以及左右广告 rail 在移动端隐藏。CI 安装 Chromium 后强制执行。测试发现 App Router 持久布局可能保留认证前账号状态，登录、注册、接受邀请和关闭账号现统一在成功后完整导航。真实 SMTP/PostgreSQL 下的邮件与运营写入仍由目标环境 E2E 验收。

2026-09-09 账号浏览器 E2E 补充复核：有状态 mock 增加密码验证码/重置、关闭账号、收藏、关注、隐藏、历史和反馈路由；浏览器新增验证码与注册用户个人功能场景。六个场景在桌面与移动视口共 12 项全部通过，并确认关闭后 session Cookie 消失、页头恢复游客登录入口，历史清除后页面不可见且保留说明仍展示。静态安全合同锁定新增场景与 mock 路由；真实邮件内容和数据库状态仍由目标环境 E2E 验收。

2026-09-10 运营浏览器 E2E 补充复核：覆盖作品审核发布、邀请与用户权限、admin 按 Request ID 查询最小披露审计时间线、反馈三角色流转，并包含权利请求登记、editor 只读、admin 近期密码执行以及公开番号搜索立即撤下的跨站闭环。完整套件还锁定搜索 API `no-store`、移动端操作列、认证导航和响应式长文本边界。十二个场景在桌面与移动共 24 项通过；真实 PostgreSQL 事务、邮件、outbox、S3/R2 物理删除与 Cloudflare purge 仍由目标环境 E2E 验收。

2026-09-10 回归复核：最新执行 `npm run preflight:release-a -- --skip-build --with-compose`，统一预检状态为 `passed`；同一轮包含 144 项 Python 测试与 54 个 subtests、46 项工具合同、13 项前端烟测、standalone HTTP 冒烟和三套 Compose 静态解析。当前 production build 此前已成功生成，本轮按参数跳过重建；桌面/移动 24 项 Playwright、12 张标准视觉截图与两类性能探针沿用同日通过的独立证据。该证据仍是本地合成数据/静态部署包证据，不替代目标 PostgreSQL、Mailpit/SMTP、S3/R2、Cloudflare 和最终域名联调。

2026-09-10 浏览器性能复核：新增 `release_a_performance_probe.mjs`，落实产品定义的 LCP `<2500ms` 与移动 CLS `<0.1` 门槛。默认本地模式对公开首页和正常合成作品详情分别在 1366×900、390×844 视口预热一次后采样 3 次，记录 LCP、CLS、FCP、TTFB、DOMContentLoaded/load、资源数量和字节；最新统一预检的四组 LCP 中位数为 156–352ms，桌面 CLS 最大值约 0.00156、移动为 0，[JSON 证据](./evidence/release-a-performance-2026-09-10.json) 状态为 passed。远程模式要求不含凭据/路径/查询的 HTTPS origin 和显式已发布作品路径，并可由统一预检 `--with-performance` 调用；本地无网络限速与内存合成 API 结果不代表 Cloudflare 缓存命中、真实用户场数据或目标服务器资源已经验收。

2026-09-10 HTTP 延迟复核：新增 `release_a_http_latency_probe.mjs`，通过公开站同源代理对首页 API 和番号搜索 API 各预热 3 次、顺序采样 20 次，完整消费响应后计算 nearest-rank P95；同时要求 HTTP 200、`application/json`、有效 JSON 和 2 MiB 响应上限。最新统一预检的本地合成报告中首页 API P95 为 11.35ms、搜索 P95 为 6.30ms，分别通过 `<300ms`、`<500ms` 门槛；[JSON 证据](./evidence/release-a-http-latency-2026-09-10.json) 只保存番号 SHA-256、字节和耗时，不保存番号原文或响应正文。远程模式要求 HTTPS origin 与显式已发布番号。46 项工具合同通过；本地 mock、回环网络和顺序请求不代表真实 PostgreSQL/Cloudflare、服务器端路由直方图或并发负载已经验收。

2026-09-10 部署工具复核：API 镜像增加静态 `/create-owner`，日本 Compose `tools` profile 增加一次性 `owner-bootstrap`，等待 v16 迁移和运行账号配置后使用 `platform_api_login` 创建或轮换唯一 owner；已有不同活动 owner 时拒绝。引导邮箱/密码只由受控终端临时传入，环境校验器验证邮箱、12～128 位密码及不可复用且不输出值。统一预检同时修复 Windows 无全局 `python` 时未回退仓库 `.venv` 的问题，并使用隔离 Docker 配置静态验证日本 `edge`、`monitoring`、`tools` 全部 profile。包含 130 项 Python、54 个 subtests、46 项工具合同和三套 Compose 的 `--skip-build --with-compose` 结果为 passed；没有启动容器，真实镜像执行仍待目标主机验收。

2026-09-10 fixture 安全复核：内部确认由“变量存在”改为只接受精确值 `1`，生产加载器增加 `--no-psqlrc`，并在单事务内对 13 张会被写入的表申请 `SHARE ROW EXCLUSIVE` 锁和 5 秒 lock timeout。5 项执行级 Shell 测试及数据库静态合同通过；PostgreSQL 16 roundtrip 已配置验证错误内部值拒绝、首次/重复生产加载器导入数量稳定、加入非 fixture 来源后拒绝。最新不启动 Docker 的 `--skip-build --with-compose` 统一预检为 passed，包含 136 项 Python 和 54 个 subtests；本机未执行真实 `psql`，因此该 roundtrip 仍是 CI/目标环境待产出的证据。

2026-09-10 监控通知复核：修复当前 `master` 分支 push 不会触发仅监听 `main` 的 CI 问题；本地与日本 monitoring profile 增加 Alertmanager，Prometheus 固定投递到内部 9093，生产示例按 critical 与 warning/info 分流并发送恢复通知。SMTP 密码只通过独立私有文件挂载，env 检查会拒绝占位地址、内联密码、缺失路由和过短密码文件；相关静态合同、两套 Compose 解析和包含 136 项 Python 的统一预检通过。Alertmanager 镜像、真实 SMTP 连接和告警送达仍需目标主机验收。

2026-09-10 API 延迟监控复核：请求耗时指标不再把只有 `_sum/_count` 的数据声明为 summary，而是输出 10ms～5s 的累积 histogram buckets，并在 300ms/500ms 设置与 Release A 门槛对齐的边界；未知 HTTP method 统一归入 `OTHER`，route 继续使用 chi 模板，因此 slug、番号和查询词不会制造高基数或泄漏。告警规则分别聚合公开目录成功 GET/HEAD 与规范搜索路由 `/api/v1/search/works`，10 分钟窗口至少达到 20/10 次请求且 P95 连续 10 分钟不达标才告警。Go 专项测试、监控静态合同以及包含 140 项 Python、54 个 subtests 和三套 Compose 静态解析的完整无 Docker 预检通过；本机没有 `promtool` 且按要求未启动容器，真实规则加载与告警触发仍是目标环境门槛。

2026-09-10 主机饱和度监控复核：本地与日本 monitoring profile 增加 `prom/node-exporter:v1.9.1`，只启用 CPU、meminfo、filesystem collector；生产模板以 UID/GID `65534`、只读挂载、`cap_drop: ALL`、`no-new-privileges`、64 pids 和回环 9100 约束其权限。Prometheus 新增 exporter 可用性、根分区 80%/90%、内存 80% 和 CPU 85% 告警。日本 Prometheus/node-exporter CPU 上限合计仍为 0.10，监控全开后总上限约 2.0 CPU/2.9 GiB。专项安全合同、告警合同和两套 Compose 静态解析通过；本轮没有启动镜像，必须在目标 Linux 主机确认 root mountpoint 标签、真实数值和通知链路。

2026-09-10 数据库连接监控复核：新增 transport-neutral 的 pool stats 合同，数据库 Store 从 pgxpool `Stat()` 读取 acquired/idle/total/max 和 acquire 计数，API `/metrics` 导出连接池利用率与等待/取消信号，全程不执行额外 SQL。`PlatformDatabasePoolUtilizationHigh` 在利用率 `>=80%` 持续 10 分钟时发送 warning。专项 Go 与 4 项监控合同通过；真实 10 连接池、并发请求、statement timeout 和目标 PostgreSQL 资源下的阈值仍需联调。

2026-09-10 Worker 调度监控复核：日本 Worker 的 outbox dispatcher 记录每轮 poll 心跳与成功/失败、最后成功 poll、当前 claimed batch 和事件结果；连续 3 次 poll 失败时 `/readyz` 返回 503，成功 poll 后恢复并清零。`PlatformWorkerPollFailures` 对该状态持续 2 分钟发送 critical，指标标签保持低基数且不包含事件或用户标识。最新完整无 Docker 预检通过，包含 Go test/vet、144 项 Python、54 个 subtests、46 项工具合同、13 项前端烟测和三套 Compose 静态解析；真实 PostgreSQL 故障与恢复、规则加载和通知送达仍待目标环境。

2026-09-10 审计查询复核：新增 admin/owner 专用 `GET /admin/v1/audit-logs?request_id=...` 和后台“审计查询”页面，editor 被拒绝。输入只接受 1～128 位有限 ASCII Request ID，精确匹配并按时间正序返回最多 200 条；响应只包含操作者、动作、对象、理由和时间，不披露 revision 前后快照或原始 metadata。实现复用 `audit_logs_request_idx`，无需迁移。最新不启动 Docker 预检通过 144 项 Python、54 个 subtests、46 项工具合同、13 项前端烟测和三套 Compose 静态解析，桌面/移动 Playwright 24/24 已同日通过；真实 PostgreSQL 权限、索引计划与目标数据查询仍待联调。

2026-09-10 备份轮转复核：替换 35 天 mtime 近似策略后，进一步修复同秒覆盖、并发作业和无验证证据直接清理的风险。`RETENTION_COUNT=4` 现在是正常保留目标，不是无条件硬删上限：最新完整已验证恢复点与未验证/异常文件受保护，超额输出 deferred；生成完整清理计划期间 SQL 失败不会删除旧文件，completed 后的轮转失败也不会篡改备份状态。新增执行级故障与时钟回拨用例，并固定 Windows 日期 mock 优先级。真实 SQL 合同已加入 CI 调用的 PostgreSQL roundtrip，覆盖 completed/copied 不能冒充 verified、路径/哈希/字节不匹配、复制/恢复摘要缺失和备份区域/类型边界；本机只做离线测试，不能把脚本存在作为真实 PostgreSQL 通过证据。

2026-09-09 开发命令复核：`npm run check:api-types` 和统一预检均通过 `scripts/run_python.mjs` 自动选择仓库 `.venv`，不再要求 Windows 主机预先提供全局 `python`；`npm run test:preflight` 当前为 46 项通过，并覆盖显式 `PYTHON`、虚拟环境回退、可选浏览器/性能门槛、本地预览端口、视觉捕获、浏览器性能与 HTTP P95 的 HTTPS/隐私/输出约束。

2026-09-09 依赖安全复核：npm registry 新增的 Next.js/sharp advisory 曾使审计失败；现已升级至 `next 16.3.3`、`eslint-config-next 16.3.3`、`sharp 0.35.4`，三个 workspace 的 package.json、lockfile 和安全合同一致，生产审计恢复为 0 漏洞。

2026-09-09 lockfile 复核：`npm ci --dry-run --ignore-scripts` 成功解析升级后的依赖树；未执行容器启动。当前本机没有 Docker daemon，因此 Compose 仍只做静态渲染，不把容器运行态当作已验收。

2026-09-09 镜像构建复核：公开站和运营后台 Dockerfile 均复制根 `package-lock.json` 并执行 `npm ci`，新增安全合同阻止回退到 `npm install`；生产审计和 lockfile dry-run 通过。镜像实际构建仍需 CI/目标主机的 Docker 运行环境。

2026-09-09 健康检查复核：Go API/Worker 的 `--healthcheck` 访问本地 `/readyz`，将数据库和 Schema 就绪纳入健康状态；公开站首页、后台登录页和媒体 `/health/live` 分别由镜像内回环探测。公开站/后台等待 API healthy，日本 Worker 等待 PostgreSQL healthy，日本 edge 等待 API 和两个前端全部 healthy，Prometheus 等待 API 与 Worker healthy。新增安全合同、Go 命令单测和三套 Compose 静态渲染通过；容器实际启动与资源占用仍需 CI/目标主机验证。

2026-09-09 部署环境复核：新增不依赖 Docker 的 `release_a_env_check.py`。core 模式验证日本核心生产变量，full 模式交叉验证日本/北京版本、媒体密钥、公开图片基地址、双 bucket 与 10 GiB 上限，并可检查证书、私钥和 metrics token 文件；错误信息不包含变量值。5 项专项测试、ruff、13 个生产 Python 源文件 mypy、130 tests + 54 subtests 通过。真实私有 env 尚未提供，因此仍是目标环境放行前置门槛。

2026-09-09 镜像产物复核：新增 Docker 构建上下文排除清单与统一 Bake 文件，覆盖 API、Worker、公开站、运营后台和媒体服务。Go 镜像把构建版本链接进二进制默认值，五个镜像统一写 OCI version/revision；CI 在所有代码、迁移、前端和 Compose 作业通过后执行 Bake、校验标签并上传 14 天 image inspect 清单，不自动推送。`docker buildx bake release-a --print`、Go 两个二进制的实际链接构建和 6 项依赖安全合同通过；本机未启动 Docker daemon，真实镜像层构建仍待 CI/目标主机。

2026-09-09 部署探针复核：新增只读 GET 探针，要求公开首页、后台、API health/ready、Worker ready 与可选 metrics 同属指定 Release A 版本，并校验数据库 ready、安全头、缓存边界、18+ /资料声明及广告占位边界。Cloudflare Access 与 metrics 凭据仅从环境变量读取，Access 头只发往后台，HTTP 自动重定向被禁用，JSON 证据不含响应正文或凭据。专项回归使用两个本地 HTTP 端点确认跨源 302 不会触发第二次请求，防止 Access 或 metrics 请求头随跳转泄漏；5 项专项测试及加入探针后的完整统一预检均通过。真实入口尚未提供，目标环境执行结果仍为 no-go 门槛。

同轮重新执行根目录、日本 edge+monitoring、北京三套 Compose 静态渲染与 `docker buildx bake release-a --print`，五个镜像目标和部署配置均可解析；未启动 Docker 容器，因此不把镜像层、健康检查或资源占用记为运行态证据。

## 视觉证据边界

- `preview-desktop.png`、`preview-mobile.png`、`preview-work-detail.png`、`preview-work-detail-mobile.png` 和 `preview-footer-disclosure.png` 是早期 HTML 原型的设计基线，不作为当前 Next.js 构建已经通过视觉验收的证据；
- 当前 Next.js production build 已新增 [可重复生成的桌面/移动全页视觉证据](./evidence/release-a-visual-2026-09-10/README.md)，覆盖公开首页、作品详情、运营后台登录、owner 运营概览、用户权限和审计查询页，共 12 张截图并带 SHA-256 manifest。审计页桌面/移动截图使用 `release-a:publish-42` 合成时间线，捕获器确认无横向溢出、坏图或脚本异常。该证据仍只代表本地合成数据，不替代目标环境与真实资料视觉验收；
- 用户预览使用可正常搜索、进入详情和执行账号操作的 `TEST-001` 合成资料，XSS 脚本闭合字符串仅保留在 `TEST-JSONLD` 自动安全夹具中；桌面/移动 24 项在拆分夹具和按作品 ID 维护偏好状态后通过；
- 原型可确认桌面广告位密度、移动端纵向布局、详情页图片数量方向和页脚资料/资源/权利边界符合产品意图；
- `preview-ad-layout.png` 包含贴边与底部悬浮广告概念，明确不属于 Release A，不能据此实现悬浮、倒计时关闭或联盟脚本；
- Release A 当前实现只允许带“广告”标识的静态占位。自动 HTTP 冒烟会拒绝常见联盟脚本、激活态广告、悬浮、倒计时和 popunder 信号；
- 当前 Next.js 构建的后台登录页、owner 登录态运营概览、用户权限页、审计查询页和注册首步已完成本地合成数据响应式复核；后台改派、重置/关闭账号完整表单仍需在真实依赖联调环境中复验。

### 当前 Next.js 构建实测（2026-09-10）

- 修复本地 standalone 直接启动未同步 `.next/static`/`public`、导致 CSS 和默认图 404 的问题；`npm run start --workspace ...` 现在在加载 server 前同步当前资源；
- 公开首页 1440×900：文档宽度未超过视口，左右 rail 广告占位均可见，无坏图、无站外脚本；
- 公开首页 390×844：无横向溢出，两个 rail 占位均隐藏，桌面头部搜索隐藏，无坏图；
- API 不可用的作品详情：显示明确“资料暂时无法加载”状态、返回发现页入口及完整页脚资料边界；
- Turnstile 未配置的注册页：邮箱标签和必填属性存在，发送验证码按钮禁用，并明确说明人机验证暂不可用；
- 运营后台登录页 1440×900 和 390×844：无横向溢出、无坏图，邮箱/密码标签、必填属性、autocomplete 和登录按钮均可见；
- owner 运营概览、用户权限和审计查询页 1440×900、390×844：关键指标、账号角色与状态操作、Request ID 输入和最小披露时间线均可见；移动端长邮箱/对象 ID 正常换行且没有横向溢出；
- 本次证明当前公开站主要布局、正常合成详情、后台登录和三类 owner 登录态页面。真实内容、真实图片、目标 PostgreSQL 账号状态和其余后台业务页面仍需 API/邮件/对象存储联调后完成目标环境视觉验收。

## 真实环境放行顺序

1. 日本 PostgreSQL 16：fresh migration v1-v21、运行账号、seed、`/readyz`；
2. Mailpit/SMTP：注册、重置、关闭账号、邀请和旧 session 失效；
3. 运营链路：人工录入 → 他人领取/改派/审核 → 发布 → 番号搜索 → 回滚/隐藏；
4. 验收已开发的图片生命周期：处理 → 双 bucket → manifest → 展示 → 主图替换 → 旧母版保留/清理 → 下架 → purge/删除 → 对账；发现问题后修复并重验，不能以排队成功替代真实删除；
5. 备份：日本生成 → 北京复制 → 独立空库恢复 → verified；
6. 最终域名：Cloudflare 缓存边界、TLS、Turnstile、权利邮箱和后台网络限制；
7. 最终域名和真实目标数据下的桌面/移动视觉、LCP/CLS、API/搜索延迟与资源占用；
8. 所有门槛通过后再决定公网开放。

## 当前需要的外部输入

第一轮只需要：日本 PostgreSQL 16 测试库、Mailpit 或 SMTP、首个 owner 邮箱。密码和密钥必须写入服务器私有 `.env`，不要发送到聊天或提交到 Git。S3/R2、Cloudflare、北京媒体目录和最终域名可在第一轮业务联调通过后接入。

2026-09-11 本机只读探测结果：没有 PostgreSQL Windows 服务、没有 `psql`，`DATABASE_URL`、API/Worker 合同库 URL 和 `SMTP_ADDR` 均未设置；探测只输出存在性，未读取或显示任何值。因此在“不启动 Docker”约束下，当前无法继续执行真实 SQL/Mailpit 门槛。下一次可选择其一：在本机安装并启动 PostgreSQL 16；或在受控终端设置一次性远程测试库连接变量。真实密码仍只放本机环境/私有配置，不发到聊天。
