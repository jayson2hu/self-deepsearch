# 幕鉴资料展示平台

`Release A` 是面向 18 岁以上用户的作品与人物公开资料索引测试站。网站只展示可追溯的文字和图片资料，不提供、存储或跳转到视频、音频、磁力、种子、网盘或下载资源。

产品边界见 [PRODUCT_PLAN.md](./PRODUCT_PLAN.md)，服务与部署边界见 [ARCHITECTURE.md](./ARCHITECTURE.md)，实施和验收项见 [IMPLEMENTATION_PLAN.md](./IMPLEMENTATION_PLAN.md)。自动采集属于 Release B，广告联盟属于 Release C，两者在当前版本均关闭。

## 当前能力

- `apps/display-web`：公开站、番号/标题/人物/厂牌搜索、最新发行、人物/作品/厂牌列表与详情、默认图、广告占位、账号功能、canonical、Open Graph、robots 和 sitemap；
- `apps/ops-web`：独立运营后台、人工作品录入、资料目录、版本差异/回滚、审核领取/显式改派/发布、编辑推荐排期、首页发现流配置、字段冲突裁决、反馈审核、图片 manifest、用户角色、用量复核、上传控制和权利下架；
- `services/platform-api`：Go API，包含邮箱验证码、Argon2id 登录、服务端 session、公开目录、审核发布、统计和审计事务；
- `services/platform-worker`：日本 Go Worker，租约处理发布 outbox，使用独立 HMAC 调用公开站失效缓存和北京媒体删除服务；`media_delete` 只有在远端确认 S3 与北京副本均不可访问后才完成；
- `workers/collector-python`：只处理 fixture 的采集合约骨架，不访问真实来源；
- `workers/media-python`：受控人工图片处理、WebP 母版/三档派生、S3/R2 上传验证、两桶当前对象与未完成 multipart 部件容量准入（默认 10 GiB、单次扫描最多 256 个列表请求）、共享上传锁/中断保护、北京副本及签名删除服务；不下载来源 URL；
- `db`：日本单 PostgreSQL，使用 `collector`、`platform`、`audit` 三个 Schema 和 21 个迁移；第 13 个迁移加入管理员邀请，第 14-16 个迁移分别加入来源证据、邮件守卫和搜索小时聚合，第 17 个迁移收紧图片所属资料的公开发布边界，第 18 个迁移保存每日图片检查证据，第 19 个迁移保存账户用量观察状态与审计，第 20 个迁移加入用量复核与账期切换回执，第 21 个迁移加入上传控制队列与可信执行回执。数据库引擎仍为 PostgreSQL 16。
- `docker-bake.hcl`：统一构建 API、Worker、公开站、运营后台和媒体服务 5 个镜像；镜像写入 OCI 版本/源码 revision 标签，CI 构建后保留 image inspect 清单但不自动推送。

当前进度和未验收项见 [docs/PROGRESS.md](./docs/PROGRESS.md)，本地联调和运营流程见 [docs/RELEASE_A_RUNBOOK.md](./docs/RELEASE_A_RUNBOOK.md)。

2026-09-12 Ubuntu 验收已实际启动隔离 Docker PostgreSQL 16 与 Mailpit，迁移/权限、API 与 Worker SQL 合同、账号邮件及异人审核发布闭环、本地备份恢复均通过。此次修复了真实 SQL 参数类型、种子守卫、Linux 兼容性及独立镜像构建问题；完整结果与外部剩余门槛见[Ubuntu 验收记录](./docs/evidence/release-a-ubuntu-acceptance-2026-09-12.md)。下方较早日期证据保留其当时范围，公网结论仍以最新就绪审计为准。

新增[上传前用量准入与自动暂停](./docs/MEDIA_UPLOAD_ADMISSION.md)：北京每次 Listing/PUT 前通过私有签名接口读取日本现有用量状态；拒绝或不可用时阻断操作并持久记录系统暂停，旧恢复快照失效。默认 off，不新增服务/迁移，不冒充管理员、不清 pending、不自动恢复。此保护由下一次操作触发，不能撤销在途 SDK 请求，也不是计费保证；[本轮证据](./docs/evidence/release-a-media-upload-admission-2026-09-11.md)仅覆盖离线合同和本机 HTTP。日本 API/页面已另接默认 off 的动态默认图执行，Cloudflare 私有 R2 网关代码已补已知 URL/旧对象缓存入口；存储账单口径和网关真实部署验收仍未完成。

