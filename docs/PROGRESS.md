# 项目进度

更新时间：2026-09-12（Asia/Shanghai）  
当前目标：Release A 测试网站  
目标推进状态：`blocked`，等待隔离 PostgreSQL 16 / Mailpit 测试环境，或确认开始准备并启动本机测试依赖。
状态：主要业务链、上传控制、API/页面动态默认图、Cloudflare 私有 R2 边缘网关及一次性 Storage Analytics 估算代码已实现，所有执行器默认 off。可信账单计量、真实 R2/Cloudflare 网关切换及整体验收仍未完成

2026-09-12 阻塞复核：当前终端未配置 `DATABASE_URL`、`PLATFORM_API_TEST_DATABASE_URL`、`CONTRACT_DATABASE_URL` 或 `SMTP_ADDR`；未发现可用的 PostgreSQL/Mailpit 命令及对应系统服务，根目录和两地区域的私有 `.env` 均不存在。此次仅检查存在性，没有读取或输出凭据。独立交付复核未发现必须继续离线补写的 Release A 功能；完整目标尚未完成。首轮 `core-e2e` 会自动生成 owner、邀请第二个运营账号并创建合成作品，无需预先提供真实资料或手工准备账号。按此前暂不启动 Docker 等基础设施的安排，本轮没有安装或启动新的测试依赖；待环境准备好或确认进入该阶段后继续真实联调。完整 Release A 与公网仍为 `no-go`。

## 仓库交付准备（2026-09-12）