[后台授权上传控制队列](./docs/MEDIA_UPLOAD_QUEUE.md)：admin/owner 近期认证后提交五分钟请求，日本 Go Worker 保存派发记录，再调用[北京执行端](./docs/MEDIA_UPLOAD_CONTROL.md)；回执丢失保持待确认，后续签名状态可确认且不重复执行。v21 增加两张表，复用现有服务/连接池，默认 off；不清 pending，不由用量复核自动恢复。后台 `/media/upload-control` 已接入状态、独立暂停/恢复确认和回执历史；原请求重试不换键，状态缺失或过期时禁止新操作。[页面证据](./docs/evidence/release-a-media-upload-ui-2026-09-11.md)与[队列证据](./docs/evidence/release-a-media-upload-queue-2026-09-11.md)保留历史验证范围，不替代真实环境验收。
图片容量检查已覆盖共享锁、两桶全部当前对象、未完成 multipart 部件、未知写入及 manifest 写入失败后的人工核对；一次扫描共享 256 个列表请求预算，异常或预算耗尽 fail-closed。需要升级北京共享状态卷与只读 multipart 权限，见[上传容量与恢复](./docs/MEDIA_UPLOAD_CAPACITY.md)。[主动默认图执行端](./docs/MEDIA_DELIVERY_MODE.md)现同时支持最高优先级人工 `default_only` 与可选动态 `enforce`：Go API 按请求读取粘性用量状态去图，Next Proxy 对每个文档请求读取私有策略，以 CSP 立即阻断旧 ISR HTML 的远程图片；任一依赖错误 fail-closed。新增默认关闭的[Cloudflare 图片边缘网关](./docs/MEDIA_EDGE_GATEWAY.md)，以私有 R2 binding 在对象缓存之前执行同一策略，已知媒体 URL、旧边缘对象和缺失源对象都会受控或回退默认图，不新增日本常驻服务。Go Worker 的[一次性用量查询](./docs/MEDIA_USAGE_OBSERVATION.md)现同时报告账户操作量和全部已观测 bucket 的 UTC 日峰值/GB-month 整数估算；只有显式确认全账户 Standard 类别后才比较 10 GB-month 免费额度。持久观察器仍只保存操作量，高水位和复核证据默认 off，未增加 Schema；[人工复核与账期切换](./docs/MEDIA_USAGE_REVIEWS.md)不会被较低新样本自动清除。Analytics 不是账单，供应商版本历史、未返回 bucket/共享应用、真实类别/水位、网关 R2 私有化和 purge 仍待验证，不能据此承诺零费用。
图片模块尚未整体验收：已实现主图查询和并发安全替换 API、后台独立确认、共享旧资产保留、非共享旧公开图删除排队与私有母版 30 天保留，接入已有逐对象删除和权利下架提前机制。新登记接口仍只新增，不隐式覆盖。作品固定 1 张位置 0 主图和最多 3 张位置 1-3 的精选图，位置不可重复；精选图要求先有主图，公开读取不把只有精选图的数据提升为封面。当前 Schema 为 v21；本机 PostgreSQL 合同已通过；真实双 bucket、北京副本和缓存完整生命周期仍待目标环境验证，不能当作已上线。见[主图替换证据](./docs/evidence/release-a-media-primary-2026-09-10.md)和[展示槽位证据](./docs/evidence/release-a-media-display-slots-2026-09-11.md)。
当前 go/no-go 证据矩阵见 [docs/RELEASE_A_READINESS.md](./docs/RELEASE_A_READINESS.md)。
9 月 11 日统一预检、production build、性能/HTTP P95 和 desktop/mobile 78 项浏览器回归保留为[历史证据](./docs/evidence/release-a-local-regression-2026-09-11.md)。最新 [Ubuntu 前端验收](./docs/evidence/release-a-frontend-ubuntu-2026-09-12.json)已通过 82/82 浏览器、12 张视觉截图、两站构建/HTTP 与性能门槛，生产及全量 npm 依赖均 0 漏洞；本轮修复了 Ubuntu 桌面广告文字重叠及 E2E 覆盖历史截图的问题。这些前端证据使用内存合成 API，不替代真实 PostgreSQL、邮件、R2/Cloudflare 或目标服务器。逐项需求状态见[Release A 需求与验收矩阵](./docs/RELEASE_A_REQUIREMENTS_MATRIX.md)。
图片对账已补完整响应确认、样本体积控制和拿锁后调度快照，实际 Go→Python 对账合同已有本地证据；每日注册公开状态与两张默认图 HTTP 内容检查、独立审计及后台告警已接入；本机真实 SQL 合同已通过，bucket 权限和目标部署仍待验收。见[对账验证](./docs/evidence/release-a-reconciliation-2026-09-10.md)。
Release A 威胁模型、风险登记和安全放行条件见 [docs/RELEASE_A_SECURITY_REVIEW.md](./docs/RELEASE_A_SECURITY_REVIEW.md)。
定时全量图片对账已接入[用量准入](./docs/MEDIA_TASK_ADMISSION.md)：独立开关默认 off；显式启用后在高风险、缺失/过期/复核状态下不发起跨区扫描，权利删除继续运行。不控制上传、页面图片或直连 URL，也不增加服务、数据库表或 Analytics 请求。
[对象存储任务策略](./docs/MEDIA_STORAGE_TASK_POLICY.md)已把 Release A 的上传、定时全量对账、权利删除和公开读取四类数据面入口登记为机器可检查清单；当前唯一非必要自动对象任务是全量对账。以后新增直接 SDK 客户端或自动任务必须先登记分类与准入，不能绕过额度边界。

三 Schema、62 张表、5 个公开视图和核心关系见 [docs/DATA_MODEL.md](./docs/DATA_MODEL.md)。
开始真实联调前的外部准备项见 [docs/RELEASE_A_PREP_CHECKLIST.md](./docs/RELEASE_A_PREP_CHECKLIST.md)。
真实发布时复制并填写 [docs/RELEASE_A_GO_LIVE_RECORD.md](./docs/RELEASE_A_GO_LIVE_RECORD.md)，记录范围冻结、负责人、发布窗口、证据、回滚决策和稳定期结果。

每日检查配置、规则与升级说明见 [MEDIA_INSPECTION.md](./docs/MEDIA_INSPECTION.md)，本轮验证见[每日检查证据](./docs/evidence/release-a-media-inspection-2026-09-11.md)。

## 本地启动

要求：Node.js 22、Go 1.22+、Python 3.12、Docker Desktop / Docker Compose。

```powershell
Copy-Item .env.example .env
npm ci
docker compose up -d --build
```

默认地址：公开站 `http://127.0.0.1:3000`，运营后台 `http://127.0.0.1:3001`，API `http://127.0.0.1:8080`，Mailpit `http://127.0.0.1:8025`。

首次启动后需要按运行手册创建 owner，才能进入运营后台完成审核和发布。

只验证前端时可以不启动 Docker。`npm run build` 会生成两个 standalone 产物，随后分别运行：

```powershell
npm run start --workspace @self-deepsearch/display-web
npm run start --workspace @self-deepsearch/ops-web
```

两个 `start` 命令会在启动 server 前自动把当前构建的 `.next/static` 和可用的 `public` 文件复制到 standalone 目录，避免本地预览出现 HTML 可访问但 CSS、图标或默认图无法加载的问题。

两站默认使用 3000/3001。未启动 PostgreSQL、Go API、Mailpit 和对象存储时，这只证明前端构建及运行壳可用，不代表注册、审核发布、图片或邮件链路已经端到端通过。

Release A 前端 HTTP 冒烟可以完全脱离 Docker 运行。脚本会在随机端口启动两套已构建的 standalone server 和一个只返回合成资料的本地 mock API，检查公开站首页、登录/注册入口、页脚免责声明和 18+ 标识、广告占位与默认图、运营后台登录页、作品/人物/厂牌三类 JSON-LD，以及 API 上游不可用时的 `503`/`no-store` 代理响应；结束时会清理启动的进程。首次运行前先构建：