- 按用户本轮要求，当前仅整理并推送代码和文档，不启动本机服务、Docker、数据库或邮件依赖，也不部署目标服务器。
- 已创建私有远程仓库 [jayson2hu/self-deepsearch](https://github.com/jayson2hu/self-deepsearch)；提交排除环境凭据、依赖缓存、构建产物、运行数据，以及与本项目无关的本地智能体看板调研。真实配置仍需在部署时单独提供，不进入 Git。
- 初始提交使用 `[skip ci]`，本次不触发远程 CI；既有工作流保留，后续正常代码提交可执行自动检查。本次只做提交范围、敏感文件、文档链接和暂存区检查，不把已有测试结果写成重新执行。
- 提交前复核：622 个项目文件，常见密钥签名扫描无命中；65 份 Markdown 的 410 个本地链接无缺失。普通空白检查提示已有 Markdown 硬换行、日志前缀末尾空格和文件末尾空行，保留原内容；排除行尾/文件末尾空白提示后的暂存区检查通过。
- 仓库上传不等于功能上线或真实联调通过：Release A 仍是本地交付 `conditional-go`，整体与公网 `no-go`，上述 PostgreSQL/Mailpit 和目标存储环境门槛保持不变。

## 最新复核：主图读取防御统一（2026-09-12）

- 后续补上三组真实 PostgreSQL 仓储行为用例，覆盖摘要/关注/历史/后台的异常主图读空、替换失败时新旧资产均无残留变更，以及精选图顺序与缺主图兜底；现有 CI 数据库包命令自动包含这些测试。该包编译、单元测试与 vet 通过，SQL 子场景因缺少测试库全部 SKIP，尚未形成真实数据库通过证据。独立只读评审完成，执行命令已加入运行手册。
- 列表/搜索/推荐等作品摘要、人物摘要、关注列表和账号浏览历史现在都只接受实体对应的 `cover/primary/0` 或 `avatar/primary/0`；历史异常用途或位置不会被投影为主图，没有合法图片时继续由前端默认图兜底。
- 运营后台当前主图查询和主图替换事务内重读采用相同约束；详情图库既有 `cover/0 + gallery/1..3` 规则不变。没有新增 API、配置或迁移，Schema 保持 v21。
- 受影响 Go 测试、API 全量 test/vet 和 Release A 产品合同 **46/46** 通过；随后统一预检再次通过 Go test/vet、OpenAPI 类型漂移、Markdown **65/408/0**、TypeScript/ESLint、production audit 0 vulnerabilities、Ruff、18 文件 mypy、Python **299 passed/303 subtests/1 skipped**、工具合同 **62/62**、前端单元 **40/40**、standalone HTTP 和 Go→Next 缓存合同 **9/9**。本轮显式跳过 build、性能、Playwright 与 Compose，沿用 9 月 11 日对应独立证据。文档证据按实际日期拆分，自动外部评审因本机网络不可用未产生结果且不计作通过。见[主图读取防御证据](./evidence/release-a-media-primary-read-guards-2026-09-12.md)。

## 最新审计：初始目标与当前实现统一（2026-09-11）

- 新增[Release A 需求与验收矩阵](./RELEASE_A_REQUIREMENTS_MATRIX.md)，逐项对应最初确认的资料展示、作品发现/番号搜索、人物混排、图片兜底、账号/邮件、匿名聚合、审核、广告占位、两地部署、备份和免费对象存储保护边界。
- 修正文档偏差：发行日期统一为可选且缺失时不进入“最新发行”；作品 CSV、人物/厂牌人工录入和图片 manifest 不再混写成“全部 CSV 导入”；首页/目录缓存口径与当前运行时架构一致；北京/日本采集器明确属于 Release B；当前浏览器回归统一为 39 场景、desktop/mobile 78 项。
- 文档修改后单独重跑 Markdown 门禁：63 份文件、399 个本地链接、0 缺失；没有把该文档检查写成代码全量预检重跑。
- 审计结论：没有发现偏离初衷而启用资源、头像识别、自动采集或广告联盟，也没有确认出仍可在无外部依赖条件下补写的 Release A P0 业务功能。当前有效下一步是 PostgreSQL/邮件第一阶段真实联调，再进入对象存储、Cloudflare 和目标服务器验收；公网继续 `no-go`。

## 最新验证：当前代码全量本地回归（2026-09-11）

- 本轮代码与就绪文档合并后的最终 Markdown 复核为 **64 份文件、404 个本地链接、0 缺失**；统一预检执行时为 403 个链接，新增的第 404 个是就绪文档指向本轮证据的链接。
- 收紧详情页图片槽位：处理器、HTTP、仓储和运营后台统一只接受作品 `cover/primary/0`、作品 `gallery/non-primary/1-3` 与人物 `avatar/primary/0`；作品精选图必须先有主图、最多三张且位置不可重复。公开 API 增加读取侧过滤，只有精选图或历史异常组合时返回空图片并由前端使用默认图。详情页上报去掉全局一秒时间去重，改为挂载实例内抑制 Strict Mode 重放，快速返回同一作品会再次上报。Go 定向测试、Python 媒体 **9 passed/7 subtests**、Ruff、两站 TypeScript/ESLint/production build、API 类型检查、两条新增浏览器合同 desktop/mobile **4/4** 及完整 Playwright **78/78** 均通过；Schema 仍为 v21，真实 PostgreSQL 槽位并发未执行。见[展示槽位与详情重入证据](./evidence/release-a-media-display-slots-2026-09-11.md)。
- 加固运营后台作品 CSV 错误下载：原导出虽然正确引用逗号与双引号，但用户提供的番号可用 `= + - @` 或前置控制空白触发表格公式。现将导出逻辑抽成纯模块，危险前缀统一加文本标记，保留 UTF-8 BOM、行号和服务端错误信息；只导出错误行，不回填或自动执行原 CSV。新增 2 个行为单测并纳入默认前端门槛，同时新增桌面/移动真实页面“上传预检→下载→读取文件”回归；ops TypeScript/ESLint/production build、产品安全合同和完整 Playwright 均通过，见[CSV 错误导出证据](./evidence/release-a-csv-error-export-2026-09-11.md)。
- 修复生产镜像可能烘入构建期合成目录的 P1：公开首页不再在 API 不可用时返回 `TEST-001`～`TEST-004`，而是显示明确故障提示和空目录；首页、`robots.txt`、`sitemap.xml` 均改为运行时生成，避免把回环 API 或构建机 `SITE_URL` 固化进镜像。生产 `siteURL()` 现在只信任运行时 `SITE_URL`，缺失、非法、带路径或非本机 HTTP 时直接失败，不再使用 `NEXT_PUBLIC_SITE_URL`/回环默认值；根布局也不在构建期解析站点 origin。公开 API 数据仍按 300 秒缓存并使用 `public-catalog` 标签，Nginx/Cloudflare 目录缓存目标仍为 5 分钟。production build 确认三条路由均为动态 `ƒ`，构建产物合成资料扫描为 0，standalone HTTP 正负向站点身份冒烟和真实 Go→Next 9 项缓存合同均通过，见[运行时目录证据](./evidence/release-a-runtime-catalog-2026-09-11.md)。
- 默认统一预检完整通过：Go API/Worker test+vet、OpenAPI 漂移、Markdown 本地链接门禁、两站/API contracts TypeScript、两站 ESLint、production dependency audit 0 vulnerabilities、全范围 Ruff、18 文件 mypy、Python **299 passed/303 subtests/1 skipped**、工具合同当前 **62/62**、前端单元当前 **40/40**、两站 production build、standalone HTTP 与真实 Go→Next 9 项缓存失效合同。
- 补跑本地浏览器门槛：首页/作品详情 desktop LCP 中位数 240/168 ms，mobile 208/148 ms，mobile 最大 CLS 均为 0；HTTP 单并发 20 样本 P95 为首页 API 15.4 ms、番号搜索 8.17 ms；均通过仓库门槛。
- Playwright desktop/mobile **78/78 passed**，桌面/移动各 39 项，覆盖搜索、邮箱账号、用户功能、CSV 错误文件安全下载、主图、审核发布、邀请角色、反馈下架、用量复核、上传控制、动态停图以及公开账号页/后台 CSP nonce。性能探针新增指标读取后的有界同源流排空，专项单元 **10/10** 和完整 3 样本探针通过，页面关闭时的 `destination stream closed early` 诊断已消失；排空时间不计入指标，也不改为等待 `networkidle`。
- 完整浏览器复跑暴露媒体停图测试仍假设首页是 ISR：当前首页已按生产目录 P1 改为运行时渲染，重启到 `default_only` 后应直接从缓存 DTO 投影默认图，而不是要求 HTML 保留远程图片 URL。测试已按当前架构修正，并让隔离 standalone 丢弃来源副本中的请求期 fetch cache，防止本地预览/冒烟数据污染后续合同；缓存工具合同、媒体桌面/移动 4/4、完整 Playwright 78/78 与真实 Go→Next 9/9 均通过。
- 安全完成度复核确认 OpenAPI 与 Go 当前共有 20 个近期密码操作；补上上传控制命令在 Go 路由策略表中的缺失正例，并修正安全审计里“18 个”和“每日巡检尚未接入”的过时口径。真实 PostgreSQL、目标 HTTP 与告警验收仍保持未完成。
- 运营后台以及公开站登录、注册、重置、邀请、退出和账号中心 production HTML 改为每请求 128 位随机 CSP nonce + `strict-dynamic`，均为 `private, no-store`；登录和邀请明确改为动态渲染。公开目录继续保留框架所需脚本 `unsafe-inline`，但全站新增 `script-src-attr 'none'` 阻止内联事件处理器；这项 CSP 风险与首页改为运行时渲染是两个独立边界。两次响应 nonce 轮换、所有可执行脚本属性和桌面/移动 CSP 控制台均已验证；最终加严后的两站 CSP 定向浏览器复核 4/4 通过。风险进一步缩小但未关闭，详见[账号页面 CSP 证据](./evidence/release-a-ops-csp-nonce-2026-09-11.md)。
- 完成 Release A [对象存储任务清单](./MEDIA_STORAGE_TASK_POLICY.md)复核：上传、全量对账、权利删除和公开读取四类入口均已登记，直接 SDK 客户端受静态合同约束；新增合同 **4/4** 并已纳入上述 Python 全量回归。当前唯一非必要自动对象任务是全量对账，已有调度前/派发前双阶段准入；旧记录中的“仍需补其他非必要作业限流”不再代表当前代码缺口。加入 CSV 错误导出证据后当前复核 **62 份 Markdown、392 个本地链接**，均无缺失；该检查现已进入默认预检和 CI。真实账单、bucket、Cloudflare 和两机验收仍未完成。
- 当前重新静态解析根级、日本 edge+monitoring、北京三套 Compose，并执行 `docker buildx bake release-a --print`；四个命令均通过，五镜像目标完整。没有启动容器、构建镜像层或连接外部服务，结果只证明部署配置可解析，见[部署静态证据](./evidence/release-a-deployment-static-2026-09-11.md)。
- 修复北京 2C8G 部署模板无资源边界的问题：Go Worker 限制为 0.20 CPU/192 MiB/128 pids，图片服务为 1.25 CPU/2 GiB/256 pids，合计 1.45 CPU/2240 MiB；两者加入 10 MiB × 3 日志轮转。新增总预算合同同时锁定日本 2C4G 上限，目标 Linux 最大图片和并发 RSS 仍待实测，见[北京资源证据](./evidence/release-a-beijing-resource-budget-2026-09-11.md)。

完整命令、指标和边界见[本轮验证](./evidence/release-a-local-regression-2026-09-11.md)。本轮没有启动 Docker/真实 PostgreSQL/SMTP/R2/Cloudflare；本地性能无网络限速且使用内存合成 API，不能替代日本 2C4G、最终域名和真实事务验收。公网仍 no-go。

## 上一轮：账户 Storage Analytics 只读估算（2026-09-11）

- 日本现有 Go Worker 的一次性 `media-usage` 报告升级为 v2：在原账户操作量外，独立查询 `r2StorageAdaptiveGroups`，保留 `bucketName + datetime` 维度；同一时点先跨全部已观测 bucket 汇总，再按 UTC 日选择 `payload + metadata` 峰值。不会把单桶 `max(payloadSize)` 冒充账户总量。
- 存储响应对 partial errors、重复 bucket/时间、任一时点缺 bucket、缺 UTC 日期、越界时间、字段缺失、10000 行上限和 64 位溢出全部 fail-closed；`observed_byte_days` 和 30 日制 GB-month 微单位估算使用大整数/向上取整，不使用浮点数。报告不输出 bucket 名。
- 默认不套用 Standard 免费额度；只有操作者显式传 `--standard-only-confirmed` 后，才以 10 GB-month 和 5% 预留给出 70/85/95% 建议。无论是否确认，`billing_verified=false`、`provider_watermark_verified=false`、`enforcement=not_connected`、`automatic_recovery=false` 固定不变。
- 该增量仅进入一次性报告，不修改 Schema v21、不写 PostgreSQL、不改变持久观察器或任何图片执行状态。`mediausage` **44 个顶层测试、219 个子测试**通过，platform-worker 全量 test、vet、build 通过；未连接真实 Analytics/R2、未启动 Docker、未部署或触发远程 CI。

设计、命令和剩余边界见[只读用量观察](./MEDIA_USAGE_OBSERVATION.md)，可复跑证据见[本轮验证](./evidence/release-a-media-storage-analytics-2026-09-11.md)。Analytics 仍不能证明版本历史、所有共享应用、Standard 类别、供应商水位或账单；Release A/M5 与公网继续 no-go。

## 上一轮：未完成 multipart 容量保护（2026-09-11）

- 北京媒体处理器的上传前双桶容量核算现同时统计全部当前对象和未完成 multipart 已上传部件；对象/upload/part 的结构、大小、完成标记、重复项和分页 marker 均严格校验，缺权限、竞态、`NoSuchUpload`、异常响应或 64 位溢出 fail-closed。
- 当前对象页、multipart upload 页和 part 页共享单次 256 个列表请求预算，耗尽时在下一次供应商请求前拒绝，避免保护扫描本身无限消耗 A 类额度。每页请求仍重新检查现有上传控制/逐操作准入；不自动完成或中止未知 multipart。
- 容量/上传控制/准入/安全专项 **53 passed、101 subtests**；media-python 全量 **77 passed、118 subtests、1 skipped**，9 个源文件 mypy 通过。最终仓库 Python **288 passed、288 subtests、1 skipped**，全范围 Ruff 及 50 份 Markdown/331 个本地链接检查通过。没有连接真实 S3/R2、启动 Docker 或改变 bucket。

设计和目标权限见[容量手册](./MEDIA_UPLOAD_CAPACITY.md)，命令与边界见[本轮证据](./evidence/release-a-media-multipart-capacity-2026-09-11.md)。本轮只关闭两个专用桶的未完成分段漏计；GB-month、其他 bucket、共享账户和真实供应商验证仍未完成，Release A/M5 与公网继续 no-go。

## 上一轮：Cloudflare 图片全入口网关（2026-09-11）

- 新增 Cloudflare Worker 媒体网关，要求派生图 bucket 关闭匿名 `r2.dev`/旧 custom domain，以私有 R2 binding 读取。`S3_PUBLIC_BASE_URL` 改为受控 media origin；母版和任意非 `media-public` key 永不暴露。
- 网关先读取最多 5 秒缓存的 Go API策略，再访问 Workers Cache/R2。人工或动态 `default_only`、策略超时/错误、R2 缺对象/错误均跳过对象缓存，返回内置作品/人物 SVG 默认图且 no-store；旧边缘对象不能绕过策略。正常对象保持一年 immutable 缓存。
- Go API 新增默认 off、独立 Bearer 保护的精确 `/edge/v1/media-delivery-policy`；错误凭据表现为 404，重复 Authorization、query、非 GET 拒绝，策略/数据库错误继续 fail-closed。Nginx 仅代理这一条路径，其他 `/edge/` 与 `/internal/` 404；新增 enabled 指标。
- 选择边缘 Worker 而非日本 Go 图片代理，避免把图片回源带宽和连接压到 2C4G；代价是每次图片请求仍计 Worker 请求，真实免费额度/延迟必须验收。默认 5 秒是传播速度和 API 调用量的显式取舍，不声称瞬时撤回已经下载到浏览器的文件。
- 最终专项通过：Cloudflare 网关 Node **8/8**，环境/Nginx/安全合同 **37 passed、74 subtests**；前端/工具单元总门槛 **31/31**。Go API/Worker 全量 test/vet/build、Python **280 passed、281 subtests、1 skipped**、全范围 Ruff、两站 lint/typecheck 和 OpenAPI 类型漂移检查均通过。本轮没有部署 Worker、绑定 R2、改变 bucket 公开状态或启动 Docker。

设计、启用/回滚和目标验收见[边缘网关手册](./MEDIA_EDGE_GATEWAY.md)，命令和证据边界见[本轮证据](./evidence/release-a-media-edge-gateway-2026-09-11.md)。当前是离线 conditional-go；Release A/M5 与公网仍 no-go。

## 上一轮：API/Next 动态默认图（2026-09-11）

- 新增 `MEDIA_DELIVERY_DYNAMIC_MODE=off|enforce`，只注入日本 `platform-api` 与 `display-web`；人工 `MEDIA_DELIVERY_MODE=default_only` 保持最高优先级。动态 enforce 要求用量观察为 observe，生产配置检查拒绝无效或未配套模式，北京服务不接收此开关。
- Go API 复用现有 `platform.media_usage_state` 和 API 只读权限，不增加表/迁移/服务。只有账期有效、30 分钟内新鲜、无复核/停止锁存且状态为 `low_estimate|warning` 才允许图片；缺状态、未来/过期、`high/unknown`、仓储错误或 750ms 超时均 fail-closed。公开/用户目录 DTO 去图，文字、账号、反馈证据、运营 manifest 与权利删除不变。
- 新增固定私有路径 `/internal/v1/media-delivery-policy`，严格返回 `normal|default_only`、`no-store` 与匹配响应头；不进入 OpenAPI。Next Proxy 每个文档请求以 1 秒严格合同读取，错误也停图；生产 API Nginx 对公网 `/internal/` 返回 404。新增有效停图、动态开关、策略可用性私有指标和不可用告警。
- 动态策略不在 Server Component 增加 `no-store` fetch；Go API 新 DTO 去图，Proxy 对旧目录 HTML 即时增加 CSP。首页现为请求时渲染，以避免构建期目录和站点配置进入镜像，但其 API fetch 仍使用 300 秒 `public-catalog` 数据缓存，公共入口仍可按 5 分钟策略缓存成功目录响应。因此状态切换无需重启即可阻止浏览器远程图片请求；缓存正文、OG/JSON-LD/RSC 的历史 URL 仍需原 outbox/Cloudflare 失效后清除，恢复同样需要缓存失效。这是明确取舍，不冒充图片源站全局关闭。
- 最终 Go API/Worker 全量 vet、test、build 通过；Python **273 passed、275 subtests、1 skipped（115.52 秒）**，全范围 ruff、OpenAPI 类型漂移检查通过。前端 23 项单元、两站 lint/typecheck、display production build 通过；媒体专项手动/动态 × 桌面/手机 **4/4（2.0 分钟）** 通过，动态场景在同一 Next 进程和同一缓存 HTML 上验证 normal→default_only→normal，停图阶段没有远程图片请求。收尾又统一给私有策略端点的 400/405 错误响应增加 `no-store`，专项 Go test 与 API 全量 vet 复验通过；Ruff、两站 lint/typecheck、OpenAPI 漂移及 52 份 Markdown 本地链接检查再次通过。
- Schema 保持 v21、62 表/5 视图；公开 OpenAPI 保持 87 操作/76 路径/106 Schema。本轮没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，没有部署、暂存/提交或触发远程 CI。

设计、启用、恢复和 ISR 取舍见[动态/人工停图手册](./MEDIA_DELIVERY_MODE.md)，命令、失败记录及剩余边界见[本轮证据](./evidence/release-a-media-delivery-dynamic-2026-09-11.md)。该轮所列的已知 URL/旧缓存入口缺口已由上方 Cloudflare 私有 R2 网关补齐离线代码；当前仍需真实 bucket 私有化、Worker route/R2 binding/Cache API/purge、5 秒传播和存储 GB-month/账单口径验收。

## 上一轮：上传前用量准入与系统自动暂停（2026-09-11）

- 北京上传检查、每页 Listing、每次 PUT 前，通过私有签名接口读取日本已有用量状态；有效 allow 才继续。高风险、统计失效、数据库/网络/协议失败均阻断本次操作；启用/恢复还会再次核对原命令有效期。
- 拒绝时在北京原控制日志写明确的 v2 系统暂停，推进同一 generation，让旧恢复快照失效。不伪造管理员、不受未知人工请求阻塞、不清 pending、不因新统计变好自动恢复；必要删除仍独立运行。
- 不增加服务、迁移或公开 API；复用日本 Worker listener/连接池，北京不直连 PG。两端默认 off；配置检查强制配套开关、独立密钥与固定 IP origin。新增私有指标/告警、仅回环绑定的可选隧道上游配置；没有创建真实通道或开放端口。
- 全量 Go API/Worker vet/test、Worker 构建通过，新模块覆盖率 **95.4%**；实际 Python→Go HTTP 验证 95% 后只发出第一笔 PUT、暂停/旧恢复拒绝、必要删除、显式恢复及 pending 保留。全量 Python 最终 **268 passed、272 subtests、1 skipped（93.35 秒）**，全范围 ruff、18 文件 mypy、OpenAPI 漂移检查通过。
- 按后端、架构、运维、安全和 QA 检查补齐错误/超时/重复响应/配置偏差/日志损坏/恢复过期场景；发现并修正 CI Go 作业缺媒体 Python 依赖，已加静态门槛但没有触发远程 CI。本轮没有前端修改或重跑浏览器，旧 70/70 只作为历史证据。
- Schema 保持 v21、62 表/5 视图；OpenAPI 87 操作/76 路径/106 Schema、20 个近期密码操作。保护在下一次操作触发，不主动定时暂停空闲上传器、不撤销在途 SDK，不代表计费上限或全站停图。

设计、配置与回滚见[上传准入手册](./MEDIA_UPLOAD_ADMISSION.md)，最终检查/命令和未验证边界见[本轮证据](./evidence/release-a-media-upload-admission-2026-09-11.md)。README、架构、实施、发布/安全审计及图片相关手册已对齐；历史轮次保留原始计数。

文档收尾后，DB/安全/环境离线合同复跑 137 项、142 subtests 通过（14.80 秒）；ruff/OpenAPI/Go 格式检查通过，19 份文档的 235 个本地文件链接无缺失。

没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，没有部署、暂存/提交或触发远程 CI。Release A/M5 和完整目标继续进行，公网 no-go；真实 SQL SKIP 不视为验收通过。

后续进展：该轮所列 API/Next 动态默认图与直连/旧边缘缓存入口代码已经完成；对象存储任务清单也已确认当前没有第二个未接准入的非必要自动任务。当前剩余存储计费边界，以及真实 Cloudflare/R2、两机和供应商验收，不能用补凭据替代。

## 上一轮：后台图片上传控制页面（2026-09-11）

- 新增 `/media/upload-control`，仅 admin/owner 可进入；导航、最近远端观察、独立暂停/恢复确认和最近 10 项请求历史已实现。首次只能暂停初始化；变更操作/原因、刷新及标签页可见性变化后重新确认，状态每秒检查有效期。
- 待处理、已派发、结果待确认、已应用、拒绝和被取代分开显示。202 不会修改“最近确认状态”；部分/异常回执、过期或未来观察不允许新操作，失败读取不当作正常或零用量。
- 响应丢失时只保留原幂等键、原始微秒快照和完整载荷重试；刷新失败不会丢弃原请求，已有开放请求时禁止新增。重试副本只在页面内存，离开页面不撤销服务器已受理请求，重新进入应先核对历史。
- 复用同源 Go API 和已有近期密码认证，前端不持跨区密钥；GET/POST 配置 5/15 秒截止信号，成功响应 128 KiB/UTF-8/完整类型和关联校验，错误不展示底层正文。共享近期密码验证补截止信号，故全站登录/权限/审核均纳入回归。
- 按前端、QA 和安全技能修正表单无障碍标签、框架提示区域的测试歧义，以及“请求发送中提前显示结果未知”的时序问题；新增延迟响应断言。最终完整桌面/手机浏览器 **70/70（5.0 分钟）** 通过，四张最终截图已复核并保存，没有横向溢出或状态混淆；未把首轮失败结果标为通过。
- 最终运营站构建、两站类型/lint、22 项前端单元、56 项工具合同及两站 standalone HTTP 冒烟通过；DB/安全/环境/OpenAPI 离线合同在文档更新后复跑 **136 passed、108 subtests（30.66 秒）**。OpenAPI 生成无漂移。没有重跑 Go 全量、完整 Python/真实跨区 HTTP 或新的性能/联网依赖审计，历史结果不冒充本轮证据。
- 本轮没有新增迁移、公开 API 或常驻服务；Schema 仍 v21、62 表/5 视图，87 操作/76 路径/106 Schema、20 个近期密码操作。用量复核不自动恢复，执行器开关保持 off。

操作与故障说明见[队列及后台手册](./MEDIA_UPLOAD_QUEUE.md)，可复跑检查和最终计数见[本轮证据](./evidence/release-a-media-upload-ui-2026-09-11.md)。没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，没有触发远程 CI、部署、暂存或提交。当前属于合成资料验证，Release A/M5 和完整目标保持进行中，公网 no-go。

下一步：继续实现自动额度联动剩余执行点和保护策略；真实数据库、Linux 卷/SDK、跨区传输与供应商验收仍须后续授权及环境，不能用补凭据代替尚未完成的开发。

## 上一轮：上传控制授权队列与回执（2026-09-11）

- 新增 admin/owner 上传控制查询与提交 API；近期密码认证、同源校验、明确确认、操作者/理由/请求 ID、UUID 幂等和精确状态 CAS。202 只表示排队，重试保留原请求，不把待确认显示为执行成功。
- Schema v21 增加两张表，当前 **62 表/5 公开视图**；API 不能更新执行状态，Worker 不能改请求或终态证据。单一待处理请求、30 秒带 token 租约、数据库/本机双时钟校验，HTTP 期间不持数据库事务，复用现有日本 Worker 和连接池。
- Worker 先观察、再保存派发记录、再发请求。真实本地 Go→Python 已验证限时协议，以及远端已应用但响应丢失后通过签名状态确认、没有第二次 apply。过期/旧快照/撤销角色不发送恢复，结果未知保持 uncertain。
- 北京追加控制日志前检查五分钟有效期；数据库拒绝目标/epoch/generation 回退。恢复需要新鲜用量准入；复核/账期切换不自动恢复。日本 dispatch 要求北京 enforce 和独立匹配密钥；默认 off，不新增服务。
- Go API/Worker 全量 vet/test、两个二进制构建通过；上传控制模块覆盖率 **86.9%**（真实 SQL SKIP）。完整 Python **255 passed、204 subtests、1 skipped（107.20 秒）**，全范围 ruff、17 文件 mypy、OpenAPI 漂移、两站类型/lint、56 项工具合同与 15 项前端单元通过。OpenAPI 为 **87 操作/76 路径/106 Schema**，20 项高风险操作要求近期认证。
- 新增实际 API/Worker PostgreSQL 仓储合同并接入既有串行 CI 配置，覆盖角色/权限、锁竞争、回执/请求不可变及丢失响应恢复；本机明确 SKIP，不视为迁移成功。修正旧版本断言、静态 fixture 和健康测试配置，当前文档/部署门槛统一为 v21，历史证据保留原版本。

设计、操作和升级见[上传控制队列](./MEDIA_UPLOAD_QUEUE.md)，验证命令与边界见[本轮证据](./evidence/release-a-media-upload-queue-2026-09-11.md)。本轮没有上传控制 UI，也没有重建 Next/重跑浏览器或 standalone HTTP；旧 54 项浏览器证据不算本轮。未启动 Docker/真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，未触发远程 CI、部署、暂存或提交。Release A/M5 和完整目标保持进行中，公网 no-go。

文档收尾后复跑 DB/安全/环境检查 132 项/108 子场景通过；OpenAPI 类型无漂移，23 份文档的 204 个本地文件链接无缺失。

下一步：先做后台上传状态、独立暂停/恢复确认及回执页面，再推进自动额度保护剩余执行点；真实数据库与供应商验收仍需后续授权和环境，不阻塞当前离线 UI 开发。

## 上一轮：跨区上传暂停／恢复执行端（2026-09-11）

- 日本现有 Go Worker 新增一次性 `media-upload-control` 命令，北京现有媒体服务新增独立签名的私有状态/应用接口。显式 live、区域、快照和恢复确认后才发送；默认关闭，不连接 PostgreSQL/Analytics 或新增服务。
- 北京共享卷追加控制日志，epoch/generation CAS、随机 UUID 幂等、持久化后才回执。缺状态先暂停，首次只能 pause 初始化；损坏/部分日志拒绝，重启或把 flag 改回 off 不会自动放行。日志有界，启用指令为暂停预留空间。
- 上传准入、每页容量 Listing 和每次 SDK PUT 都检查控制状态。PUT 占锁时暂停返回 busy，不提前确认；同批中途暂停不发后续 PUT，并保留未确认批次证据。恢复不能清 pending，人工清 pending 也不能解除暂停。必要删除/校验继续可用。
- 请求和响应采用独立、路径/nonce/正文/状态绑定签名。实际本地 Go→Python HTTP、SDK 准入、旧恢复冲突、必要删除通过；篡改、旧响应重放、伪造 200 与重定向不能误报成功。HTTP 不加密，需固定 IP 防火墙加可信传输。
- Go API/Worker vet/test、Worker 重建通过；新 Go 模块 6 组/40 子场景通过、无 skip，覆盖率 89.7%。最终完整 Python **250 passed、193 subtests、1 skipped（86.11 秒）**；全范围 ruff、17 文件 mypy、OpenAPI 漂移与离线 CLI 检查通过。已修正旧模板测试未豁免默认关闭的空配置及新增测试 import 排序。命令与执行边界见[本轮证据](./evidence/release-a-media-upload-control-2026-09-11.md)。

当前是手动运维执行链，**不是后台排队/自动额度触发，更不是页面与源站停图**。命令日志暂未绑定后台用户 ID，仍需带操作者/近期认证的 API 队列和可信回执落库。升级与边界见[执行端手册](./MEDIA_UPLOAD_CONTROL.md)。未启用开关、启动 Docker/真实依赖、触发远程 CI、暂存或提交；前端未修改/重建/重跑浏览器。Release A/M5 与完整目标保持未完成，公网 no-go。

## 上一轮：定时图片对账用量准入（2026-09-11）

- 新增默认 off 的 `MEDIA_RECONCILE_USAGE_GUARD`，仅日本 Worker、observe + 对账启用时可显式 enforce。复用现有 PG 状态和连接池，不增加服务、Schema 或 Analytics 请求。
- 达到 85%/预留阈值、复核/停用建议、未初始化、配置变化、旧/未来统计及读失败时，阻止新全量扫描。整数高水位重算风险，不复用旧放行；库存准备前和跨区 HTTP 前各核对一次。
- 第一次拒绝不创建运行；第二次拒绝留 failed/usage_guard_denied 证据，不伪造完成、也不消耗每日扫描机会。Enforce 模式一分钟重查准入，普通网络失败仍遵守原日间隔。
- 新增私有有界指标与告警；不阻断账号、审核、邮件、缓存清理、权利删除和固定默认图检查。实际本地 Go→Python 已验证拒绝时不发扫描、放行时正常对账，以及扫描被阻止时删除继续成功。
- Go API/Worker 全量 vet/test、Worker 重建已通过；准入专项 4 组/30 个子场景与调度/私有指标测试通过，扩展后的两项真实 SQL 合同仍明确 SKIP。最终 Python **228 passed、141 subtests、1 skipped（82.58 秒）**，全范围 ruff、15 文件 mypy、OpenAPI 漂移检查通过。修正 SQL 合同变量重名编译错误及旧静态合同对 gofmt 空格的依赖。

该实现只接入定时扫描，不是上传/页面/源站停图或恢复，不能声称完整额度控制完成。范围、设计取舍和升级见[定时扫描准入手册](./MEDIA_TASK_ADMISSION.md)，命令与未执行范围见[本轮验证](./evidence/release-a-media-task-admission-2026-09-11.md)。未启动 Docker/真实 PostgreSQL/SMTP/S3/Cloudflare/Analytics，未触发远程 CI 或暂存/提交；前端未修改，本轮不重建前端或复跑浏览器，旧 54 项浏览器结果保留在上一轮证据中。Release A/M5、公网 no-go 和完整目标未完成的结论不变。

## 上一轮：人工复核与账期切换（2026-09-11）

- 新增管理员/owner 的用量状态查询与复核请求 API，以及后台“用量复核”页面。POST 必须近期密码认证、显式确认、快照 CAS 和幂等键；202 只表示排队，不表示恢复图片。
- 迁移 20 增加不可变复核请求/回执与前后状态证据，共 60 表/5 公开视图。API 只能提交，Worker 只能校验与结算；近期角色变更、过期、旧快照和运行中观察均不能绕过。
- Worker 在观察前处理五分钟有效请求。解除异常必须有新鲜且非高风险统计、无停用锁存，不降低高水位。新账期必须匹配全部日本 Worker 的同账户同策略显式配置；保留旧账期证据，切换后保持 hold，等待新统计和再次复核。
- 复核发现 API 的原始 `SELECT FOR UPDATE` 与状态只读权限不匹配，已改用专用固定投影锁函数，未授予 API 状态 UPDATE。新增实际 API 仓储 SQL 合同，覆盖行锁竞争、CAS/幂等/审计与最小权限；Worker SQL 合同覆盖状态与 applied 回执同事务。真实 SQL 均待执行，不以模拟测试替代。
- 最终 Go API/Worker vet/test 与两个二进制重建通过（只编译未启动）；完整 Python **225 passed、138 subtests、1 skipped（80.76 秒）**，全范围 ruff、15 源文件 mypy、OpenAPI 类型漂移检查通过。合同为 **85 操作/74 路径/100 组件 Schema**，19 个高风险操作要求近期密码。
- 两站类型/lint、运营站构建、15 项前端烟测、56 项工具合同与两站 standalone HTTP 冒烟通过。完整桌面/手机浏览器 **54/54（3.8 分钟）**，新增 8 项用量复核测试；已修正刷新后残留旧待处理提示并复验，最终手机待处理/桌面切换后保护截图核对无横向溢出。

实现、架构取舍与升级见[人工复核/账期切换手册](./MEDIA_USAGE_REVIEWS.md)，可复跑命令及证据边界见[本轮验证](./evidence/release-a-media-usage-reviews-2026-09-11.md)。未启动 Docker、真实 PostgreSQL/SMTP/S3/Cloudflare 或真实 Analytics，未暂存/提交/触发远程 CI；公开站构建与性能/依赖审计沿用原轮次证据。本轮不新增服务。Release A/M5 仍未完成，公网 no-go，完整目标保持进行中。

## 上一轮：观察状态持久化与定时器（2026-09-11）

- 迁移 19 新增 `platform.media_usage_state`、`audit.media_usage_runs`，总计 59 表/5 公开视图。账户摘要、明确账期/策略快照、高水位与首次复核/停用建议不会因重启或较低新统计自动清除；数据库也禁止隐式重置、覆盖已完成观察。
- 日本 Worker 接入 `MEDIA_USAGE_MONITOR_MODE=off|observe`，模板默认 off；开启后 5～15 分钟观察一次，READ COMMITTED/advisory lock/租约协调多实例，HTTP 查询不持数据库事务。中断/失败保留证据，完成核对 run ID、窗口和数据库时钟；北京/前端不持 Analytics token。
- 私有指标和三条告警区分状态不可读、复核/陈旧、停用建议，不声称停图已执行，不让账号或权利下架依赖 Analytics。授权恢复、账期切换、动态跨区执行及源站控制仍未接入。
- API readiness、seed、SQL 合同、监控、CI、文档与前端合成 fixture 的 Schema 门槛同步为 19；真实 PostgreSQL 并发/权限合同已接串行 CI，本机明确 SKIP。没有启动 Docker/真实依赖、真实 Analytics 或远程 CI。

最终 Go API/Worker vet/test、两个二进制重建和离线命令通过；`mediausage` 整包 31 个顶层测试 + 121 个子测试通过，覆盖率 91.8%，真实 SQL 合同 SKIP。全量 Python 222 项 + 136 subtests 通过（1 skip）；最终定向回归 162 项 + 103 subtests；ruff、15 文件 mypy、OpenAPI 漂移、15 项前端单元/56 项工具合同及两站 standalone HTTP 通过。没有重跑浏览器或真实外部依赖。

架构取舍、升级及未完成项见[持久观察手册](./MEDIA_USAGE_STATE.md)，命令与证据边界见[本轮验证](./evidence/release-a-media-usage-state-2026-09-11.md)。此前查询/浏览器证据保留原适用范围。

## 上一轮：账户操作量观察基础（2026-09-11）

- 新增 Go Worker 一次性 `media-usage` 子命令，显式 `--live` 才查询 R2 官方账户 Analytics；固定 HTTPS 目的地、环境变量只读 token、禁止重定向/自动重试，不启动数据库或常驻任务。没有执行真实账户查询。
- 查询全部 bucket 的已知 A/B/免费操作，最多 31 个串行分段；共同端点允许保守重复，避免毫秒事件落入分段间 1 秒空隙。任一失败、未知操作、字段缺失/重复、部分 GraphQL 错误、截断或溢出，整次 unknown，不能使用部分累计数放行。
- 风险建议含 70%/85% 与 5% 默认预留（默认到 95% 建议停图），并检查错账期/陈旧结果。该轮 v1 报告明确“不是账单、无完整计量水位、未含存储、未接执行、不可自动恢复”；当前 v2 已由顶部最新轮次增加只读存储估算，但仍不是自动限额服务。官方文档明确 Analytics 不能作为账单依据，GB-month 也不等于当前两桶字节数。
- 最终新模块 **17 个顶层测试 + 82 个子测试通过，覆盖率 95.8%**；Go API/Worker vet/test、Worker 编译与离线命令退出码验证通过；受影响 Python 合同 **129 项 + 30 subtests 通过（20.14 秒）**。详见[本轮证据](./evidence/release-a-media-usage-2026-09-11.md)。本轮未修改前端/Python、数据库 Schema 或部署配置；不把上一轮浏览器/供应商结果算成本轮验证。
- 该轮登记的持久快照、统计失败策略、定时查询、动态跨区执行和源站入口控制已有后续离线实现；一次性存储估算也已补。该轮“其他非必要作业限流”待办已由后续对象存储任务清单复核关闭；可信账单口径、存储持久化是否需要与真实联调仍未完成，仅提供凭据不能替代这些工作。

使用与架构边界见[用量观察手册](./MEDIA_USAGE_OBSERVATION.md)。没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare 业务服务，未暂存/提交/触发远程 CI。只读获取了 Cloudflare 公开技术文档。

## 上一轮：主动默认图执行端（2026-09-11）

- 新增配套 `MEDIA_DELIVERY_MODE=normal|default_only`。日本 API 在展示 DTO 去掉图片/派生地址，覆盖公开目录和用户收藏、关注、历史；保留数据库、运营 manifest 与下架证据。私有 mode 指标可核对当前状态，不冒充账户使用量。
- 北京 `prepare` 在 SDK 创建/解码/Listing/写盘前拒绝，S3 适配器也阻止新准入；本地检查、恢复、物理删除和权利下架仍可用。根级/两地模板和环境校验已接入相同模式。
- Next 对新目录响应、HTML/OG/JSON-LD 做无副作用去图；请求级完整 CSP 对旧 ISR HTML 禁止远程 img/srcset，SafeImage 补水合前失败兜底。edge 配置尊重停图 no-store，不把它重写成公共缓存。
- 最终桌面/手机浏览器 **46/46 通过（3.6 分钟）**：新增同一缓存正常→停图→恢复，停图时模拟远程网络处理次数为 0、默认图可读、新详情/OG/JSON-LD 无远程地址，no-store 与匿名登录入口正常。Go API/Worker vet/test、216 项 Python + 123 subtests（91.20 秒；1 项 symlink skip）、15 项前端烟测、56 项工具合同、类型/lint/公开站构建、OpenAPI 漂移及两站 standalone HTTP 冒烟通过。
- 仍需账户 A/B 次数、月存储计量及自动触发、非必要任务限流和图片域名/源站全入口控制。直接图片 URL、旧已打开标签页及 Cloudflare 旧缓存不受页面 CSP 全局控制；不能称为账户零费用保护。部署时必须配套源站限制、Next 与 Cloudflare HTML 失效及目标验证。

实现和操作边界见[主动停图手册](./MEDIA_DELIVERY_MODE.md)，最终测试和截图见[验证证据](./evidence/release-a-media-delivery-2026-09-11.md)。正常模式的本地首页/详情桌面手机 LCP/CLS 探针也通过，属于无网络限速的合成数据实验，不替代目标性能验收。API 二进制已重新编译，未启动。没有启动 Docker、真实 PostgreSQL/邮件/S3/Cloudflare；未暂存、提交或触发远程 CI。目标保持完整且未完成。

## 上一轮：上传容量与中断保护（2026-09-11）

- 复现并修复两个 `prepare` 同时读取旧用量后均通过容量检查的问题。北京单机共享 OS 锁从解码前持有至上传校验及 manifest 持久化；并发失败返回 busy，不排队无限等待。容量改为汇总两桶全部当前对象，包括未管理前缀；分页/字节异常拒绝，不用部分结果放行。
- SDK PUT 前持久记录批次和对象证据。进程退出、超时未知写入、HEAD/manifest 失败保留 pending；正常释放系统锁也不能绕过未确认批次。只对已确认 PUT 做删除并 HEAD 确认后清理对应备份，未知写入和失败删除保留备份；碰撞不删已有对象。
- 新增本地 `upload-status` / `acknowledge-upload`，恢复要求人工先核对两桶、备份及旧在途写入，再按精确 run-id 显式确认并保留私有审计；命令不接触 S3，不伪装自动验证。下架删除不受上传准入限制。
- 根级/北京 Compose 已增加同一私有 `media-state` 卷，镜像预建 UID 10001 可写目录，环境校验器拒绝错误锁路径。部署前必须停旧上传器、核对既有卷权限；没有执行容器启动或目标 Linux 卷验收。
- 完整串行 Python **210 passed + 123 subtests**（79.91 秒），1 项 symlink 测试因本机权限明确 SKIP；Windows 真实子进程互斥/中断已测。全范围 ruff、15 个生产源文件 mypy、媒体 Linux 平台静态类型检查通过。Go API/Worker vet/test 通过，显式启用实际 Go→Python HTTP；真实 SQL 仍未执行。
- **当前已知开发缺口**：A 类 100 万/B 类 1000 万月额度、非必要作业自动限流、额度耗尽后主动默认图和已缓存/直连图片入口控制。不能用后台字节统计或失败兜底宣称账户免费额度全部受控；当前 10 GiB 也不是 10 GB/GB-month 的账单承诺。

恢复/升级见[容量手册](./MEDIA_UPLOAD_CAPACITY.md)，验证与未执行范围见[本轮证据](./evidence/release-a-media-upload-capacity-2026-09-11.md)。本轮按要求未启动 Docker、PostgreSQL、SMTP/S3/Cloudflare，未运行远程 CI、暂存或提交；未修改前端，不重复引用旧浏览器测试为本轮结果。M5、Release A 未完成，公网 no-go 不变。

## 上一轮：每日图片检查（2026-09-11）

- 日本 Go Worker 新增独立 24 小时检查：注册对象公开前缀/路径、URL-key、版本与权利/发布状态，以及公开关联的三档派生。草稿提前准备、共享资产、保留私有母版和有效删除排队不误报；只读业务状态，不自动删图或修复。
- 实际 GET 两张固定默认 SVG：必须 200、正确 MIME、完整有界内容并匹配发布哈希。禁止重定向/记录 URL，单张 5 秒/64 KiB，整次 45 秒；库存每类 10000 条上限，超限/中断明确失败，不写“全 0 正常”。
- 新增迁移 18 与独立审计表，当前 57 张表/5 个公开视图；API、seed、监控、CI、受限合同门槛与文档同步 v18。后台新增未检查/异常/不可用/恢复四态，私有指标和四条告警已接入。API 无记录或检查过旧时为 degraded，问题样本不对公开用户开放。
- 最终 Go API/Worker vet/test 和两份二进制编译通过（只编译未启动）；两站类型/lint、运营站构建、OpenAPI 漂移、13 项前端烟测、56 项工具合同和两站 standalone HTTP 冒烟通过。桌面/手机 **44/44** 浏览器回归通过（2.9 分钟），两张截图已复核；完整 Python **188 passed + 93 subtests**（91.25 秒），相关 ruff 通过。
- 新增受限 PostgreSQL 并发调度/不可覆盖完成合同与 11 场景状态矩阵，已排在 CI 现有 Worker 合同后串行执行；本机两个 SQL 测试明确 **SKIP**，不算数据库验收。无 Docker、真实数据库/邮件/S3/Cloudflare、远程 CI 或代码提交；临时本地测试服务已退出。

规则与升级见[每日检查运行手册](./MEDIA_INSPECTION.md)，结果与边界见[验证证据](./evidence/release-a-media-inspection-2026-09-11.md)。M5、Release A 整体验收仍未完成，公网 no-go 不变。下方各轮记录保留历史版本/计数，旧“未开发”措辞不代表当前状态。

## 上一轮：图片对账确认与调度（2026-09-10）

- 已用本机 HTTP 复现并修复“缺少计数的报告被当作全 0、对账完成”。Worker 现在核对完整字段、身份/计数/样本、HTTP 200/JSON、UTF-8 和 64 KiB 上限；无效响应记 invalid_report，不写完成。
- Python 对六类样本分别做 50 条/8 KiB JSON 预算，保留真实异常总数；长 Unicode 路径不会让合法报告突破接收上限。调度改为拿到 advisory lock 后使用新快照读取最近运行，库存仍是单语句快照，防止同周期重复扫描的旧快照风险。
- 实际 Go→Python HTTP 合同覆盖保留母版、服务失败、缺失、孤儿、真实删除发送器/接收器和删除后模拟库存恢复。新 SQL 并发合同已接 CI 串行门槛，本机明确 SKIP；容量/库存仍包含所有未物理删除旧图。
- Go API/Worker vet/test、Worker 编译、13 个报告 HTTP 场景/12 个语义负向场景、Python 服务 9 项、CI 合同 13 项、相关 ruff/mypy/OpenAPI 漂移通过。最终串行 Python **184 passed + 88 subtests**（82.10 秒）；见[对账验证证据](./evidence/release-a-reconciliation-2026-09-10.md)。无 Docker、真实供应商或远程 CI。
- 复核确认每日公开图片发布状态与默认图可读性检查尚未接入；已补回 M5 开发待办，不能以清单文件对账或前端回归替代。

## 上一轮：主图替换与 30 天保留接入（2026-09-10）

- 新增主图查询及专用替换 API，要求 admin/owner，替换要求近期密码确认。以预期旧资产 ID 校验并发，父资料锁、旧资产锁和关联重验保护切换；新资产、旧关联隐藏、审计、缓存 outbox 与删除任务同事务。
- 无其他 published 关联时，旧公开图立即排队删除、私有母版 30 天后到期；仍有其他关联的旧资产保留，权利下架可提前保留期限。201 不等于物理删除或所有缓存已刷新。
- 后台读取当前主图并要求额外确认；读取失败、409 或不完整响应保留清单并暂停提交，刷新后必须重新确认。新资产已是主图时阻止重复写入。手机图片对象表格补上完整横向滚动，避免通用表格样式隐藏字段或挤压行高。
- 三组新增 PostgreSQL 合同覆盖作品/人物 CAS、30 天期限/权利提前、共享保留/回滚、并发仅一方成功；本机明确 SKIP，不启动数据库。Schema 仍为 v17，无新增迁移。OpenAPI 为 83 个操作、72 个路径、95 个组件 Schema，18 个高风险操作要求近期密码。
- Go API/Worker vet/test（含实际 Go→Python HTTP）、API 编译、两站类型/lint、运营站构建、13 项前端单元烟测通过；图片表格视觉调整后，最终桌面/手机浏览器回归 **42/42** 通过（2.6 分钟），两张截图复核并保存。首轮 Python 发现新接口 YAML 逗号解析问题，修正后最终完整回归 **182 passed + 88 subtests**（82.86 秒）；44 项产品合同 + 7 个 subtests、相关 ruff 和 OpenAPI 漂移亦通过。
- 最终 standalone HTTP 冒烟与 77 个文档本地链接检查通过。未启动 Docker、真实 PostgreSQL/SMTP/S3/Cloudflare，未触发远程 CI 或提交代码；临时测试 HTTP 服务已退出。

详见[主图替换证据](./evidence/release-a-media-primary-2026-09-10.md)。M5 仍未整体验收，公网 no-go 不变。下方各轮状态、计数与“尚未开发”描述为当时的历史记录，不代表当前仍缺替换 API。

## 上一轮：重复下架与旧图退出基础（2026-09-10）

- 图片删除改为按 `media_object_id` 复用任务，重复权利工单保留原 URL、payload、租约与失败次数；pending/retry 的期限只能提前，不得推迟。公共对象立即到期，内部退出函数支持私有保留期限，权利下架将其提前；未确认删除前不减少数据库容量统计。
- 先锁已有 outbox、再锁对象，匹配 Worker 完成顺序。原 URL 缺失时只从资产/scope/key/北京路径完全匹配的旧任务恢复，不使用来源页或猜测地址；不存在可恢复地址则回滚，不返回假成功。旧工单式任务保留历史，新规范键至多带来一次每对象重复删除，仍依赖远端幂等。
- 用事务替身复现并修复“草稿无主站 publication 导致下架 409”：现在能撤下已准备图片的未发布资料，不伪造 publication；缺失实体仍拒绝。HTTP 错误不返回 completed，OpenAPI 明确工单完成不代表远端删除完成。
- 五组受限 PostgreSQL 合同已加入当前 CI 可运行的数据库测试包，本机均明确 SKIP。主图切换 API/后台操作仍未开发，30 天保留只有内部基础及测试夹具，不能把它标为实际可用功能。整体 Schema 保持 v17，无新迁移。
- 本轮 Go API/Worker vet/test、API 编译、实际 Go→Python HTTP、旧地址匹配/Worker 新字段兼容专项通过；Python 181 项 + 88 个 subtests 通过（81.49 秒）。OpenAPI 漂移、两站类型/lint、13 项前端烟测、standalone HTTP 冒烟和相关 ruff 通过；无 Docker、真实供应商、远程 CI、重新前端构建或 Playwright。

本轮证据见[媒体退出与删除调度](./evidence/release-a-media-retirement-2026-09-10.md)。下方图片登记段落为上一轮记录，其中重复下架的开发缺口已由本轮修复，真实 SQL 证据仍待执行。

## 上一轮：图片登记与公开隔离（2026-09-10）

- 图片关联、审计与 `cache_purge` 同事务提交，失败不返回成功；登记前锁定父资料并拒绝已权利下架或已合并的目标。
- 新增迁移 17：公开图片视图必须匹配作品/人物的主站公开发布视图；不再仅凭图片自身的 published 状态放行。API readiness、种子、监控、CI 和受限测试库门槛同步为 Schema v17，PostgreSQL 引擎仍为 16。迁移文件已开发，但未实际执行。
- Go API/Worker vet/test 与 API 编译通过；真实 Go→Next 缓存合同 9 场景通过，包含不改文字版本的图片新增、未失效负向对照及隐藏后无图片信息。新登记 SQL 两组本机 SKIP；另有作品/人物共 22 种公开状态组合、隐藏与恢复的受限读者 SQL 测试，已接 roundtrip，尚未执行。
- 本轮串行 Python 回归 180 项 + 88 个 subtests 通过（83.85 秒）；相关 ruff、OpenAPI 漂移、两站/合同包类型、两站 lint、56 项工具合同、13 项前端烟测和 standalone HTTP 冒烟通过。未重新构建前端或运行 Playwright；缓存合同与 HTTP 烟测使用已有 production 产物。未启动 Docker、真实供应商或远程 CI。
- **M5 图片模块未完成**：主图替换、旧母版私有保留 30 天及清理、重复下架删除任务的原 URL 保留仍需开发。之前“没有已知核心代码缺口”的判断已被本轮复核修正，不能把这些开发项归为缺少外部凭据。

详细实现、验证边界和下一步见[图片登记与隔离证据](./evidence/release-a-media-publication-2026-09-10.md)。以下为先前轮次记录，旧测试计数和 Schema 版本保留其历史含义。

最新完成角色会话加固：真实角色变化时，在同一事务更新角色、撤销该账号旧 session 并写审计；重新登录按 user 30 天、editor/admin 8 小时签发。相同角色返回 409、不重复撤销或审计；后台补上操作前说明、失败不误报成功与成功提示。Go vet/test、事务/HTTP 专项、运营站 build、两站类型/lint 和完整桌面/手机 32 项浏览器回归通过。两组新增真实 PostgreSQL 合同明确 SKIP，不代替实际 SQL 验收。详见[角色会话验证](./evidence/release-a-role-sessions-2026-09-10.md)。以下密码与退出段落保留为先前执行记录。

本轮完成密码计算保护：全进程 1 个计算、2 个等待、最多等待 500ms；超载 `503 AUTH_BUSY` 可重试，取消不提前释放在途名额、不写凭据。邀请改为先检查验证码/次数/额度，再计算并在最终事务重验，预检不双计额度。新增私有聚合指标与安全的可重复邀请 SQL 测试夹具，无 Schema 迁移。Go vet/test、专项和 OpenAPI 漂移检查通过，串行 Python 174 项 + 78 个 subtests 通过；真实 Argon2 8 请求突发的计算峰值为 1。SQL 合同本机仍 SKIP，日本限额下 RSS/CPU 验收未做。详见[密码计算护栏证据](./evidence/release-a-password-work-2026-09-10.md)。下方退出与浏览器测试计数保留为上一轮历史证据。

最新本地回归：退出确认加固的 8 个 Go HTTP 子场景、Go API/Worker vet/test（含真实 Go→Python HTTP）、两站构建/ESLint/类型、桌面与移动 32 项浏览器回归、HTTP 冒烟、当前构建 Go→Next 缓存 7 场景、54 项工具合同、13 项前端烟测，以及最终串行 Python 172 项 + 73 个 subtests 均通过；新 API 二进制已编译但未启动。真实 PostgreSQL 生命周期合同明确 skip。未启动 Docker、连接 PostgreSQL/SMTP/S3/Cloudflare 或触发远程 CI。最新修复、先失败后恢复的证据与 4 张退出提示截图见[退出确认验证](./evidence/release-a-logout-confirmation-2026-09-10.md)；此前账号快照加固见[账号生命周期](./evidence/release-a-identity-lifecycle-2026-09-10.md)。

## 当前阻塞与需要外部准备

角色会话本轮最终回归：Python 175 项 + 78 个 subtests、修改的合同 ruff、13 项前端单元烟测与两站 standalone HTTP 冒烟通过。没有启动 Docker/真实数据库/外部供应商，没有触发远程 CI；最新 API 二进制只编译未启动。下面“本轮最终补验”指此前密码护栏轮次，保留原执行边界。

本轮最终补验：Go vet/test 再次通过，关键排队/取消与邀请预检回归重复 20 次通过；两站/合同包 TypeScript、两站 ESLint、13 项前端单元烟测、修改的 Python 合同 ruff 通过。最新 `.cache/core-e2e/platform-api.exe` 已重新编译但未启动；本轮没有重新构建前端或运行浏览器 E2E。

文档同步后，安全/反向代理合同 79 项 + 11 个 subtests 通过，相关六份文档 39 个本地链接有效。新增邀请 SQL 三组在本机明确 SKIP，没有把测试套件的整体退出成功当成数据库验收。

本轮退出确认：已复现并修复 Go 撤销失败仍返回 204、两站忽略失败清 Cookie、内部 localhost 跳转问题。API 使用 2 秒截止时间、失败 503 保留 Cookie；两站上游 3 秒且只认 204，失败进入可重试的未确认页。保持后台 no-referrer，通过严格 Fetch Metadata 同源导航例外兼容浏览器原生表单的 null Origin；跨站或缺少证明的请求仍拒绝。Nginx 与 robots 已补 `/logout` 边界。需先更新入口规则，再全部 API，最后两站；旧 API/前端混跑不能视为生效。SQL 合同新增幂等退出和首次撤销证据保护，现在共 5 组，本机未实际运行数据库。

上一轮账号加固：登录写 session 时锁定并核对刚验证过的账号 UUID/邮箱/密码摘要/角色与 active、邮箱验证状态；密码重置同时绑定挑战中的用户 UUID，阻止旧账号验证码跨越同邮箱重新注册。退出、重置、关闭、owner 密码轮换及后台停用的撤销时间不早于 session 创建时间，避免并发时间差触发约束并回滚。这些风险来自代码/Schema 审查，未在真实库复现；最初 4 组 SQL 合同已纳入现有受限 API 登录的 CI migrations，实际行锁与事务证据仍待执行。该轮无需迁移或 HTTP 合同变更，依赖全部 API 实例替换后才可使用新保证。

修复 outbox 租约与重试边界：用可控时钟复现旧 Dispatcher 在 30 秒租约外完成第 1 条并继续执行第 2 条，且处理器没有截止时间。现在副作用和结果落库均受本批剩余租约约束，关闭/过期不再提交旧结果。Claim 返回数据库截止时间；Complete/Fail 校验捕获的 `attempt_count`、Worker 与双侧时钟期限，媒体完成先锁定 outbox 行再更新对象。耗尽 10 次的可领取事件进入 `dead/attempts_exhausted`，避免崩溃重领递增到数据库 20 次 CHECK 约束外并卡住新事件。真实 PostgreSQL 的 5 类合同使用独立空测试库和受限 Worker 登录，进入 migrations 必过步骤；本机尚无 SQL 运行态证据。无需迁移，升级不能新旧日本消费者并跑；外部副作用仍需幂等。

补齐缓存运行态证据：`npm run test:cache:release-a` 用真实 Go 签名发送器调用当前 Next.js production build，以合成目录验证首页、作品详情和 sitemap 的实际变化。未触发刷新、错误签名、过期签名三个负向对照均不回源；有效发布立即更新标题与 sitemap 时间，下架后移除公开内容与链接，同一事件重放仍安全。测试限制在缓存 300 秒 TTL 到期前完成，不以自然过期冒充失效成功。连续回归发现共享 Next 数据缓存被测试污染后，已改为独立临时 standalone 副本并验证源缓存不变，结束后清理自有目录。该合同进入统一预检与 CI web 必过门槛；目录 API 为内存替身，不代表 PostgreSQL outbox 调度、真实 Go API、Cloudflare 或完整部署已验收。

修复 Worker 副作用“假成功”：此前缓存刷新和媒体删除仅按 2xx 完成任务，已用返回 HTML 的 200 响应复现误标 completed。现在强制 200/JSON/完整 UTF-8/4 KiB 上限和事件 ID 校验，媒体删除额外校验 scope/key；Python 删除服务只在实际步骤完成后返回对象回执。漏配 dispatcher 处理器也不再退化为校验后完成。新增 30 个回执场景、缺少处理器保护以及真实 Go→Python HTTP 的失败→重试→恢复合同，后者已在本机执行通过并接入 CI；S3/Cloudflare 在该合同中用临时文件替代，真实 PostgreSQL 和对象存储验收仍待完成。升级顺序必须北京媒体服务先、日本 Worker 后。

新增真实核心服务 CI 门槛：`core-e2e` 使用独立 PostgreSQL 16/Mailpit、受限 API 数据库登录和编译后的 Go API，串联随机 owner 引导、邮箱账号流程、邮件邀请 editor、异人审核发布与隐藏；失败会阻断五镜像交付，只保留脱敏 JSON 报告。接线时修复旧邮件 E2E 的真实缺陷：创建邀请前缺少近期密码确认；现明确执行 reauth 并在失败时停止。运行器只接受固定名称的回环专用测试库，不创建或重置数据库，也不启动本机 Docker。当前已加入代码与离线合同，真实作业尚未运行；Worker、前端真实集成和目标服务仍是独立门槛。

可直接照着准备的清单：[Release A 联调准备清单](./RELEASE_A_PREP_CHECKLIST.md)。

已登记的退出确认、Argon2 计算护栏、角色变化后旧会话失效三项本地加固均已实现并有本地回归；这不等同于整体 Release A 验收完成。角色/邀请/账号生命周期 SQL、密码护栏的目标内存/CPU，以及其他真实服务端到端证据仍未完成，不能声称日本 API 不会 OOM 或 SQL 并发已通过。负责人和验收条件见[安全审计](./RELEASE_A_SECURITY_REVIEW.md)。

真实环境验证另外需要下列准备。仓库已有合成 fixture，因此第一轮联调不需要先提供真实资料：

| 事项 | 需要你准备/确认 | 当前是否阻塞本地代码 |
| --- | --- | --- |
| PostgreSQL 16 | 日本服务器上的测试库地址、owner 迁移账号、网络白名单；或确认后续用 Docker 临时库 | 否；阻塞真实迁移、权限和发布验收 |
| 邮件 | 测试期可用 Mailpit；真实环境需 SMTP 地址、发件邮箱和域名 DNS（SPF/DKIM/DMARC 后置） | 否；阻塞注册、重置、关闭和邀请 E2E |
| 图片存储 | S3/R2 endpoint、私有/公开 bucket、访问密钥、公开图片域名；两桶必须分开 | 否；阻塞真实上传、对账和下架删除 |
| 域名与入口 | display/ops/api/media 域名、Cloudflare Zone、Origin Certificate；后台固定 IP/VPN 或 Access 策略 | 否；阻塞公网边缘验收 |
| 首批内容 | 可先使用仓库合成 fixture；验证真实运营时再提供 5～10 部作品，图片可暂缺并使用默认图 | 否；只阻塞真实内容验收 |
| 初始化账号 | 首个 owner 邮箱和一次性强密码 | 否；阻塞后台登录联调 |
| 运营政策 | 权利下架联系人、隐私/历史归档保留周期、18+ 文案和是否开放公开注册 | 否；缺省值可继续用测试占位配置 |

以上信息不需要一次性全部给出。建议先准备“测试 PostgreSQL + Mailpit + owner 邮箱”，即可使用合成 fixture 完成第一轮业务联调；测试图片、S3/R2、域名和公网访问放在联调通过后接入。密码和密钥只写入服务器私有 `.env`，不要通过聊天发送。

真实发布执行统一使用 [Release A 上线执行记录](./RELEASE_A_GO_LIVE_RECORD.md)：默认保持 no-go，只有范围冻结、负责人、发布窗口、真实环境证据、回滚触发和稳定期检查均完成后才记录 go/completed。实施计划中的旧前置数据要求已修正为仓库 fixture，不再误导用户必须先准备 30 人/100 作品。

CI 的 web 作业现会在 Chromium 安装后生成固定路径的 12 张合成视觉截图，执行浏览器 LCP/CLS 和首页/番号搜索 HTTP P95 门槛，再运行桌面/移动 Playwright；视觉与性能 JSON 作为 `release-a-web-evidence-<commit>` 保留 14 天。现有安全合同锁定这三类命令、Chromium 通道和证据上传，防止后续只保留功能 E2E 而丢失视觉/性能门槛。

首个 owner 的生产引导不再要求宿主机安装 Go 或保留源码：`platform-api` 镜像同时编译静态 `/create-owner` 工具，日本 Compose 新增只在 `tools` profile 下运行的 `owner-bootstrap`，并等待迁移与运行账号配置成功。邮箱和一次性密码只通过受控终端环境传入，工具容器执行后删除；环境检查器会在显式提供引导变量时校验真实邮箱与 12～128 位密码且不输出值。

统一预检的 Python 解析已与 `run_python.mjs` 对齐：未传 `--python` 且未设置 `PYTHON` 时，Windows/Unix 都会优先使用仓库 `.venv`，最后才回退系统命令。此前 Windows 无全局 `python` 时预检会在 OpenAPI 漂移检查处报 `ENOENT`，现有同一测试方法同时覆盖缺失显式参数、仓库虚拟环境和显式解释器优先级。

统一预检的 Compose 静态校验现在使用仓库 `.cache/docker-config-preflight`，不读取用户级 Docker registry 配置；日本模板一次覆盖 `edge`、`monitoring` 和 `tools` 三个 profile，CI 同步锁定。这样既消除受限环境中的用户配置权限警告，也保证新增 `owner-bootstrap` 不会逃过发布门槛；仍不会启动容器。

修复“文档说真实联调可用 fixture、生产迁移却没有安全加载入口”的矛盾：新增 `database-fixtures` 一次性 tools 服务和 `load_release_a_fixtures.sh`。外层必须同时开启允许开关并输入固定确认短语，SQL 还要求 Schema v16、显式 psql 标志，并拒绝存在非 fixture 用户或目录数据；开发 seed 的两个固定审计账号改为关闭且不可登录。推荐顺序为 migration → runtime logins → 可选 fixtures → owner bootstrap，自动采集仍保持关闭。

fixture 加载边界进一步收紧：SQL 现在只接受值精确为 `1` 的 `release_a_fixture`，加载器使用 `--no-psqlrc`，并在单事务中以 5 秒 lock timeout 锁住会被写入的 13 张目录/审核表，避免空库检查与在线写入竞态。新增 5 项执行级 Shell 测试验证关闭开关、错误确认、缺失文件、数据库失败和成功参数；PostgreSQL roundtrip 还会验证 `0` 被拒绝、生产加载器双次幂等以及出现非 fixture 来源后拒绝。该 roundtrip 已进入 CI 配置，但本机没有 PostgreSQL，因此仍等待 CI/目标 PostgreSQL 16 产生运行证据。

最新 `npm run preflight:release-a -- --skip-build --with-compose` 已使用自动解析的仓库 Python 完整通过：Go test/vet、OpenAPI 漂移、TypeScript、ESLint、生产依赖 0 漏洞、ruff、mypy、144 项 Python 与 54 个 subtests、46 项工具合同、13 项前端烟测、standalone HTTP 冒烟和三套 Compose 静态配置均为 passed。没有启动容器；Next.js production build、Playwright 12 场景/桌面移动 24 项和两类性能探针沿用同日已通过的独立证据。

修复 CI 与监控交付缺口：仓库当前主分支为 `master`，CI push 触发由仅 `main` 改为同时覆盖 `main/master`；本地和日本 monitoring profile 新增 Alertmanager，Prometheus 告警会进入 9093，生产配置按 critical 与 warning/info 分流且恢复时通知。SMTP 密码仅从 Alertmanager 私有密码文件读取，环境检查器会拒绝占位配置、内联密码、缺少分级路由和过短密码文件。加入最小化主机监控后仍保持 Release A 2C4G 总 CPU 上限约 2.0、启用监控后内存约 2.9 GiB；静态合同、环境校验、ruff、mypy 和两套 Compose monitoring 解析均通过，真实邮件送达仍待目标环境。

补齐 API 延迟运行态告警：Go API 的请求耗时从只有 `_sum/_count` 的伪 summary 改为固定 10 桶 histogram，额外包含与产品门槛对齐的 300ms/500ms 桶；method、规范 route 与 status class 均保持有界，动态 slug、番号和查询词不进入标签。Prometheus 新增公开目录 API P95 `>=300ms` 和番号搜索 P95 `>=500ms` 两条 warning 规则，并分别要求 10 分钟内至少 20/10 次成功请求且持续 10 分钟，避免测试期低流量单次慢请求误报。Go 专项测试、监控合同、告警 YAML 解析和包含 140 项 Python 的完整无 Docker 预检已通过；真实 Prometheus 规则加载、流量触发和 Alertmanager 邮件仍待目标环境。

补齐日本主机饱和度监控：monitoring profile 增加最小化 `node-exporter`，仅启用 CPU、meminfo 和 filesystem 三个 collector，固定非 root、只读根文件系统/`proc`/`sys` 挂载、丢弃全部 capabilities、限制进程数且只绑定 `127.0.0.1:9100`。Prometheus 新增采集与 exporter-down、根分区 80% warning/90% critical、内存 80% 和 CPU 85% 持续告警；将 Prometheus CPU 上限由 0.10 调为 0.07、node-exporter 限为 0.03，使日本全栈 CPU 总上限仍约 2.0。两套 Compose 静态解析和监控安全合同已通过，真实 Linux 主机指标与告警仍待目标服务器加载验证。

补齐 PostgreSQL 连接池运行态指标：API 通过独立 observability 合同读取 pgxpool 内存快照，导出 acquired/idle/total/max、连接池利用率、成功获取、无空闲连接获取和上下文取消计数，不发起额外 SQL。连接池利用率 `>=80%` 持续 10 分钟触发 warning，可与 API P95、5xx 和 PostgreSQL 慢语句日志交叉定位。实现保持 database/http 依赖方向解耦，Go API/数据库专项测试与第 4 项监控合同通过；真实并发负载和连接池告警仍待目标 PostgreSQL。

补齐日本 Worker 调度循环运行态监控：每轮 outbox poll 上报心跳、成功/失败结果、最后成功时间和当前 claimed batch 数；连续失败计数在成功 poll 后归零，达到 3 次时 `/readyz` 返回 503，Prometheus 持续 2 分钟触发 critical。指标只包含区域与聚合计数/时间差，不保存事件 ID、载荷、番号或用户标识。最新完整无 Docker 预检通过：Go test/vet、144 项 Python 与 54 个 subtests、46 项工具合同、13 项前端烟测和三套 Compose 静态解析均为 passed；真实 PostgreSQL 故障注入、Prometheus 规则加载与告警送达仍待目标环境。

补齐按 Request ID 查询审计记录的后台闭环：新增 `GET /admin/v1/audit-logs?request_id=...` 与“审计查询”页面，仅 admin/owner 可用，editor 返回 403；Request ID 必须精确匹配有限 ASCII 安全格式，结果按时间正序且最多 200 条。响应只披露操作者、动作、对象、理由与精确时间，不返回 revision 前后快照或原始 metadata；复用现有 `audit_logs_request_idx`，无需数据库迁移。OpenAPI 已更新为 70 paths/91 schemas，API 与前端类型、桌面/移动 E2E 及 12 张视觉证据均已同步。

加固周备份的防覆盖与安全保留：默认 `RETENTION_COUNT=4`，以私有目录锁排除并发，同秒名称冲突拒绝覆盖，使用同文件系统硬链接发布新文件。新备份、SHA-256 和 completed 审计成功后，才按 UTC 文件名生成完整清理计划；除最新 4 份外，额外保护最后一份完整已验证恢复点，以及所有未验证、证据不完整或损坏的备份。删除资格必须同时匹配本地文件和数据库复制/恢复证据，SQL 故障不会执行半份计划；超额保护输出 `retention_status=deferred`，不伪称始终只占 4 份空间。新增并发、同秒重试、时钟回拨、校验异常和审计失败用例，并修正 Windows Shell 日期 mock 的优先级。PostgreSQL roundtrip 已接入真实 SQL 状态转换与路径/哈希/字节不匹配合同，但本机未执行真实数据库；操作手册已同步锁恢复、退出码 73/75 和补验要求。

## 2026-09-08 安全复核

- 修复生产依赖告警 `GHSA-2v37-7h3g-55p8`：根级 override 固定 `nanoid 3.3.18`，安装树与 lockfile 一致，`npm audit --omit=dev --audit-level=high` 为 0；
- CI 新增生产依赖 high/critical 发布门槛，并有合同测试防止移除精确版本、锁文件修复或审计步骤；
- 两个 Next.js 应用新增 CSP；公开站只为 Cloudflare Turnstile 放行脚本、连接和 frame，运营后台不放行该第三方域名；
- Nginx 公开站 `X-Frame-Options` 已由 `SAMEORIGIN` 统一为 `DENY`；修复 location 自定义 `Cache-Control` 时不继承 server 级 `add_header`、导致部分安全头丢失的问题；
- 反向代理合同 13 项、安全合同 52 项、前端单元烟测 13 项通过；两站 production build、TypeScript、ESLint 和 standalone HTTP 冒烟通过，HTTP 冒烟会检查 CSP、反嵌入、`nosniff`、Referrer 和 Permissions Policy；
- 新增 [Release A 安全审计](./RELEASE_A_SECURITY_REVIEW.md)，登记威胁模型、负责人、未关闭风险和公网放行条件。当前仍只建议进入真实测试环境，不建议公网开放。
- 本轮统一预检使用 Python 3.12.14 完整通过：Go test/vet、OpenAPI 类型生成/漂移检查、TypeScript、ESLint、生产依赖审计、ruff、Python 全套 130 项、前端 13 项单元合同、两站 production build 和 standalone HTTP 冒烟均通过；默认参数未启动 Docker，也未执行 Compose。
- 三套 Compose（本地、日本 edge、北京）`config --quiet` 均通过；Go race test 已尝试但本机 `CGO_ENABLED=1` 缺少 `gcc`，属于工具链限制，待 CI/服务器复跑。
- 同一 Python 3.12.14 环境已一次性复验 collector 5、media 16、db 17、backup 4、reverse-proxy 13、security 52、scripts 23，共 130 项全部通过；ruff 同轮通过。
- 修正日本 Nginx 与产品缓存目标不一致的问题：公开首页、作品、人物、厂牌等目录 HTML 现在默认 `s-maxage=300`，账号、登录/注册、搜索、API 和后台继续 `no-store`；反向代理合同和日本 Compose 渲染通过。Cloudflare 最终规则必须沿用同一边界。
- 进一步收紧缓存：公开目录只有 GET/HEAD 才带公共缓存头，POST/PUT/PATCH/DELETE 和其他方法强制 `private, no-store`，避免错误配置或非读请求被边缘缓存；反向代理合同 12 项继续通过。
- 缓存响应再按上游状态收紧：只有 2xx 的 GET/HEAD 公开目录响应带 5 分钟缓存，4xx/5xx 统一 `private, no-store`；日本/北京 Compose 静态渲染、代理合同和前端 HTTP 冒烟通过。
- 作品、人物和厂牌详情进入时各上报一次浏览；前端使用 1 秒短窗口去除 React Strict Mode 的开发期重复挂载，同时按时间戳清理去重键，稍后重新进入仍会计数且不会随长期浏览无界积累。新增 HTTP 回归直接固定“游客只写小时聚合、有效登录同时写精确历史”的隐私边界。公开站 TypeScript、ESLint、两站 production build、52 项安全合同、13 项前端单元烟测和 standalone HTTP 冒烟已复验通过。
- 完成实施文档与代码复核：Release A 的真实数据库实现统一为 `pgx + 类型化 repository + 参数化 SQL`，不再把尚未采用的 `sqlc` 写成现行技术栈；生产迁移使用仓库内 `psql` 迁移器读取 SQL 的 goose Up/Down 分段，不依赖 goose CLI，并以 advisory lock、连续版本校验、逐版本事务和 Schema 版本写入校验保护迁移。本地与日本 PostgreSQL 模板记录超过 500ms 的慢语句，并以 `log_parameter_max_length=0` 禁止绑定参数值进入日志。运行账号继续使用 API 15 秒、Worker 30 秒语句超时和 5 秒锁等待上限；三套 Compose 渲染、17 项数据库合同及 52 项安全合同通过。
- 新增 [Release A 数据模型与表清单](./DATA_MODEL.md)：从 v1-v16 迁移归纳三 Schema、56 张表、5 个公开视图、核心 ER 关系和 Release A 数据不变量；新增静态合同确保迁移中出现的每张表/视图都被文档列出。数据库迁移与数据模型离线合同现为 17 项并全部通过。搜索结果点击率只有产品目标、尚无事件口径，当前明确只能计算搜索量和零结果率，避免把未实现指标误报为可用。
- OpenAPI 前端合同由手写类型改为确定性生成：仓库内生成器覆盖当前 70 paths 下的全部 91 个组件 Schema，处理引用、枚举、常量、数组、对象和 `allOf/oneOf`；包含 `ApiError`、`FeedbackListResponse`、`ConflictResolution` 与最小披露审计响应命名合同。统一预检在 TypeScript 之前检查生成文件漂移，CI 同样阻断；生成器、三套 TypeScript、ESLint、两站 production build 和 standalone HTTP 冒烟通过。
- 生产部署配置新增静态合同：日本和北京 Compose 引用的每个变量都必须出现在各自 `.env.example`，同时拒绝重复键，避免新增部署参数后运行手册和服务器变量清单漏更新；加入一次性 owner、fixture 和 Alertmanager 私有文件变量后，两套模板当前分别覆盖 60 和 20 个变量引用。TypeScript 增量缓存也已加入忽略清单，避免误提交本机构建产物。
- 新增 `scripts/release_a_catalog_e2e.py`：使用不同的录入与审核账号自动执行作品创建、任务领取、近期密码确认、批准、发布、番号搜索、详情/sitemap 验证和隐藏后不可见验证。密码只从本机环境变量读取，输出不含密码；发布后任一断言失败会尽力隐藏合成记录。离线测试固定完整调用顺序及“作者不能自审”边界，真实执行仍需 PostgreSQL/API 和两个运营账号。
- 邮件/邀请 E2E 同步移除 owner 与邀请账号密码的命令行参数，改为指定名称的本机环境变量；关闭 argparse 参数缩写，避免 `--owner-password` 被误当成 `--owner-password-env` 接受。默认临时注册密码也改为每次运行随机生成。
- 修复生产客户端 IP 信任链：Nginx 在公开站、API 和后台三个动态入口统一覆盖 `CF-Connecting-IP`，两个 Next.js 同源代理只转发该受控值到 Go API；Go 仍仅在 `TRUST_PROXY_HEADERS=true` 时使用它。本地或绕过可信边缘的请求不会信任客户端自带头，避免注册、登录、验证码和反馈限流把全部用户错误聚合到同一个容器 IP。
- 统一 SEO URL 建议：运营发布队列对作品优先提取番号，对人物/厂牌使用可转写名称，并统一追加实体 UUID 的末 8 位稳定短标识；核心目录 E2E 也按 `{番号}-{short-id}` 发布和验证，降低番号复用或同名资料造成 slug 冲突的风险。后端仍保留人工覆盖 slug 的能力，历史测试 URL 不会被强制改写。
- 修正架构文档中过时的“北京反向代理运营后台”描述：浏览器后台实际由日本 `ops-web` 同源代理内部 Go API，北京只运行 Worker/媒体边界，不持有用户 session。
- 补齐 SEO 结构化数据：作品、人物和厂牌详情分别输出 `CreativeWork`、`Person`、`Organization` JSON-LD，包含绝对 canonical、番号/名称、公开图片和可用关联字段；统一序列化会转义 `<` 与 Unicode 行分隔符，避免资料文本闭合脚本标签。13 项前端单元烟测会解析真实 JSON-LD 并拒绝未转义 `<`；standalone HTTP 冒烟通过包含 `</script>` 攻击片段的合成作品标题复验三类详情。TypeScript、ESLint 和两站 production build 均通过。
- Playwright 浏览器套件现有 12 个场景并在桌面 1366×900、移动 390×844 两个项目执行，共 24 项通过：真实 Chrome 下完成匿名番号搜索与详情、账号功能、审核发布、邀请与用户权限、admin 按 Request ID 查询最小披露审计时间线、反馈 editor/admin 分权，以及权利请求登记→editor 只读→admin 近期密码执行→公开搜索撤下。审计场景同时锁定 editor 无入口/无权限、精确查询与不显示原始快照载荷。该浏览器证据不替代真实 PostgreSQL 权限或对象存储物理删除与 Cloudflare purge。
- 新增当前 Next.js production build 的桌面/移动全页视觉证据，覆盖公开首页、作品详情和运营后台登录。人工复核发现本地 stateful mock 缺少关注人物新作品接口，导致游客首页错误显示“关注动态暂时无法加载”；现补齐与真实 API 一致的 401/登录用户响应，游客改为正常登录引导，并由 Playwright 锁定不再出现错误告警。截图及证据边界见 [本地视觉证据](./evidence/release-a-visual-2026-09-10/README.md)。
- 本地预览进一步把正常展示夹具与 XSS 安全夹具拆开：首页示例 `TEST-001` 现在可按页面占位提示完成搜索、进入正常作品详情，并与示例人物混排；`TEST-JSONLD` 仅保留给脚本闭合转义回归。收藏、隐藏、历史和关注作品 mock 改为按作品 ID 工作，审计 mock 提供 `release-a:publish-42` 最小披露时间线。完整 Playwright 12 场景/桌面移动 24 项通过。
- 新增可重复的 `capture:visual:release-a(:build)`：在随机回环端口启动同一套有状态 mock 和两站 standalone build，用本机 Chrome 自动捕获公开首页、`TEST-001` 作品详情、后台登录、owner 运营概览、用户权限与审计查询页的桌面/移动共 12 张全页截图。输出被限制在 `docs/evidence` 子目录，全部成功后写入带视口、字节数和 SHA-256 的 manifest，并自动清理浏览器、mock、监听和临时资源。审计页在 1440×900 与 390×844 下均无横向溢出、坏图或脚本异常；TypeScript、ESLint、13 项前端单元烟测、46 项工具合同、HTTP 冒烟和 Playwright 24/24 均通过。
- 新增 `probe:performance:release-a(:build)` 浏览器性能门槛：本地合成模式在公开首页和正常作品详情的桌面/移动视口各预热一次、采样 3 次，保留 LCP/CLS/FCP/TTFB/加载时间/资源字节原始数据；LCP 中位数必须 `<2500ms`，移动 CLS 最大值必须 `<0.1`。最新统一预检的四组 LCP 中位数为 156–352ms，桌面 CLS 最大值约 0.00156、移动为 0，报告见 [本地性能证据](./evidence/release-a-performance-2026-09-10.json)。探针支持只接受 HTTPS origin 的最终域名模式，并作为统一预检可选 `--with-performance` 门槛；本地无网络限速/合成 API 结果不替代 Cloudflare 和真实用户验收。
- 新增 `probe:http-latency:release-a(:build)`：通过公开站同源代理对首页 API 与番号搜索 API 各预热 3 次、顺序采样 20 次，要求 200、有效 JSON、响应不超过 2 MiB，并分别执行 P95 `<300ms`、`<500ms` 门槛。最新统一预检的本地合成结果：首页 API P95 11.35ms，搜索 P95 6.30ms，[JSON 证据](./evidence/release-a-http-latency-2026-09-10.json) 不保存番号原文或响应正文。最终模式要求 HTTPS origin 和显式已发布番号；该实验室低并发结果不替代真实 PostgreSQL、网络、Cloudflare、Prometheus 或并发压测。
- 统一预检在干净 Python 3.12.14 虚拟环境中再次通过；`requirements-test.txt` 固定 ruff `0.16.6`，`ruff.toml` 固定 `E4/E7/E9/F/I` 基础规则和 120 列宽，避免 ruff 版本或默认规则漂移导致本地与 CI 结果不一致。ruff 自动整理的 16 个 import 仅为格式变更，未改变业务逻辑；随后 ruff、mypy、130 个 Python 测试、两站构建和 standalone HTTP 冒烟全部通过。
- 生产依赖审计在新 advisory 出现后曾短暂阻断；已将两个前端升级到 `next 16.3.3`、`eslint-config-next 16.3.3` 和 `sharp 0.35.4`，lockfile 与安全合同同步，`npm audit --omit=dev --audit-level=high` 当前为 0。
- 根级 `generate:api-types`/`check:api-types` 改用 `scripts/run_python.mjs`：优先显式 `PYTHON`，其次仓库 `.venv`，最后回退系统解释器；Windows 没有全局 `python` 命令时仍可运行。启动器的 2 项跨平台解析测试与 API 类型漂移检查通过。
- 依赖升级后再次执行 `npm ci --dry-run --ignore-scripts`，lockfile 可被 npm 正常解析；两个前端 Dockerfile 已改为强制 `npm ci`，镜像不会绕过已审计的 lockfile。生产审计、类型检查、Playwright 和完整 Release A 预检均恢复通过。工作区未启动 Docker，Docker daemon 当前不可用。
- API、Go Worker、公开站、运营后台和媒体镜像均增加本地健康检查；Go 使用自身 `--healthcheck` 探测 `/readyz`，Node/Python 使用回环 HTTP 探测。公开站/后台等待 API healthy，日本 Worker 等待 PostgreSQL healthy，日本 edge 再等待 API、公开站和后台 healthy；Prometheus 等待 API 与 Worker healthy 后启动，避免 Compose 仅以进程启动状态判断服务可用。健康检查安全合同、Go 单测和三套 Compose 静态渲染通过；CI 已显式同时启用日本 edge 与 monitoring profile 做静态校验。
- 新增 `scripts/release_a_env_check.py` 只读部署前校验器：core 检查日本核心生产配置，full 交叉核对日本/北京版本、媒体密钥、公开图片基地址、双 bucket 与 10 GiB 上限，并可验证证书、私钥和 metrics token 文件。错误输出只包含变量名，不包含变量值；5 项专项测试、ruff、mypy 和统一 Python 回归通过，CI/统一预检已将该脚本纳入类型检查。
- 新增 `.dockerignore`，明确排除 env、证书/私钥、开发 metrics token、Node/Python/Go 缓存、测试产物和运行数据，避免敏感文件或无关大目录进入 Docker 构建上下文。新增 `docker-bake.hcl` 统一 5 个 Release A 镜像；Go API/Worker 将构建版本写入二进制兜底值，全部镜像带 OCI version/revision 标签。CI 只有在 Go、Python、PostgreSQL 迁移、前端与 Compose 作业通过后才构建全部镜像、校验标签并上传 14 天 image inspect 清单，不自动推送 registry。Bake `--print`、Go 实际链接构建和安全合同通过；真实镜像构建仍由 CI/目标 Docker 主机完成。
- 新增 `scripts/release_a_deployment_probe.py` 部署后只读验收：统一 GET 公开首页、后台登录、API health/ready、Worker ready 和可选 metrics，核对同一构建版本、数据库依赖、入口安全头、公开/私有缓存边界、页脚资料/18+ 声明及广告仍为占位。metrics 与可选 Cloudflare Access 凭据只从环境变量读取，只发往对应入口，客户端禁止自动重定向；报告不保存凭据或响应正文。5 项专项测试覆盖通过、版本/数据库/广告/缓存/跨域失败、凭据缺失和 URL 安全边界，并使用两个本地 HTTP 端点确认跨源 302 不会触发第二次请求；CI/预检已纳入 mypy，加入探针后的完整统一预检已通过。
- 本轮再次静态解析根目录、日本 edge+monitoring、北京三套 Compose，且 `docker buildx bake release-a --print` 正确列出 API、Worker、公开站、运营后台和媒体服务五个版本化镜像；全程未启动容器或访问真实环境。

## 2026-09-07 快速复核

- 未启动 Docker、PostgreSQL、Mailpit 或任何真实外部服务；
- `go test`、`go vet`、公开站与运营后台 ESLint、TypeScript 检查均通过；
- 当前硬阻塞只影响真实端到端验收，不影响继续完成本地代码、自动测试和文档；
- 当时已补齐审核改派、近期密码确认、v16 readiness 和关键 HTTP 烟测；“没有已知核心代码缺口”的当时判断已被 2026-09-10 图片复核修正，当前以页首与剩余门槛为准。搜索结果点击率仍缺少经确认的匿名统计口径，不列为已实现。

## 已实现

- 单仓库的 Go API/Worker、两个 Next.js 应用、Python worker 骨架和 OpenAPI/TypeScript 合同；
- PostgreSQL 17 个迁移文件，`collector`、`platform`、`audit` 三 Schema；迁移 13 增加管理员邀请，14 绑定 revision 来源证据，15 增加邮件额度/熔断，16 增加搜索小时聚合，17 收紧公开图片父资料隔离；最新迁移尚无真实库执行证据；
- API/Worker 使用独立非超级用户数据库 LOGIN 并继承对应 NOLOGIN 权限组；owner 连接只用于迁移、备份和恢复，真实连接允许/拒绝矩阵与 fresh rollback 已验证；
- 生产迁移器使用 advisory lock、连续版本校验和逐版本事务；日本 `tools` profile 固定 migration → runtime login 顺序；v1-v12 已在 PostgreSQL 16 完成 fresh、运行账号权限合同和逆序回滚，v13-v16 已加入但本轮尚未执行真实数据库复验；开发 bootstrap 使用初始化期 Unix socket 创建运行账号；
- 确定性开发目录包含 30 个合成人物、100 部合成作品、审核 revision、公开 publication、搜索文档和 fixture 来源记录；重复执行幂等，不进入生产迁移；
- 公开首页、最新发行、最近收录、人物/厂牌列表与详情、作品详情和发布视图驱动的搜索；首页真实渲染按后台规则混排的发现流、最新作品、编辑推荐、人物入口、7 日热门和 30 日浏览最多；发现流默认每 4 部作品插入 1 个人物、首屏 10 条，admin/owner 可修改比例和窗口，修改写审计并通过 outbox 立即失效公开缓存；最新发行排除缺失实际发行日的作品，最近收录按首次公开时间排序；公开搜索、最新发行、最近收录、热门、人物和厂牌列表统一使用绑定查询作用域的 keyset cursor，每页 24 条；编辑推荐由 admin/owner 排期并自动过滤隐藏或下架作品；
- 搜索支持番号、标题、人物艺名/别名和厂牌，包含全角 ASCII/空白/大小写/分隔符归一；
- 作品、人物和厂牌详情读取无副作用，页面进入后单独上报一次；短窗口只去除 Strict Mode 重挂，不吞掉稍后的真实再次进入；游客仅写小时聚合；作品详情提供最多 8 条相关作品，仅从公开发布视图取数并排除当前作品，按同人物、同厂牌、同标签、发行日期接近和编辑推荐确定性排序；
- 注册、登录、退出、忘记密码和关闭账号，注册/重置/关闭均要求邮箱验证码；
- 公开站与运营后台使用同源 API 代理，session Cookie 留在各自前端 Host；生产写请求强制允许列表 Origin，CORS 预检支持 CSV 幂等头，访问日志只记录规范路由模板，请求失败日志不记录邮箱、用户/内容标识或底层错误文本；API 所有非 GET/HEAD 响应强制 `no-store`，公共只读资料保持可缓存；
- 请求关联 ID 仅接受有限 ASCII 字符集，包含控制字符或超长值时由服务端替换为随机 ID，避免污染响应头、日志和审计上下文；
- owner 可邀请 `admin`、`editor`、`user`，admin 只能邀请 `user`；公开站 `/invite` 通过邮箱验证码接受邀请并设置密码，后台可创建、查看和撤销 pending 邀请，邀请验证码和理由不会在公开接口返回；
- Argon2id、HMAC 验证码、哈希 session、Turnstile、邮箱/IP 限流和统一枚举防护响应；公开验证码请求在限流、重复发送、邮件或数据库失败时仍返回相同 202，真实原因只写安全分类日志，SMTP 全链路受 10 秒截止时间限制；
- 登录用户收藏、人物详情关注/取消关注、隐藏/恢复不感兴趣作品、精确到秒的历史、透明软删除历史和登录后反馈；收藏和关注按钮会加载真实初始状态，关注列表使用人物卡片，首页通过私有接口展示关注人物的新作品并排除已隐藏偏好；反馈进入后台人工队列，editor 可领取，admin/owner 可接受、驳回或关闭，状态变化写审计，证据 URL 不自动访问；公开发现 HTML 保持无账号状态，浏览器按私有 `no-store` 偏好过滤卡片；
- 公开站和运营后台的业务时间统一按 `Asia/Shanghai` 显示到秒，并使用语义化 `time[datetime]` 保留原始时间值；
- 公开页共享 HTML 立即展示游客“登录/注册”入口，私有会话检查成功后升级为账号菜单；API 暂不可用时入口仍保留并提供重试按钮，不再只显示账号加载或不可用状态；
- 运营后台登录、owner 引导、作品/人物/厂牌录入、人物关联、资料编辑、审核领取/批准/驳回、发布、目录内隐藏、编辑推荐排期、首页发现流配置、角色与账号状态管理；admin 可锁定/暂停/恢复普通用户，owner 可管理所有非 owner，停用立即撤销 session 并写审计；普通隐藏只对 admin/owner 的已发布资料显示并强制填写理由；编辑推荐支持编辑、暂停、恢复和审计式移除；已发布资料审核期间保持旧版本公开；审核领取仅开放给 admin/owner，editor 只能提交 revision；
- 审核任务领取后绑定领取人，只有领取人可以批准或驳回；admin/owner 可通过要求近期密码确认的显式改派接口选择 active admin/owner，事务校验任务与账号状态，并在追加式审计中保留原审核人、新审核人、理由、操作者和精确时间；不同运营人员不能通过决定接口隐式接管；
- 运营概览为 admin/owner 展示真实 outbox 积压、未物理删除媒体字节、最近已验证备份、最近媒体对账、对账问题和 24 小时失败数；依赖不可用时显示未知而不是零，editor 不读取系统运维状态；
- admin/owner 可将同类型、已发布的重复实体合并；事务迁移关系和小时浏览统计、隐藏源实体、移除源搜索文档与作品推荐、写合并映射/审计/cache outbox，并为旧公开地址建立 301；
- 字段冲突列表与人工裁决：editor 可查看，admin/owner 可处理；裁决只写结论和审计，不直接覆盖正式资料，最后一个冲突处理后原子关闭冲突任务；
- 作品 CSV 预检与幂等批次提交：限制 1 MiB/500 行，检查表头、必填值、日期、UUID、人物数量和批内番号重复；整批事务写作品、revision、审核任务、来源与标准化记录，支持错误 CSV 下载和最近批次列表；错误导出已中和表格公式前缀，不能通过恶意番号在管理员打开文件时执行公式；
- 受控人工图片链路：安全解码、去元数据、私有母版、三档 WebP、S3/R2 上传 HEAD 校验、北京逐对象副本和生成式 manifest；后台只读导入并登记一一映射；作品详情最多展示 1 张主图和 3 张精选图；
- 北京媒体处理器在共享单机上传锁内、任何副本/PUT 前汇总两桶全部当前对象和本批字节；突破默认 10 GiB 时整批拒绝。中断/未知写入保留证据和备份，恢复须人工复核。70%/85%/95% 为分级提示，超额拒绝新上传不影响文字/搜索/下架；A/B 次数及主动停图尚未接入；
- 权利下架登记与执行：公开发布、搜索和媒体链接事务撤下；HMAC 调北京服务物理删除 S3/R2、Cloudflare 缓存和北京副本，确认后才将数据库对象标为已删除并完成 outbox；
- 日本 Worker 每日生成数据库媒体清单，北京服务核对双桶对象与北京副本的字节数和 SHA-256，报告缺失、损坏和孤儿对象并持久化审计结果，不自动删除；
- 日本 Worker 使用 `FOR UPDATE SKIP LOCKED`、租约、指数退避和死信；发布类事件通过带时间窗的 HMAC 请求真实失效公开站缓存，非 2xx 不完成事件；北京 Worker 不持有数据库凭据；
- canonical、Open Graph、按固定页面/作品/人物/厂牌拆分的 sitemap 索引、robots/noindex、5 分钟公开缓存和统一 `public-catalog` 按需失效标签；根布局与公开详情不读取 session，账号菜单、隐藏偏好和详情操作在浏览器端通过同源私有 `no-store` 请求加载，避免用户状态污染共享 HTML；
- Bearer 保护的 API/日本 Worker Prometheus 指标、规范路由请求计数与固定桶耗时直方图、数据库/审核/outbox/媒体/备份/邮件额度与熔断/搜索零结果指标，以及本地和日本部署的 7 天/512 MiB Prometheus、Alertmanager 分级通知配置与 Release A 错误/延迟告警规则；
- 日本 Worker 可按保留门槛批量归档用户已清除的浏览历史：确定性 gzip JSONL、SHA-256、原子落盘、不可变 manifest 与事务性 `archived_at` 标记；首版保留数据库原行；
- 备份审计状态机与脚本：日本生成前登记运行、失败留痕、北京无数据库凭据校验副本、日本登记复制证据、空库恢复后才标记 verified；v8 数据库约束禁止无文件/复制/恢复证据的 verified 状态；v9 为 R2 母版/公开派生图增加强制存储范围；
- 日本生产 Compose 已包含 PostgreSQL、API、Worker、公开站、运营后台以及可选 Prometheus/Alertmanager；北京生产 Compose 包含无数据库凭据的 Worker 与媒体服务；两套模板均可使用各自 `.env.example` 做 CI 静态渲染，两个前端只在内部网络暴露，由反向代理控制外部访问；运营后台邀请账号流程已接入，正式开放前仍需固定 IP/VPN 或 Cloudflare Access；
- 两个 Next.js 应用均以 `output: "standalone"` 构建；本地 `npm run start --workspace ...` 会先同步当前 `.next/static` 和可用的 `public` 资源，再加载 standalone server，默认分别使用 3000/3001，避免直接预览时 CSS、图标和默认图 404；Docker runtime 也固定相同端口，不再调用不适用于该产物的 `next start`；
- 日本 Compose 新增可选 Nginx `edge` profile：TLS 三 Host、动态 no-store、Next 静态长缓存、内部失效/metrics 封锁和无客户端 IP/路径访问日志；静态合同与 Compose 渲染通过，真实 Nginx 镜像语法检查纳入 CI；
- 日本 2C4G 模板为全部服务设置 CPU/内存上限和 10 MiB × 3 日志轮转；含 edge 的常驻上限约 1.85 CPU/2.34 GiB，同时启用 Prometheus/Alertmanager/node-exporter 后约 2.0 CPU/2.9 GiB；
- API 在 `APP_ENV=production` 启动时会 fail-fast 校验 PostgreSQL、认证 HMAC、SMTP、Turnstile、允许来源和公开图片基地址，避免以“账号能力关闭”的降级状态误发布；开发环境仍允许页面壳降级启动；
- 公开站注册和重置页面在运行时读取 Turnstile 站点密钥并强制动态/no-store；生产缺少站点密钥时明确停用表单，开发绕过仅在开发环境或显式开关下生效；同一 standalone 镜像已分别验证“缺少站点密钥停用”和“提供站点密钥加载真实组件”；
- Turnstile 已从仅检查 `success=true` 加固为站点与操作双绑定：注册/密码重置分别使用 `signup_code`/`password_code`，Go API 精确校验响应 action 和 `TURNSTILE_EXPECTED_HOSTNAME`，限制 token/响应大小、JSON 类型和 5 秒超时，并拒绝重定向；本地专项、路由、配置、环境和前端合同通过，真实 Cloudflare/最终域名仍待验收；
- 日本和北京生产 Compose 采用服务级最小环境变量白名单，不使用 `env_file` 将整份配置注入容器；公开前端不持有数据库、SMTP、S3 或 Turnstile 私钥，北京 Worker 不持有数据库/S3 凭据，媒体服务不持有采集配置；
- 自动采集和广告联盟保持关闭，广告只显示占位。
- API 合同新增完整双向门槛：从 Go `chi` 路由注册和 OpenAPI `paths` 分别提取全部 80 个 method/path 操作并做集合相等校验；同时解析本地 `$ref`，校验所有路径占位符均有必填 path 参数、80 个 `operationId` 全部存在且唯一；
- 高风险认证合同补齐：OpenAPI 的“完成权利下架”操作现在与 Go 一致要求 `sessionCookie + recentAuthCookie`；自动测试固定全部 17 个近期密码操作集合，并补测编辑推荐更新和冲突裁决的 Go 路由策略；
- 关闭账号 HTTP 合同补齐：验证码必须绑定当前登录用户 ID 和邮箱，过期说明版本不能调用关闭服务，成功关闭后同时清除 session 与近期密码 Cookie；API、OpenAPI、前端及邮件 E2E 的说明版本由合同测试保持一致；
- Go Worker 现在真实读取 `COLLECTION_ENABLED`，Release A 若被配置为 `true` 会在启动校验阶段直接拒绝运行；三套 Compose 均固定为 `false`，不再依赖仅用于展示的配置或硬编码日志；
- Release A 前端 HTTP 冒烟新增广告边界门槛：除确认占位存在外，还拒绝常见联盟脚本、`active`/`floating` 状态、关闭倒计时和 popunder 信号；现有 `preview-*.png` 只作为早期原型设计基线，不作为当前 Next.js 构建的运行态视觉证据，其中 `preview-ad-layout.png` 的悬浮方案明确排除在 Release A 外；
- 当前 Next.js 构建已在真实浏览器完成公开首页桌面 1440×900、移动 390×844、API 不可用详情状态、Turnstile 缺失注册状态，以及运营后台登录页桌面/移动复核；均无横向溢出和坏图，桌面左右广告 rail 可见、移动 rail 隐藏。真实内容详情和登录态后台仍待联调环境；

## 2026-09-08 本地验证

```text
Go API 单元/数据库可选合同测试       `go test ./services/platform-api/...` 通过；未配置测试数据库的真实 PostgreSQL 合同按设计跳过
Go Worker                            `go test` 与 `go vet` 通过
前端 TypeScript                     公开站、运营后台和 api-contracts 通过
前端 ESLint                         公开站和运营后台通过，无警告
Next.js production build            公开站 20 个路由、运营后台 17 个路由构建通过
Python 离线回归                     Python 3.12.14 同轮通过 collector 5、media 16、db 17、backup 4、reverse-proxy 13、security 52、scripts 23，共 130 项；ruff 通过
前端运行态                          单元烟测 13 项、standalone HTTP 冒烟、Playwright 12 场景/桌面移动 24 项通过；覆盖资源同步、广告边界、响应式、番号搜索、账号、审核发布、邀请/权限、审计查询、反馈和权利下架公开撤除
干净 Python 预检                    `.venv` Python 3.12.14：ruff（固定 E4/E7/E9/F/I）、mypy 13 个源文件、pytest 130 tests + 54 subtests 全部通过
Compose 静态渲染                    本地、日本 edge、北京三套 `config --quiet` 通过；未启动容器
审核显式改派                        HTTP 角色/请求校验、领域接收人规则、高风险近期密码路径通过；真实 PostgreSQL 事务和审计待联调库复验
Readiness 合同                      TypeScript/OpenAPI 已包含 `schema_mismatch`，与 API v16 检查响应一致
CI Python 环境                      新增可复现测试依赖文件并在干净 runner 安装 PyYAML、Pillow、boto3、本地包和 ruff
CI PostgreSQL 合同                  v16 迁移与 seed 后使用受限 API 账号执行数据库 repository 合同，覆盖审核改派事务和审计
CI 前端运行态                      production build 后执行前端单元烟测、standalone HTTP 冒烟，并安装 Chromium 执行 Playwright 12 场景 × 2 视口
统一本地预检                       `npm run preflight:release-a` 汇总 Go、前端、Python、production build 和 HTTP 冒烟；`--with-e2e` 可纳入 24 项浏览器回归，`--with-performance` 可纳入预热 LCP/CLS 门槛，Docker/Compose 默认关闭
预检合同测试                       `npm run test:preflight` 46 项通过，覆盖 Docker/E2E 默认关闭、可选浏览器/性能门槛、本地预览、视觉捕获、浏览器性能与 HTTP P95 证据参数/端口/HTTPS/隐私/输出边界、移动端用户卡片、Python/mypy/pytest 门槛和跨平台 Python 解析
文档备份口径                       测试期每 7 天、正常保留 4 份；保护恢复点/未验证文件时暂缓清理；正式生产前再升级为每日备份和 24 小时 RPO
```

本轮不启动 Docker 的统一预检和 Playwright 浏览器回归状态均为 `passed`；使用现有 production build 在同一门槛中完成 standalone HTTP 冒烟、桌面/移动 24 项浏览器回归和三套 Compose 静态渲染。Go race 不属于该预检，当前仍因本机缺少 gcc 待 CI 或目标服务器复跑。完整 go/no-go 证据见 [RELEASE_A_READINESS.md](./RELEASE_A_READINESS.md)。

新增不依赖 Docker 的持久本地预览命令：复用有状态合成 mock 和当前 standalone build，在 `127.0.0.1` 同时提供公开站、运营后台和内部 API；默认端口为 4173/4174/4175，可覆盖。实际启动复核中三个入口均返回 HTTP 200，Ctrl+C 后监听全部释放；预览输出明确标记 `.test` 合成账号、内存状态和真实依赖未联调边界。启动失败时保留两个 Next.js 子进程日志，便于直接识别端口或配置问题。

新增不依赖 Docker 的视觉证据命令：随机分配回环端口，使用本机 Chrome 生成 6 个页面 × 2 个视口的标准全页截图，成功后写入 manifest 并释放所有监听。当前证据覆盖公开首页、作品详情、后台登录、owner 运营概览、用户权限和审计查询页；该证据仍属于本地合成数据，不替代目标环境真实登录态、图片和性能验收。

新增不依赖 Docker 的性能门槛：`--with-performance` 会先执行浏览器 LCP/CLS，再执行首页 API/番号搜索 HTTP P95。两份本地报告均通过产品阈值并保留原始样本；远程模式要求最终 HTTPS origin、真实已发布作品路径和番号。`npm run preflight:release-a -- --skip-python --skip-build --with-performance` 已完整通过并确认两个性能探针均被执行，统一预检编排合同和 46 项工具合同也已通过；完整 Python、重建和 24 项 E2E 证据来自同日通过的独立轮次。

## 2026-08-31 验证

```text
ESLint                                  公开站和运营后台通过，无警告
TypeScript                              公开站、运营后台和 api-contracts 通过
Next.js production build               公开站和运营后台通过
公开站 production 运行态                首页/最新/人物 200 且共享缓存 5 分钟；私有页 307/no-store；上游不可用代理 503/no-store
OpenAPI YAML                            67 paths / 84 schemas，可解析
Docker Compose config                   本地含 monitoring profile 通过
日本生产 Compose config                 API/Worker/双前端/monitoring 通过
Python 离线测试                         db 15、security 36、backup 4、collector 5、media 16 全部通过
邮箱 E2E 工具测试                       `scripts/tests` 3 项通过；可选 owner 邀请流程已接入脚本，待真实 API/Mailpit 执行
缓存失效端点                            生产构建实测 401/413/签名 200
Go test/vet                             platform-api 与 platform-worker 使用本地模块/构建缓存、GOPROXY=off 通过；审核领取仅 admin/owner、领取人绑定和重复注册验证码不发信回归测试通过
前端 standalone 运行态                 公开首页/登录页与后台登录页均可用；初始 HTML 含登录/注册、页脚免责声明、广告占位和默认图；API 未启动时 `/api/v1/me` 为 503/no-store 且游客入口仍保留
真实 PostgreSQL migration/seed          本轮未执行；迁移 13-16、来源/邮件/搜索约束和权限需在 PostgreSQL 16 独立空库 fresh/rollback 中复验
CI PostgreSQL roundtrip                 原脚本在隔离容器完整通过，含 runtime login 权限合同
CI 迁移重跑断言                         已从 schema v13 修正为当前 v16
CI Go race 门槛                         已加入 `CGO_ENABLED=1 go test -race`，本机 CGO=0 仅无法复跑
数据库合同                              角色隔离、公开番号搜索、审计追加写、媒体双桶、对账状态机通过
运行数据库账号                         API/Worker 独立非超级用户，真实权限拒绝、幂等配置、清理回滚通过
生产配置校验                           缺少数据库/邮箱/认证/Turnstile/图片基地址时拒绝启动；配置单元测试通过
生产数据库升级                         迁移 13-16 尚待发布窗口执行；现存生产库 v10→v16 需在维护窗口验证
开发测试目录                           30 人物/100 作品，双次 seed 幂等、搜索与 fresh rollback 通过
真实邮箱注册/重置/关闭                  自动 E2E 脚本与 Mailpit SMTP/API 解析通过；待 API 运行后执行完整业务流
运营审核/发布/搜索/权利下架             待 PostgreSQL 端到端
S3 物理上传/双桶删除/Cloudflare purge   本地 MinIO 双桶上传/HEAD/删除通过；真实 R2/Cloudflare 待验收
S3/数据库/北京副本每日对账              本地 4 对象/4 副本对账 0 问题；真实跨区待验收
备份复制与恢复                         真实 PostgreSQL 16：182711 字节备份、北京副本校验、独立空库恢复、审计 verified 通过
边缘/同源代理                         Nginx/前端代理合同 10 项、代理本地错误显式 no-store、两个同源登录 SSR 运行验证和日本 edge Compose 渲染通过；待 CI `nginx -t`
资源护栏                              日本全部服务 CPU/内存上限与日志轮转经 Compose 渲染核对
媒体容量护栏                          双桶当前对象与未完成 multipart 部件汇总、256 列表请求预算及超限批次上传前拒绝通过；真实 R2 列表权限和额度待验收
浏览器自动视觉检查                      本机 Chrome 自动生成 12 张标准截图与 SHA-256 manifest；公开首页/详情、后台登录/owner 概览/用户权限/审计查询桌面移动复核通过，目标环境仍待重跑
实体合并                               真实 PostgreSQL 验证关系迁移、软删除、小时统计合计、推荐移除、源隐藏、301、审计、outbox 和重复请求冲突
本机容器运行环境                        本轮按要求未启动 Docker；PostgreSQL、Mailpit、真实 SMTP/S3/Cloudflare 和完整 E2E 未验收
Go race test                            当前机器 CGO 未启用，`-race` 无法运行；需在启用 CGO 的 CI/服务器重试，非代码失败
```

## Release A 剩余门槛

2026-09-11 本机依赖探测：无 PostgreSQL 服务、无 `psql`，数据库合同连接变量和 `SMTP_ADDR` 均未配置；未打印任何值。在继续保持“不启动 Docker”的前提下，真实 SQL 与邮件 E2E 当前等待本机 PostgreSQL 16 或受控远程一次性测试库/Mailpit 环境，其余代码目标不因此缩小。

- 每日注册公开状态和固定默认图检查已实现；执行新增状态矩阵/并发调度 SQL 及目标默认图/权限验证，不把对象对账替代公开状态检查；
- 持久观察及管理员复核/账期切换需在专用 PostgreSQL 库验证并发单领取、中断留痕、高水位/复核、最小权限 API 仓储行锁、幂等与请求审计、Worker 前后快照/不可变回执同事务；真实操作/存储 Analytics、指标和用量/动态展示告警未联调；
- 主动默认图/停上传执行端、账户一次性操作/存储查询、持久操作量观察/可选定时器、管理员复核/账期切换、定时全量对账用量准入、逐操作上传暂停、API/页面动态默认图、Cloudflare 私有 R2 网关及授权上传队列/回执/后台页面已实现；后续对象存储任务清单已确认 Release A 没有第二个未接准入的非必要自动对象任务。仍需可信供应商账单/存储类别/版本历史/共享应用完整性，以及真实 bucket 私有化、Worker route/R2 binding/Cache API/purge 和传播验收，不能把 Analytics 建议、页面 CSP、离线网关、复核结果、手动开关或字节检查当账户费用上限；
- 验证北京共享状态卷 UID/持久性、Linux 进程互斥、完整两桶 ListObjects/ListMultipartUploads/ListParts 权限、真实 multipart 完成/中止竞态与实际 SDK 超时/中断恢复；人工确认命令本身不核对 S3；
- 主图切换 API/后台与 30 天保留排队已开发；执行三组替换、五组退出真实 SQL 合同，并验证 Worker 到期领取、权利提前、真实删除与容量回收，M5 仍不能标为整体验收完成；
- 在一次性 PostgreSQL 16 测试库执行 Schema v21 fresh/upgrade/down、上传控制 API/Worker 合同、22 组公开图片隔离矩阵和两组媒体登记合同；核对公开读者无新增底表权限、重复登记无重复 cache 事件；
- 在真实 Go API/PostgreSQL 和最终两站入口复验退出 503/重试/旧 token 失效、no-referrer 同源表单与禁止缓存；本地 mock 和 Go 分层回归已完成，不代替该端到端门槛；
- 密码进程级并发/等待护栏已实现；仍须在日本 API 384 MiB/0.40 CPU 限额下验证混合流量 RSS、CPU、超载/取消恢复，以及现有高参数摘要兼容性，不把本地单并发实测当作内存验收；
- 角色变更撤销旧 session 已实现；在受限 PostgreSQL 执行新增两组合同，验证多设备撤销、旧快照行锁等待、新登录角色期限及同角色重试不撤销；
- 在一次性 PostgreSQL 16 专用库用受限 API 登录执行账号生命周期合同，验证旧快照拒绝、并发改密行锁、同邮箱重注册验证码隔离和撤销时间约束；
- 使用 Mailpit 跑通注册、重置密码、关闭账号和旧 session 失效；
- 使用 Mailpit 跑通邀请创建、邀请邮件、48 小时过期、错误验证码锁定、接受后角色和 session；
- 跑通人工录入、他人审核、发布、搜索、字段冲突裁决、隐藏和权利下架；
- 在真实发布链路验证 HMAC 缓存失效、公开站不可用重试和恢复后完成，并确认 Cloudflare 只缓存公开目录 HTML/版本化静态资源/已确认公开图片，账号、搜索、API 和后台均 bypass；
- 使用受限 R2/S3 与 Cloudflare 凭据执行一次真实处理、上传、后台发布、下架、物理删除和每日对账；
- 在真实 PostgreSQL 跑通 CSV 预检、提交、同键重放、同键异内容 409、整批约束冲突回滚和批次历史；厂牌维护、冲突处理、版本快照、字段差异、实体合并/301 和回滚入口也待 PostgreSQL 端到端；
- 在真实 PostgreSQL 执行历史归档并验证文件恢复与 manifest 对账；备份年龄、outbox 和媒体额度指标/规则已接入，磁盘、邮件和对象存储服务端额度仍待接入；
- 使用最终域名核对 Cloudflare、邮件、S3 和图片展示政策；
- 使用最终域名和真实目标数据重新执行桌面/移动视觉与性能验收；

Release B 才设计具体来源和采集脚本；Release C 才评估并接入广告联盟。当前不访问真实来源，也不启用任何广告脚本。