```powershell
npm run build
npm run test:frontend:smoke
npm run smoke:frontend:release-a
npm run test:e2e:release-a
```

另可运行 `npm run test:cache:release-a`，用真实 Go Worker 签名处理器调用 Next 的内部刷新接口，验证首页、详情和 sitemap 的内容变化、仅新增图片时的缓存刷新、错误/过期签名拒绝及下架。需要 Go 与已构建前端；不启动 Docker，目录 API 仍为合成替身。脚本使用独立临时 standalone 副本，不污染预览缓存，证据写入 `docs/evidence/release-a-cache-contract-local.json`。这项合同已纳入统一预检及 CI web 的必过门槛。

也可以让脚本自动先执行构建：`npm run smoke:frontend:release-a:build`。mock API 只服务合成资料，不访问真实外部服务；结构化数据用包含 `</script>` 攻击片段的标题验证安全转义。Playwright 使用有状态 mock，在真实浏览器覆盖番号搜索、账号、偏好、人工审核发布、邀请与用户权限、按 Request ID 查询最小披露审计时间线、反馈三角色流转、CSV 错误文件安全下载，以及权利请求登记→editor 只读→admin 近期密码执行→公开搜索撤下；目前共 41 个场景，在桌面与移动视口执行 82 项，包含公开账号页与后台 CSP nonce、用量复核、上传控制的独立确认、待处理/有效回执、响应丢失同键重试、旧状态过期与角色隔离。该证据不声明 S3/R2 物理删除已完成。本地默认使用已安装 Chrome，CI 安装 Chromium，并在 E2E 前强制生成 12 张视觉截图、执行浏览器 LCP/CLS 与首页/搜索 HTTP P95 门槛，上传 14 天证据包；测试不会启动或写入 PostgreSQL、Mailpit、对象存储，也不会替代真实业务联调。

日常开发可直接在仓库根目录分别运行 `npm run dev:display` 和 `npm run dev:ops`，然后使用 `http://127.0.0.1:3000/`（公开站）与 `http://127.0.0.1:3001/login`（运营后台）。不要直接双击 `index.html` 或 `work-detail.html` 使用 `file://` 联调；这会绕过 Next.js 的同源 API 代理，页面数据和登录请求无法正常工作。根目录脚本使用默认端口即可，若要改端口请在对应应用目录执行 `npm run dev -- --port <端口>`。

## 质量检查

推荐使用统一预检（默认不启动 Docker）：

```powershell
$env:PYTHON='C:\path\to\python.exe' # Python 已在 PATH 时可省略
npm run preflight:release-a
```

默认预检同时检查仓库根目录与 `docs/` 的全部 Markdown 本地链接；也可单独执行 `npm run check:markdown-links`。需要把 Playwright 当前全部场景（41 场景、桌面/移动 82 项）一起纳入同一轮本地门槛时，使用 `npm run preflight:release-a -- --with-e2e`；加入本地预热 LCP/CLS 门槛时追加 `--with-performance`。需要额外验证三套 Compose 静态配置时再加 `--with-compose`；Compose 参数只渲染配置，不启动容器。运营后台及公开站登录、注册、重置、邀请、退出和账号中心 HTML 已使用请求级 CSP nonce；公开目录为兼容 Next 框架引导脚本仍使用独立 CSP 策略，该风险与页面是否运行时渲染分开评估，边界见[账号页面 CSP 验证](./docs/evidence/release-a-ops-csp-nonce-2026-09-11.md)和[运行时目录验证](./docs/evidence/release-a-runtime-catalog-2026-09-11.md)。

只想持续浏览合成数据站点时，可以启动不依赖 Docker 的本地预览：

```powershell
npm run preview:release-a:build
```

公开站默认位于 `http://127.0.0.1:4173/`，运营后台位于 `http://127.0.0.1:4174/login`。可按页面示例搜索 `TEST-001` 并浏览正常合成作品；`TEST-JSONLD` 只用于自动安全回归。命令启动后会在终端输出仅用于本机的 `.test` 合成账号和验证码；状态只存在内存中，退出即清空。已存在当前 production build 时可改用 `npm run preview:release-a`，端口冲突时使用 `-- --display-port 5173 --ops-port 5174 --api-port 5175`。该预览不启动 PostgreSQL、SMTP、S3/R2、自动采集或广告联盟，不能作为真实联调证据。

需要重建当前代码的标准视觉证据时运行：

```powershell
npm run capture:visual:release-a:build
```

该命令在随机回环端口启动同一套有状态 mock 与两站 standalone build，使用本机 Chrome 自动生成公开首页、`TEST-001` 作品详情、后台登录、owner 运营概览、用户权限和审计查询页的桌面/移动全页截图，并写入包含视口、文件字节数和 SHA-256 的 `manifest.json`。默认输出到 `docs/evidence/release-a-visual-<Asia/Shanghai 日期>`；已有当前构建时可使用 `npm run capture:visual:release-a`。输出目录只能位于 `docs/evidence` 的独立子目录，结束后浏览器、mock 和前端监听会自动关闭。它仍是内存合成数据证据，不替代 PostgreSQL、SMTP、S3/R2、Cloudflare 或最终域名验收。

需要执行产品定义的 LCP `< 2.5s`、移动端 CLS `< 0.1` 浏览器门槛时运行：

```powershell
npm run probe:performance:release-a:build
```

本地模式在公开首页和 `TEST-001` 详情的桌面/移动视口各预热一次，再分别采样 3 次，记录 LCP、CLS、FCP、TTFB、加载时间和资源字节到 `docs/evidence/release-a-performance-<日期>.json`。最新 [9 月 12 日本地浏览器性能证据](./docs/evidence/release-a-performance-2026-09-12.json) 已通过，四组 LCP 中位数为 148–332ms，移动 CLS 均为 0；使用无网络限速和内存合成 API，不代表最终域名已经验收。目标环境准备好后使用 `npm run probe:performance:release-a -- --display-url https://<公开站域名> --work-path /works/<已发布-slug> --output docs/evidence/release-a-performance-target.json` 重跑；远程入口只接受不含凭据、路径和查询的 HTTPS origin。

公开 API P95 `<300ms`、番号搜索 P95 `<500ms` 使用低并发顺序探针：

```powershell
npm run probe:http-latency:release-a
```

工具对首页 API 和搜索 API 各预热 3 次、顺序采样 20 次，验证 200、JSON 与 2 MiB 响应上限；最新 [9 月 12 日本地 HTTP 延迟证据](./docs/evidence/release-a-http-latency-2026-09-12.json)的首页 API/搜索 P95 为 29.79/9.10ms，均通过门槛。报告不保存番号原文或响应正文，只保留番号 SHA-256、响应字节和耗时。最终入口使用 `--display-url https://<公开站域名> --search-code <已发布番号>` 重跑。统一预检的 `--with-performance` 会依次执行浏览器 LCP/CLS 和 HTTP P95 两个门槛。

```powershell
go test ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
node scripts/run_python.mjs scripts/generate_openapi_types.py --check
npm run lint
npm run typecheck
npm run audit:production
npm run build
docker compose config --quiet
```

Python 和真实 PostgreSQL 的命令见运行手册。真实环境准备好后，`scripts/release_a_email_e2e.py` 验证注册/重置/关闭/邀请，`scripts/release_a_catalog_e2e.py` 验证作品录入/异人审核/发布/番号搜索/隐藏；两个工具都不会输出密码，运营账号密码应只从本机环境变量读取。`.env.example` 只保存变量名和本地开发值，生产数据库、邮件、Turnstile、S3、session 和 HMAC 密钥不得进入 Git。

CI 设有两个真实服务门槛：`core-e2e` 使用一次性 PostgreSQL 16 + Mailpit + Go API 验证账号及审核发布链；新增 `real-stack-e2e` 进一步启动真实 Worker 和两个生产前端，通过 Chromium 验证邀请、异人审核、发布/隐藏及 outbox 驱动的公开缓存失效。任一失败都会阻断五镜像交付，只上传脱敏 JSON。历史归档另有受限 Worker 账号的真实 PostgreSQL 合同和离线只读校验命令。专用空库及重跑要求见 [运行手册 7.1–7.3](docs/RELEASE_A_RUNBOOK.md#71-真实核心服务-ci-验收)；这些门槛不替代目标服务器、Turnstile、真实图片存储和 Cloudflare 验收。

复制生产模板并在服务器私有目录填好真实值后，先做只读环境检查再启动 Compose。`core` 检查日本核心服务，`full` 在持有两份私有配置的受控管理机上核对日本/北京共享媒体密钥、公开图片基地址、版本号、双 bucket 和 10 GiB 上限；`--check-files` 在日本服务器上额外检查 Origin Certificate、私钥和 metrics token 文件。检查器只输出变量名和问题，不输出变量值；不要为了检查而在日本、北京服务器之间复制整份私有 env。直接检查仓库 `.env.example` 会因为仍含占位符而按设计失败。

```powershell
npm run check:release-a-env -- --japan-env C:\secure\japan.env --mode core
npm run check:release-a-env -- --japan-env C:\secure\japan.env --beijing-env C:\secure\beijing.env --mode full
python3 scripts/release_a_env_check.py --japan-env /srv/self-deepsearch/.env --mode core --check-files
```

部署启动后使用只读探针核对公开首页、后台登录页、API health/ready、Worker ready 和可选 metrics。探针不创建账号、不写浏览记录、不触发发布；要求 API/Worker 报告同一 `BUILD_VERSION`，并检查安全头、缓存边界、页脚声明和“广告仅占位”规则。metrics token 与可选 Cloudflare Access Client ID/Secret 只从环境变量读取，HTTP 客户端禁止跨域自动重定向，报告不包含这些凭据。

```powershell
$env:METRICS_TOKEN='<仅写入当前安全终端环境>'
npm run probe:release-a -- `
  --display-url https://display.example.com `
  --ops-url https://ops.example.com `
  --api-url https://api.example.com `
  --worker-url http://127.0.0.1:8081 `
  --metrics-url http://127.0.0.1:8081 `
  --expected-version 2026.09.09-a1 `
  --output runtime/release-a-probe.json
```

在全新 Python 3.12 环境中先安装 Release A 测试依赖，再执行运行手册中的离线测试：

```powershell
python -m pip install -r requirements-test.txt
ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
```

修改 `packages/api-contracts/openapi.yaml` 后运行 `node scripts/run_python.mjs scripts/generate_openapi_types.py` 更新全部组件类型。`packages/api-contracts/src/generated.ts` 禁止手工编辑；统一预检和 CI 会在 TypeScript 编译前执行 `--check`，发现漂移立即失败。

生产环境的 API 会在启动时拒绝缺少 PostgreSQL、认证 HMAC、SMTP、Turnstile、允许来源或公开图片基地址的配置；Turnstile 还要求 `TURNSTILE_EXPECTED_HOSTNAME` 为与公开 `SITE_URL` 一致的纯 hostname，注册与密码重置分别绑定固定 action，见[Turnstile 绑定证据](./docs/evidence/release-a-turnstile-binding-2026-09-11.md)。公开站的 canonical、robots 和 sitemap 只信任运行时私密 `SITE_URL`；生产缺失、非法、带路径或非本机 HTTP 时会 fail-closed，不退回 `NEXT_PUBLIC_SITE_URL` 或回环默认值。请先通过环境检查、完成迁移和运行账号配置，再启动 API/Worker。开发环境仍可在依赖未就绪时启动页面壳用于前端检查。

需要构建同一批 Release A 镜像时使用 Bake，并保证 `BUILD_VERSION` 与日本、北京私有 env 中的版本一致。`--load` 只加载到当前 Docker 主机；配置并登录私有 registry 后才能改用 `--push`。仓库 `.dockerignore` 会拒绝把 env、证书私钥、metrics token、缓存和运行数据发送给 Docker daemon。

```powershell
$env:BUILD_VERSION='2026.09.09-a1'
$env:BUILD_REVISION='<git-commit-sha>'
$env:IMAGE_PREFIX='registry.example.com/self-deepsearch'
docker buildx bake release-a --load
```
