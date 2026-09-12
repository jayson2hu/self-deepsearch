# Release A 本地联调手册

当前整体 Schema 为 v21。每日图片检查见 [MEDIA_INSPECTION.md](./MEDIA_INSPECTION.md)；迁移 19 的账户用量持久观察/可选定时器、私有告警及升级见 [MEDIA_USAGE_STATE.md](./MEDIA_USAGE_STATE.md)。迁移 20 新增[用量复核与账期切换](./MEDIA_USAGE_REVIEWS.md)，需配套 API/Worker/运营站。观察器和[动态默认图](./MEDIA_DELIVERY_MODE.md)均默认 off；启用动态执行后 API/Next 按请求消费粘性状态，错误 fail-closed，复核仍是恢复保护的必要条件。本轮未启动 Docker 或真实外部服务。

## 1. 启动

新增[后台上传控制队列手册](./MEDIA_UPLOAD_QUEUE.md)：Schema v21 增加请求/状态表，API 提交、现有日本 Worker 带租约派发、北京持久执行并返回可信回执；没有新增服务，执行器默认 off；前端操作页面现已接入 `/media/upload-control`。先核对备份并验证受限 SQL 合同、升级全部北京上传器的限时协议，再配套迁移日本 API/Worker/监控并发布运营后台制品。两地控制密钥只放私有环境；HTTP 需显式允许及 VPN/TLS。北京 enforce、缺状态先暂停初始化，日本 dispatch；恢复另需新鲜用量准入。手动 CLI 仍见[执行端手册](./MEDIA_UPLOAD_CONTROL.md)。均不清 pending、不由普通新样本自动恢复；逐操作自动暂停另见[准入手册](./MEDIA_UPLOAD_ADMISSION.md)，日本 API/Next 动态默认图见[停图手册](./MEDIA_DELIVERY_MODE.md)，图片全入口见[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)，全部默认 off。网关代码已完成，真实 bucket/route/binding/purge 未部署验收。

开始前先按 [Release A 联调准备清单](./RELEASE_A_PREP_CHECKLIST.md) 准备 PostgreSQL、邮件服务、owner 和首批测试资料。密码、密钥和 HMAC secret 只写入服务器私有 `.env`，不要放进仓库或聊天记录。

使用日本生产模板时，启动前先运行只读环境检查。第一阶段只检查日本核心服务；北京媒体配置准备好后，在持有两份私有配置的受控管理机上运行 full，不要为了检查而在日本、北京服务器之间复制整份私有 env。`--check-files` 单独在日本服务器上检查 Origin Certificate、私钥、metrics token、Alertmanager 配置和 SMTP 密码文件；它会拒绝 Alertmanager 占位收件人、内联 SMTP 密码、缺少严重度路由或过短密码文件。检查器不会输出配置值，仓库 `.env.example` 因包含故意保留的占位符不能作为通过样例。

```powershell
npm run check:release-a-env -- --japan-env C:\secure\japan.env --mode core
npm run check:release-a-env -- --japan-env C:\secure\japan.env --beijing-env C:\secure\beijing.env --mode full
python3 scripts/release_a_env_check.py --japan-env /srv/self-deepsearch/.env --mode core --check-files
```

生产镜像使用仓库根目录 Bake 清单统一构建。版本号必须与两地私有 env 的 `BUILD_VERSION` 和五个镜像标签一致；源码 revision 使用准备发布的完整 Git commit SHA。CI 会构建并保留 image inspect 清单，但不自动推送。只有部署账号已登录私有 registry 时才使用 `--push`，否则先用 `--load` 做构建验证。

```sh
BUILD_VERSION='2026.09.09-a1' \
BUILD_REVISION='<git-commit-sha>' \
IMAGE_PREFIX='registry.example.com/self-deepsearch' \
docker buildx bake release-a --load
```

构建上下文受 `.dockerignore` 保护，env、证书/私钥、metrics token、缓存和运行数据不得进入镜像上下文。部署前保留上一批私有 env 副本和 image inspect 清单。若新版本尚未执行不兼容数据库变更，可恢复上一批五个不可变镜像标签、重新通过 env 检查后执行 `docker compose up -d`；数据库仍优先 roll-forward，不通过镜像回滚掩盖 Schema 不兼容。

```powershell
Copy-Item .env.example .env
npm ci
docker compose up -d --build
docker compose ps
```

服务启动后先执行只读部署探针。公开站、后台和 API 使用最终入口 URL；Worker 与 metrics 保持内网地址。`--metrics-url` 可省略，启用时令牌默认从 `METRICS_TOKEN` 读取。若后台已接 Cloudflare Access，可在当前安全终端设置 `CF_ACCESS_CLIENT_ID` 和 `CF_ACCESS_CLIENT_SECRET`；探针只把它们发送给后台登录入口，且禁止自动跟随重定向，报告不会包含凭据。输出文件可作为该版本的部署证据。

```sh
METRICS_TOKEN='<从服务器私有配置读取，不写入命令历史>' \
python3 scripts/release_a_deployment_probe.py \
  --display-url 'https://display.example.com' \
  --ops-url 'https://ops.example.com' \
  --api-url 'https://api.example.com' \
  --worker-url 'http://127.0.0.1:8081' \
  --metrics-url 'http://127.0.0.1:8081' \
  --expected-version '2026.09.09-a1' \
  --output '/srv/self-deepsearch/evidence/release-a-probe.json'
```

探针只发送 GET，核对 API/Worker 版本、Schema ready、公开首页缓存、安全头、18+ 与资料边界、广告仍为静态占位、后台 `no-store`，以及可选版本指标。任何检查失败都保持 no-go，先修复后重复执行；不要用跳过检查或修改证据文件代替修复。

检查：API `/healthz` 为 200，`/readyz` 为 200；公开站为 3000，后台为 3001，Mailpit Web UI 为 8025、本机 SMTP 为 1025。两个 Mailpit 端口都只绑定 `127.0.0.1`。开发环境 `TURNSTILE_BYPASS=true`，生产必须关闭，并同时配置公开的 `TURNSTILE_SITE_KEY`、私密的 `TURNSTILE_SECRET_KEY` 和只含最终公开域名 hostname 的 `TURNSTILE_EXPECTED_HOSTNAME`；环境检查要求后者与 `SITE_URL` 一致。公开站 canonical/robots/sitemap 只读取运行时 `SITE_URL`；生产缺失、非法、带路径或公网 HTTP 时会返回错误，不退回构建期 `NEXT_PUBLIC_SITE_URL` 或回环默认值。站点密钥由公开站在运行时读取，不使用构建期 `NEXT_PUBLIC_` 变量；注册和密码重置分别绑定 `signup_code` 与 `password_code`，API 会精确校验 Siteverify 返回的 hostname/action。缺少生产站点密钥时两个表单会明确停用。未启动的依赖不能标记为联调通过。

镜像健康检查与人工验收含义不同：Go API/Worker 的容器健康检查访问本机 `/readyz`；API 拒绝数据库不可用或 Schema 不是 v21，Worker 检查数据库连通与调度心跳；公开站、后台和媒体服务分别探测首页、登录页和 `/health/live`。Compose 会让 API 和日本 Worker 等待 PostgreSQL healthy，让两个前端等待 API healthy，日本 edge 再等待 API 和两个前端 healthy；启用 monitoring profile 时，Prometheus 还会等待 API 与 Worker healthy。容器显示 healthy 只证明基本依赖和 HTTP 入口就绪，邮箱发送、审核发布、S3/R2 与 Cloudflare 仍必须按本手册执行真实业务验收。

只验证前端构建和 standalone 运行态时不需要 Docker。先构建，再在两个终端分别启动；公开站与后台会尝试访问 `PLATFORM_API_URL`，API 未启动时只能验证页面壳、登录页和显式不可用状态，不能据此宣称业务端到端通过：

```powershell
npm run typecheck
npm run lint
npm run build
npm run start --workspace @self-deepsearch/display-web
npm run start --workspace @self-deepsearch/ops-web
```

两个 `start` 脚本会先把当前构建的 `.next/static` 和可用的 `public` 文件复制到对应 standalone 应用目录，再加载生成的 server，默认分别监听 3000 和 3001；它们不使用 `next start`。这样直接启动也不会出现 HTML 正常但 CSS、图标或默认图 404。测试结束后停止两个进程。若只做日常开发，可改用根目录的 `npm run dev:display` 与 `npm run dev:ops`。

无需 Docker 的自动运行态复核使用以下命令。脚本会启动两个随机端口的 standalone server 和只返回合成资料的本地 mock API，验证三类详情 JSON-LD、恶意 `</script>` 标题转义、安全响应头、后台两次响应 nonce 轮换、可执行脚本 nonce、CSS/默认图及代理 `503/no-store`；不会写数据库或对象存储：

```powershell
npm run test:frontend:smoke
npm run smoke:frontend:release-a
npm run test:e2e:release-a
```

Playwright 命令要求先存在当前 production build。本地默认使用已安装 Chrome；CI 会安装固定版本 Chromium。浏览器套件使用有状态 mock API，覆盖匿名番号搜索与详情、账号验证码和个人功能、人工录入审核发布、邀请与用户权限、admin 按 Request ID 查询最小披露审计时间线、反馈 editor/admin 分权、CSV 错误文件安全下载、权利下架、图片与用量/上传保护，以及公开账号页和后台请求级 CSP nonce；当前共 41 个场景，在桌面与移动视口执行 82 项，不发送真实邮件，也不把 mock 状态变更视为 PostgreSQL 权限或 S3/R2 物理删除证据。

需要人工浏览当前构建时运行：

```powershell
npm run preview:release-a:build
```

该命令只监听回环地址，默认公开站 `127.0.0.1:4173`、运营后台 `127.0.0.1:4174`、内部 mock API `127.0.0.1:4175`，并在终端输出合成 `.test` 账号与统一测试验证码。所有注册、收藏、审核、邀请、反馈和下架状态只保存在当前进程内，停止后丢弃；它不会连接真实 PostgreSQL、邮件或对象存储。已有 production build 时可省略 `:build`，端口可分别用 `--display-port`、`--ops-port`、`--api-port` 覆盖。按 Ctrl+C 停止后，脚本会关闭三个监听并清理本次临时补齐的 standalone 静态资源。

需要把当前 production build 的视觉状态固化为可复核文件时运行：

```powershell
npm run capture:visual:release-a:build
```

工具使用随机 `127.0.0.1` 端口和本机 Chrome，自动捕获公开首页、正常合成作品 `TEST-001` 详情、后台登录、owner 登录后的运营概览、用户权限和审计查询页，共 6 个页面 × 桌面/移动 2 个视口。每张截图完成前会等待目标标题、字体和图片，拒绝页面级横向溢出、可见坏图与浏览器脚本异常；全部成功后才复制到 `docs/evidence/release-a-visual-<日期>` 并写入带 SHA-256 的 `manifest.json`。可用 `--output-dir docs/evidence/<独立目录>` 指定目录，不能写到证据目录之外。已有当前构建时使用 `npm run capture:visual:release-a`；需要使用已安装的 Playwright Chromium 时追加 `-- --browser-channel chromium`。这项检查不启动或证明真实数据库、邮件、对象存储和 Cloudflare。

本地浏览器性能基线使用：

```powershell
npm run probe:performance:release-a:build
```

探针对公开首页和 `TEST-001` 详情分别执行桌面 1366×900、移动 390×844 测量，每组先预热一次再采 3 次。LCP 使用中位数且必须 `< 2500ms`，移动端 CLS 使用最大值且必须 `< 0.1`；原始样本、FCP、TTFB、加载时间、资源数量/字节、阈值与合成边界写入 `docs/evidence/release-a-performance-<日期>.json`。已有构建时省略 `:build`，也可在统一预检追加 `--with-performance`。该本地结果没有网络限速、不是 RUM 数据，也没有经过 Cloudflare，不能替代最终入口验收。

最终公开站准备好后执行：

```powershell
npm run probe:performance:release-a -- --display-url https://display.example.com --work-path /works/ABC-123-00000001 --output docs/evidence/release-a-performance-target.json
```

远程 origin 必须是 HTTPS 且不能包含路径、查询、片段或凭据，作品路径必须是站内 `/works/<slug>`。探针会在同一浏览器上下文先预热再采样，但仍属于实验室数据；公网放行时还要结合 API/搜索 P95、服务器资源、Cloudflare 缓存状态和真实用户指标。

公开 API 与番号搜索延迟门槛使用：

```powershell
npm run probe:http-latency:release-a
```

本地模式通过公开站同源代理请求 `/api/v1/site/home` 和 `/api/v1/search/works`，每个接口预热 3 次后顺序采样 20 次，避免性能验收本身制造并发压力。完整响应 P95 必须分别 `<300ms`、`<500ms`，同时要求 HTTP 200、`application/json`、有效 JSON 和不超过 2 MiB。报告只记录耗时、响应字节和番号 SHA-256，不保存番号原文或响应正文，默认写入 `docs/evidence/release-a-http-latency-<日期>.json`。

最终入口准备好后执行：

```powershell
npm run probe:http-latency:release-a -- --display-url https://display.example.com --search-code ABC-123 --output docs/evidence/release-a-http-latency-target.json
```

远程模式同样只接受 HTTPS origin；它仍是顺序实验室采样，不能替代 Go/Prometheus 路由直方图、真实并发压测或用户侧指标。统一预检的 `--with-performance` 会同时运行浏览器 LCP/CLS 探针和本 HTTP P95 探针。

需要查看监控时使用 profile 启动 Prometheus 和 Alertmanager：

```powershell
docker compose --profile monitoring up -d --build
```

Prometheus 只监听 `127.0.0.1:9090`，保留 7 天且最多使用 512 MiB；Alertmanager 只监听 `127.0.0.1:9093`。本地 Alertmanager 使用无外发的 `alertmanager.dev.yml`，避免开发告警误发。生产使用服务器私有的 Alertmanager 配置与独立 SMTP 密码文件：从 `infra/monitoring/alertmanager.example.yml` 复制后替换 SMTP 主机、发件人与两个收件人，密码只写入 `ALERTMANAGER_SMTP_PASSWORD_FILE`。Compose 固定 Alertmanager 为 UID/GID `65534:65534`；服务器可将 secrets 目录设为 `root:65534`/`0750`、两份文件设为 `root:65534`/`0640`，或采用等价的只允许部署人员与该容器用户读取的 ACL，不能使用 world-readable。开发 metrics 令牌来自 `infra/monitoring/dev-metrics-token.txt`；生产使用至少 32 字节随机值，并保证 `METRICS_TOKEN_FILE` 内容与 API、日本 Worker 的 `METRICS_TOKEN` 完全一致。任一私有文件未准备好时不要启动生产 monitoring profile。

API 的 `self_deepsearch_http_request_duration_seconds` 是固定桶 histogram，可直接用 `histogram_quantile` 计算服务端路由 P95。`PlatformPublicAPIP95LatencyHigh` 只统计公开目录的成功 GET/HEAD：10 分钟内至少 20 次请求、P95 `>=300ms` 且该状态持续 10 分钟才发送 warning；`PlatformSearchP95LatencyHigh` 只统计规范路由 `/api/v1/search/works`：10 分钟内至少 10 次成功请求、P95 `>=500ms` 且持续 10 分钟才发送 warning。低于最小请求量时告警保持静默，不代表延迟已通过，仍需执行前述 HTTP 探针。指标 route 是模板值，未知方法统一为 `OTHER`，不得改成真实 slug、番号、查询词、邮箱或用户标识。

API 同时导出 PostgreSQL 连接池 acquired/idle/total/max、利用率和 acquire 计数。`PlatformDatabasePoolUtilizationHigh` 在 acquired/max `>=80%` 持续 10 分钟时发送 warning；先结合 API P95、5xx、`empty_acquire_total`/`canceled_acquire_total` 增速和 PostgreSQL 慢语句日志判断是慢 SQL、事务占用还是并发过高。当前 API 池上限为 10，不要只为消除告警直接增大连接数；日本 2C4G 应优先修复慢查询、缩短事务或降低 Worker 并发，并复核 PostgreSQL `max_connections` 预算。

日本 Worker 每轮 outbox poll 都更新心跳，并导出最后成功 poll 的时间差、累计 poll 失败、连续 poll 失败、当前 claimed batch 数和事件成功/失败累计值。连续 3 次 poll 返回错误时 `/readyz` 变为 503；该状态持续 2 分钟后 `PlatformWorkerPollFailures` 发送 critical。一次成功 poll 会清零连续失败计数。心跳正常但连续失败通常表示调度循环仍在运行、Claim/Complete/Fail 链路却无法完成；应先检查 PostgreSQL、运行账号权限、statement/lock timeout 和 Worker 错误日志，不要通过重启掩盖持续故障。指标不包含事件 ID、载荷、番号或用户标识。

同一 profile 中的 `node-exporter` 只启用 CPU、内存与文件系统 collector，端口仅绑定 `127.0.0.1:9100`。它以非 root 只读读取宿主 `/proc`、`/sys` 和根文件系统；不要给它增加写挂载、特权模式或额外 capabilities。`PlatformNodeExporterDown` 表示主机容量监控失明；根分区使用率达到 80% 持续 15 分钟为 warning，达到 90% 持续 5 分钟为 critical；内存使用率达到 80% 或 CPU 使用率达到 85% 持续 15 分钟为 warning。磁盘 warning 先清理可重建缓存、过期日志和已验证的超保留备份，critical 时停止非必要批处理；不得删除未验证备份、媒体 manifest、审计或数据库文件。

目标环境启用监控后，先在 Prometheus Targets 页面确认 API、Worker 与 node-exporter 全部为 up，再在 Rules 页面确认规则无加载错误；随后在隔离测试窗口让 Worker 连续 3 次 poll 失败并恢复，确认 `/readyz` 的 503 → 200、连续失败指标的 3 → 0，以及告警 pending → firing → resolved；再用受控测试流量分别超过 API 延迟规则的最小请求量并注入可恢复延迟，核对 Alertmanager 邮件。主机阈值优先使用临时告警规则或隔离测试机验证，不要在正式服务器故意填满磁盘或制造内存耗尽；测试结束后立即恢复配置。不得在正式用户请求上进行故障注入，也不能仅用单次慢请求或“没有告警”宣称监控已验收。

日本生产模板需要外部入口时启用 `edge` profile。先把 Cloudflare Origin Certificate 和私钥放到 `.env` 指定的服务器绝对路径，再执行：

这里的 `--env-file` 只向 Compose 提供变量插值，不会自动把整份 `.env` 注入每个容器。生产 Compose 禁止添加服务级 `env_file`；服务只能接收模板 `environment` 中显式声明的变量。部署前应运行 `infra/security/tests`，确认公开前端没有数据库、SMTP、S3 或 Turnstile 私钥，北京 Worker 没有数据库凭据。

```sh
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile edge config --quiet
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile edge up -d
```

源站防火墙必须只允许 Cloudflare 出口访问 80/443，Cloudflare SSL 模式使用 Full (strict)。公开目录只有成功的 GET/HEAD HTML 由 Nginx 默认缓存 5 分钟；其他方法、4xx/5xx、账号页、搜索、API 和后台返回 `no-store`。`/api/internal/`、`/metrics`、Worker、PostgreSQL 与 Prometheus 不暴露。正式开放 `OPS_HOST` 前必须配置 Cloudflare Access、VPN 或固定 IP 白名单。

浏览器账号与后台请求统一调用各自 Next.js Host 的同源 `/api/v1/*`；后台还可调用同源 `/admin/v1/*`。Next Route Handler 将受限请求头和最多 2 MiB 请求体转发到内部 `PLATFORM_API_URL`，并把 session Cookie 留在当前前端 Host。不要重新引入 `NEXT_PUBLIC_PLATFORM_API_URL`，也不要把 session Cookie 的 Domain 放宽到根域。Go API 在生产模式拒绝所有缺少 `Origin` 的写请求；前端代理会转发浏览器 Origin，服务端退出路由会补当前站点 Origin。

生产数据库首次安装和后续升级统一使用一次性工具服务，不能在 API 启动时自动迁移：

```sh
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile tools run --rm database-migrate
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile tools run --rm database-logins
```

迁移器使用 PostgreSQL advisory lock，要求 migration 从 1 连续编号；每个版本独立事务并验证 `platform.system_metadata.schema_version`。当前版本为 v21：迁移 13 创建 `platform.user_invitations`，迁移 14 绑定 revision 来源证据，迁移 15 增加邮件额度/熔断状态，迁移 16 增加搜索小时聚合，迁移 17 收紧公开图片父资料可见性，迁移 18 增加每日图片检查审计，迁移 19 增加账户用量持久状态和运行审计，迁移 20 增加不可变复核请求、账期切换回执和最小权限锁函数。只有两个命令均成功后才能启动新版本 API/Worker。数据库回滚优先 roll-forward；不要对有价值的数据直接执行测试用 Down 全回滚。

## 2. 创建第一个 owner

生产镜像已经包含独立的 `/create-owner` 一次性工具，服务器不需要安装 Go 或保留源码。密码为 12～128 位；该命令只允许创建首个 owner，相同邮箱重跑会轮换密码并撤销旧 session，已有不同邮箱的活动 owner 时拒绝执行。

```sh
export BOOTSTRAP_OWNER_EMAIL='owner@example.com'
export BOOTSTRAP_OWNER_PASSWORD='use-a-one-time-strong-password'
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile tools run --rm owner-bootstrap
unset BOOTSTRAP_OWNER_EMAIL BOOTSTRAP_OWNER_PASSWORD
```

两个一次性变量只在受控终端中设置，不写入 Git、聊天、工单或长期运行容器；设置期间不要执行会展开环境变量的 `docker compose config`、截屏或复制终端输出。`--rm` 会在完成后删除工具容器。需要在本地源码环境排障时仍可设置 `DATABASE_URL`、`BOOTSTRAP_OWNER_EMAIL`、`BOOTSTRAP_OWNER_PASSWORD` 后执行 `go run ./services/platform-api/cmd/create-owner`。命令只输出 owner UUID，不输出邮箱或密码。

API、owner 引导工具和日本 Worker 分别使用 `platform_api_login`、`platform_api_login` 和 `platform_worker_login`。PostgreSQL owner `platform` 连接只允许迁移、备份、恢复和下列运行账号配置步骤使用，不能放入 API/Worker 容器：

```sh
ADMIN_DATABASE_URL="$ADMIN_DATABASE_URL" \
PLATFORM_API_DB_PASSWORD="$PLATFORM_API_DB_PASSWORD" \
PLATFORM_WORKER_DB_PASSWORD="$PLATFORM_WORKER_DB_PASSWORD" \
sh infra/database/provision_runtime_logins.sh
```

## 3. 邮箱流程

打开公开站注册页，请求验证码后到 `http://127.0.0.1:8025` 查看邮件。分别验证注册、忘记密码、关闭账号和 `/invite` 接受邀请。邀请由后台 owner/admin 创建，验证码只在邮件中出现，owner 可邀请三种角色，admin 只能邀请普通用户；邀请接受后应创建对应角色并设置 session，重复接受、错误验证码达到 5 次、过期和撤销均应失败。关闭后旧 session 必须失效且账号不能再次登录。

API 和 Mailpit 就绪后可执行自动验收。脚本使用唯一测试邮箱，真实读取三封 Mailpit 邮件并验证注册、密码重置、旧 session/旧密码失效、重新登录、关闭账号和关闭后拒绝登录；输出不会包含密码或验证码：

```powershell
python scripts/release_a_email_e2e.py
```

如果要同时验证管理员邀请链路，额外提供 owner 和被邀请账号参数。已有账号密码和邀请账号的新密码只通过本机环境变量传入，不放在命令参数、进程列表、仓库或聊天记录中：

```powershell
$env:RELEASE_A_OWNER_PASSWORD='<owner-password>'
$env:RELEASE_A_INVITED_PASSWORD='<invited-password>'
python scripts/release_a_email_e2e.py `
  --owner-email owner@example.test `
  --invited-email invited-release-a@example.test
$env:RELEASE_A_OWNER_PASSWORD=$null
$env:RELEASE_A_INVITED_PASSWORD=$null
```

该模式会登录 owner、完成近期密码确认，再创建 48 小时有效的普通用户邀请、从 Mailpit 读取邀请验证码、接受邀请并确认新 session 的角色为 `user`。密码确认失败时不会创建邀请或发送邀请邮件。

若 Go API 直接运行在宿主机而不是 Compose 容器中，使用 `SMTP_URL=smtp://127.0.0.1:1025?from=no-reply@example.invalid&insecure=true`；容器内仍使用服务名 `mailpit:1025`。

### 账号与会话一致性补验

顺序邮箱 E2E 不能代替数据库并发合同。最新实现会在登录写 session 时锁定账号并核对已验证的邮箱、密码摘要、角色和状态；改密必须匹配重置挑战绑定的原用户 UUID，关闭后同邮箱新账号不能使用旧验证码。所有 session 撤销语句保护 `revoked_at >= created_at`，防止并发创建晚于请求开始而触发约束回滚。

下面的真实 SQL 合同只在一次性测试库准备好后执行。测试库必须是回环 `127.0.0.1`、显式端口、固定库名 `self_deepsearch_migrate_test`、Schema v21，并使用无超级用户/BYPASSRLS 权限的 `platform_api_login`；`PLATFORM_API_TEST_DATABASE_URL` 通过受控终端私有环境变量设置，不要放进聊天或命令参数。只允许无查询参数或 `sslmode=disable`；不得指向业务库。

```powershell
go test -count=1 -v ./services/platform-api/internal/database -run 'TestIdentityContractDatabaseGuard|TestPostgresIdentityLifecycleContract'
```

当前需验证 5 组结果：改密/关闭撤销旧会话且拒绝旧快照、角色/邮箱/状态变化拒绝签发、同邮箱重注册不接受旧验证码、实际账号行锁等待后拒绝旧密码快照，以及新增的幂等退出/首次撤销证据保护；同时覆盖请求开始早于 session 创建的撤销时间约束。缺少数据库变量会明确 SKIP，不能记为通过。现有 CI migrations 会在迁移/fixture 加载后用受限 API 登录执行整个 database 包，无需增加权限或另开 CI 作业。

合同使用随机合成账号，测试后保留合成记录和追加式审计，随一次性 CI 库结束处理；不要为了清理授予 DELETE、删除审计或停用触发器。2026-09-12 Ubuntu 受限账号真实 SQL 与核心闭环已通过，见[本轮后端证据](./evidence/release-a-backend-ubuntu-2026-09-12.json)；[账号生命周期历史证据](./evidence/release-a-identity-lifecycle-2026-09-10.md)保留其原执行范围。

账号快照加固本身没有 Schema/OpenAPI 迁移，部署需替换全部日本 API 实例及同镜像 owner 引导工具；旧 API 未退出前不能依赖新保证。后续退出确认改动涉及两站前端与 Nginx，按对应小节配套升级。密码计算护栏与角色变化后旧会话失效已实现，目标资源及实际 SQL 验证仍列在[安全审计](./RELEASE_A_SECURITY_REVIEW.md)；顺序邮箱测试成功不能覆盖这些门槛。

### 角色变更与旧会话失效

只有 owner 且完成近期密码确认才能改其他非 owner 账号的角色。真实变化会在同一事务更新角色、撤销该账号所有未撤销 session、写入审计；任何一步失败不得返回成功。撤销原因为 `role_change`，时间不早于 session 创建时间，原已撤销记录不覆盖。相同角色返回 409，无撤销和新角色审计；不要将 409 当作需反复重试的服务故障。

重新登录后，普通用户为 30 天会话，editor/admin 为 8 小时；旧 session 及其近期密码确认不能继续用于鉴权。运营后台已显示变更前说明与成功反馈，503 时不修改界面角色或误报撤销成功。说明针对提交后的请求，不代表自动撤回已通过鉴权的在途操作。

角色会话修复本身不增加迁移；当前整体 Schema 为 v21；先更新全部日本 API，再更新运营站，避免新界面配旧 API 误报保证。浏览器可能仍保留目标用户的旧 Cookie，但服务端必须拒绝旧 token；不能以删除 Cookie 或页面跳转作为撤销证据。

在隔离测试环境使用 owner 与目标测试账号完成以下验收：

1. 目标用户在两个独立会话登录；owner 将 user 改为 editor，旧会话访问 `/api/v1/me` 均应为 401，旧 session 搭配近期确认也不能执行后台变更；owner 会话仍可用。
2. 目标用户重新登录，服务端 session 期限为 8 小时；降级到 user 后再登录则为 30 天。原已撤销记录的时间/原因保持不变。
3. 重复提交当前角色返回 409，不踢刚建立的新会话、不重复写角色审计。模拟撤销写入失败时，角色更新和审计一同回滚，后台不显示成功。
4. 用受限一次性数据库执行下列两组 SQL 合同，核对登录与角色修改共用行锁，等待中的旧角色快照在提交后被拒绝；没有环境变量时 SKIP，不算通过。

```powershell
go test ./services/platform-api/internal/database -run '^TestPostgresRoleChange' -v -count=1
```

合同沿用前述受限 v21 测试库与随机合成夹具，保留追加式审计，不增加 DELETE 权限。本轮 Ubuntu 真实 SQL、HTTP 与桌面/移动回归均已通过，见[Ubuntu 验收](./evidence/release-a-ubuntu-acceptance-2026-09-12.md)；[角色会话历史验证](./evidence/release-a-role-sessions-2026-09-10.md)保留原日期。

### 密码计算超载与邀请预检

当前默认全进程 1 个计算、2 个等待，最多排队 500ms；这不是总请求耗时上限。已开始的 Argon2 无法中断，即使请求取消也持有名额直到计算返回，随后丢弃结果。新密码保留 Argon2id 64 MiB/t3/p1；历史摘要参数范围不变，最高允许 256 MiB。

队满或排队超时返回 `503 AUTH_BUSY`、`Retry-After: 1`、no-store，不签发 Cookie、不改密码/session、不消费验证码。界面显示“账号服务繁忙，请稍后重试”，由用户稍后重试；不要对认证写请求增加自动无限重试，也不要把这个错误算作密码不正确。已有安全尝试额度仍可计数。

私有 `/metrics` 提供 `self_deepsearch_password_work_active`、`waiting`、`active_limit`、`waiting_limit`、`wait_timeout_seconds`、`rejected_total`、`canceled_total`、`completed_total`。这些名称均加同一 `self_deepsearch_password_work_` 前缀，不包含账号/IP 标签。completed 是实际 KDF 完成次数，包含取消后丢弃结果及初始化计算，不能当登录成功率。指标重启清零；单个快照的 active/waiting 是瞬时值。

上线前在隔离测试环境、实际 384 MiB/0.40 CPU 限额内验证正常目录流量与认证突发共存时的 RSS、CPU、响应时间和拒绝恢复；检查全程 active 不超过 1、waiting 不超过 2，取消计算真正结束后名额归还。测试既有高参数合成摘要兼容性，不在正式服务器故意制造 OOM；若空间不足，先评估服务预算/GC 与负载，不直接降低密码算法强度。本轮未设置 GOMEMLIMIT；单并发不保证 RSS 低于限额。多 API 实例各自计数，Release A 仍保持单实例。

邀请接受预检使用短事务，正确码不消费邀请或预占额度；无效码计次，已达同 IP 日额度则在 KDF 前拒绝。最终接受再校验并原子创建账号/session、消费邀请、计额/审计，防止预检后过期、撤销或额度竞态。本轮新增三个 SQL 测试组、五个实际用例，沿用上一节受限 v21 一次性数据库；缺少数据库时 SKIP，不视为通过：

```powershell
go test ./services/platform-api/internal/database -run 'TestInvitationPrecheck|TestInvitationFinalValidation' -v -count=1
```

旧邀请夹具也已改为受限测试库守卫和随机合成命名空间，保留审计，不执行 DELETE 清理。当前 CI migrations 已覆盖该 package，未新增权限。护栏无需 Schema 迁移，更新全部 API 与 owner 工具即可；OpenAPI 增加 503/Retry-After 描述，前端错误结构兼容。具体证据与离线命令见[密码计算护栏验证](./evidence/release-a-password-work-2026-09-10.md)。

### 退出登录故障与恢复

Go API 只在撤销成功或请求本来没有 session 时返回 204，同时清除 session 和近期密码 Cookie。服务未配置、数据库失败、请求取消或 2 秒处理预算耗尽返回 `503 LOGOUT_UNAVAILABLE`，Cookie 保持不变；日志只保留 request ID 和错误分类。没有、已过期或已经撤销的 token 可重复退出，不需要先成功调用 `/me`。

两站的 POST `/auth/logout` 在联系 API 前校验同源 Origin；Host 使用入口保留的实际 Host，不使用 Next 内部 `localhost` 地址或客户端提供的 `X-Forwarded-Host`。生产 Nginx 必须继续覆盖 `X-Forwarded-Proto: https`。上游调用最长 3 秒，不跟随 3xx，200 HTML/JSON、202 或其他非 204 均不能视为撤销完成。失败转到 `/logout?error=unconfirmed`，显示“退出尚未确认”与重试按钮，不清登录凭据、不自动反复重试。不要以关闭页面、跳到登录页或删除客户端 Cookie 作为服务端撤销证据。

后台保留 `Referrer-Policy: no-referrer`。真实 Chrome 的原生表单在该策略下会发送 `Origin: null`；只有同时带 `Sec-Fetch-Site: same-origin` 和 `Sec-Fetch-Mode: navigate` 才走同源导航例外，不能放行普通 null Origin、same-site 或 cross-site。入口需透传浏览器这两个 Fetch Metadata 头，不得人为给所有请求补成 same-origin；缺失头的旧客户端应拒绝此例外。

新增页面强制动态、noindex/no-store，公开站 robots 排除 `/logout`；日本 Nginx 模板同步将 `/logout` 放入禁止缓存清单。真实 Cloudflare 自定义规则也必须同时绕过 `/logout` 和 `/auth/`。

配套发布顺序：先更新入口禁止缓存规则，再替换全部日本 API，最后替换公开站与后台两份前端构建。旧 API 会错误返回 204，旧前端会忽略失败，因此混合版本不能视为确认合同生效。此次没有数据库迁移，不需要更新北京服务或 Worker。

隔离测试环境至少验证：

1. 两站各登录一个测试账号并保留其测试会话；撤销操作受控失败时，API 返回 503，前端显示未确认、两枚 Cookie 不被清除。
2. 恢复依赖后点击“重试退出”，确认两枚 Cookie 消失，原 token 再调用 `/me` 返回 401；同一 token 再退出应返回 204且不覆盖首次撤销时间/理由。
3. 模拟上游 200 HTML/JSON、202、302、断开连接与不响应，确认都不会显示退出成功，302 不继续请求目标，超时有界。
4. 跨站/缺少 Origin 的 POST 被拒绝且不联系 API；GET 返回 405。入口保留正确 Host/协议，不能跳到内部 localhost。
5. 普通用户误入后台后，若自动退出失败，也必须进入明确的重试页。

浏览器合同可用 `npm run test:e2e:release-a` 验证两站 production build 与回环合成 API；真实 SQL 的幂等退出与撤销证据保护追加到 `TestPostgresIdentityLifecycleContract`，现在共 5 组。本机未配置 PostgreSQL 时仍 SKIP，不作为真实数据库或最终 Cloudflare 验收通过。

本轮完整桌面/移动 32 项、Go HTTP 8 个子场景、最终 Python 172 项 + 73 个 subtests 均通过，失败到恢复的记录、四张界面截图和复验命令见[退出确认验证](./evidence/release-a-logout-confirmation-2026-09-10.md)。

## 4. 人工发布流程

1. editor/admin/owner 在后台新建作品或人物；
2. 使用另一个 admin/owner 账号领取并批准，editor 不能领取审核任务，作者不能审核自己的 revision；领取后的任务只能由领取人完成批准或驳回，需要换人时必须先通过运营流程重新分配并留下审计记录；
3. 在“已通过待发布”中设置稳定 slug 并发布；
4. 公开站按番号搜索，确认详情、canonical 和 sitemap；
5. 页面打开后单独调用一次浏览上报，刷新算新的一次请求。

API、PostgreSQL 和两个不同的运营账号就绪后，可以用脚本自动验证上述核心链路。录入账号必须是 editor/admin/owner，审核账号必须是另一个 admin/owner；脚本会创建唯一合成作品，领取并批准审核任务，完成近期密码确认，发布后检查番号搜索、详情和 sitemap，最后隐藏该作品并确认三个公开入口都不可见。隐藏记录保留在数据库中用于审计，不会访问外部资料来源，也不会输出密码：

```powershell
$env:RELEASE_A_EDITOR_PASSWORD='<editor-password>'
$env:RELEASE_A_REVIEWER_PASSWORD='<reviewer-password>'
python scripts/release_a_catalog_e2e.py `
  --editor-email editor@example.test `
  --reviewer-email owner@example.test
$env:RELEASE_A_EDITOR_PASSWORD=$null
$env:RELEASE_A_REVIEWER_PASSWORD=$null
```

密码只通过本机环境变量提供，避免出现在命令参数、进程列表或仓库文件中。若发布后的公开断言失败，脚本会尽力再次隐藏刚创建的作品；清理也失败时会在错误中明确报告 entity ID，管理员应立即在后台隐藏。

已发布资料可在“资料目录”提交修订、查看完整 revision 快照和变化字段。审核期间旧公开版本继续可见。admin/owner 可选择已批准旧版本执行回滚；系统会追加新的回滚快照，不会修改历史版本。权利下架状态禁止通过回滚恢复。

排查运营动作时，从 API 响应头或错误体取得 Request ID，由 admin/owner 打开后台“审计查询”，输入完整编号进行精确查询。页面最多显示 200 条并按时间正序排列，只展示操作者、动作、对象、理由和精确时间；editor 不可访问，页面和 API 都不会返回 revision 前后快照或原始 metadata。若结果达到 200 条，应缩小到更具体的 Request ID 或直接由数据库负责人按事件流程调查，不要通过扩大公开响应字段来排障。

开发种子包含 30 个合成人物和 100 部合成作品，用于分页、混排、最新发行和搜索联调；其中未发布 `TEST-005` 可用于批准和发布流程。它们具有固定 ID、审核 revision、publication、搜索文档和 fixture 来源记录，不代表真实人物或作品，也不会由生产迁移自动加载。种子中的两个审计引用账号均为关闭状态且密码哈希不可登录，不会抢占真实 owner。

只在全新或仅含同一 fixture 的 Release A 测试库中，可以显式运行一次性加载器。SQL 自身要求 Schema v21 和值精确为 `1` 的 `--set release_a_fixture=1`，并拒绝存在非 fixture 用户、作品、人物、厂牌或来源的数据库；Shell/Compose 外层还要求双确认。加载器忽略用户级 `psqlrc`，在同一事务内锁住会写入的目录/审核表，5 秒无法取得锁即失败，因此必须在常驻 API/Worker 启动前执行。真实内容库、已经注册用户的数据库或公网生产库禁止执行：

```sh
export ALLOW_SYNTHETIC_FIXTURES=true
export CONFIRM_SYNTHETIC_FIXTURES=release-a-test-only
docker compose --env-file infra/compose/japan/.env \
  -f infra/compose/japan/compose.yaml --profile tools run --rm database-fixtures
unset ALLOW_SYNTHETIC_FIXTURES CONFIRM_SYNTHETIC_FIXTURES
```

推荐首次测试库顺序为 migration → runtime logins → 可选 fixtures → owner bootstrap → 常驻服务。加载器可对纯 fixture 库幂等重跑；一旦创建真实 owner、注册用户或人工资料，就会按设计拒绝再次执行。

发布、隐藏、权利下架、编辑推荐排期和首页发现流规则变更提交后会写 outbox。日本 Worker 使用 `CACHE_HMAC_SECRET` 对时间戳、事件 ID 和精确请求体签名，再调用 `DISPLAY_REVALIDATE_URL`；公开站验证 5 分钟时间窗后立即失效 `public-catalog` 标签和根布局。只有 HTTP 200、`application/json`、`revalidated=true` 且 `event_id` 与本次请求完全一致，事件才会完成。回执最多 4096 字节，必须是完整 UTF-8 JSON；空 200、HTML 登录页、202 受理响应、截断/超大内容和不匹配回执均以 `revalidate_invalid_response` 重试，不会误写 completed。联调时应确认首页、搜索、详情、人物/厂牌列表和 sitemap 不必等待 5 分钟即可反映变更；停掉公开站时事件应进入 retry，恢复后自动完成。

`CACHE_HMAC_SECRET` 使用至少 32 字节随机值，且不能复用 `AUTH_HMAC_SECRET`、`INGEST_HMAC_SECRET` 或 `METRICS_TOKEN`。该值只进入日本 Worker 和公开站的服务器运行环境，不能使用 `NEXT_PUBLIC_` 前缀。反向代理不得把 `/api/internal/` 转发到公网；Worker 也不会跟随 3xx，以免签名头被带到错误目标。若 Cloudflare 代理最终域名，账号页、搜索和 `/api/` 必须 bypass cache；公开目录 HTML 可按 Nginx 5 分钟策略缓存，只缓存已确认的公开图片和版本化静态资源，否则 Next 缓存失效不能清除 Cloudflare 的额外页面缓存。

本地可在不启动 Docker 的情况下先验证真实 Go→Next 缓存链路：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
npm run test:cache:release-a -- --output docs/evidence/release-a-cache-contract-local.json
```

需要已构建的 standalone 产物、Go 及已缓存依赖；首次可追加 `--build`。工具只监听随机回环端口、使用每次随机的临时 HMAC 和内存合成目录，没有目标 URL 参数，不读取生产缓存密钥。真实 Go 处理器必须执行而非 skip，真实 Next 页面必须更新而非仅返回成功回执。测试覆盖首页、作品详情和 sitemap，记录 HTTP 状态/回源次数，不保存正文、账号、密钥或事件 ID；失败返回非零状态并保存失败报告。为防止共享 Next 数据缓存污染，运行前在 `.cache/cache-contract-*` 创建独立可写 standalone 副本，正常或捕获异常退出时清理自己的副本；不要把这类临时目录部署到服务器。复制会暂时占用一份公开站 standalone 产物的磁盘空间。

该合同属于统一预检的必过项，CI web 在 build 后执行并上传 `release-a-cache-contract-ci.json`，失败阻断镜像交付。它不运行数据库 Claim/租约/重试调度，不覆盖 Nginx、Cloudflare、多实例缓存传播或真实 API；这些仍按目标环境流程复验。隐藏断言还识别 Next 流式 `notFound()` 的 noindex/404 UI，不能把普通 HTTP 200 或“服务不可用”页面冒充成功下架；当前本机实际隐藏响应为 HTTP 404。证据见 [Go→Next 缓存验证](./evidence/release-a-cache-contract-2026-09-10.md)。

### outbox 租约与失败重试

日本 Worker 默认使用 30 秒租约、每批 2 条。每次 Claim 返回原始 `attempt_count` 和数据库 `lease_expires_at`；Complete/Fail 必须匹配这次领取，且应用时间和数据库当前时间均未超过租约。同一 `WORKER_ID` 重启后也不能用旧次数提交新任务的结果。仍建议每个同时运行的实例使用不同 Worker ID，并保持时钟同步。

副作用只能使用本批剩余租约，预留 1 秒提交结果。本批执行预算耗尽、租约失效或进程关闭后不会伪写成功，也不继续执行本批下一条；普通 HTTP 失败若仍有足够租约时间，可以记录重试后处理下一条。未提交的 running 任务在租约过期后重领。成功回执丢失时远端可能已经执行，重领会再次调用，因此不能承诺“只执行一次”。正常失败指数退避，第 10 次失败转 dead；已崩溃耗尽次数的 running/retry/pending 任务在下一轮符合领取条件时转 `dead`，错误码 `attempts_exhausted`，避免单条任务碰到次数 CHECK 约束并卡住整批。不能把 dead 当成 completed，也不要在旧执行可能存活时手工清零次数来复用领取凭据。

本轮不增加 Schema 迁移或改变 HTTP 签名协议；发布前必须执行真实 Worker 数据库合同，并停止旧日本 outbox 消费者后再替换，避免没有次数校验的旧进程继续落库。北京媒体服务本轮无需调整；此前完成回执的配对升级要求仍有效。

CI migrations 作业会创建独立空库 `self_deepsearch_worker_test`、迁移到 v21、配置 `platform_worker_login`，执行 `TestPostgresOutboxLeaseContract`。它不与 API fixture 测试库混用，要求回环地址、固定库名、显式 `CONFIRM_WORKER_OUTBOX_CONTRACT=disposable-database`，并用 advisory lock 排除并行合同；检测到既有用户、作品、媒体资产或 outbox 时拒绝。管理员连接只创建/清理合成事件，实际 Claim/Complete/Fail 使用受限 Worker 登录。五类合同包括旧领取成功/失败拒绝、退避与 dead、崩溃次数耗尽、并发领取和媒体完成时的行锁竞态；本机尚无真实 PostgreSQL 运行证据。

手动复验需先由受控终端配置上述确认值、`WORKER_CONTRACT_ADMIN_DATABASE_URL` 和 `WORKER_CONTRACT_DATABASE_URL`，再执行下列命令。只能指向同名一次性空测试库，不要用于运营库，也不要在聊天中提供凭据：

```powershell
go test -count=1 -timeout=90s -v ./services/platform-worker/internal/outbox -run '^TestPostgresOutboxLeaseContract$'
```

未设置两个地址时普通本地单测会明确 skip，不可把它当作数据库通过；设置不完整、错误库名或缺少确认则直接失败。代码和证据说明见 [Worker 租约加固记录](./evidence/release-a-worker-lease-2026-09-10.md)。

### 4.1 字段冲突

开发种子包含 `TEST-001` 的一条发行日期冲突。editor 可在后台“字段冲突”查看，admin/owner 可选择保留现值、接受候选、标记未知、合并或拒绝候选，并填写裁决理由。

裁决本身只更新 `collector.conflict_reviews` 和审计记录，不修改正式资料。选择接受候选、标记未知或合并后，仍需从“资料目录”创建正式修订并走完审核、发布。重复裁决必须返回 409；同一任务全部冲突处理完后，任务才变为已完成。

### 4.2 作品 CSV 预检

后台“CSV 导入”先执行服务端预检。文件上限 1 MiB、最多 500 条非空数据，UTF-8 表头为 `code,title,title_original,release_date,studio_id,performer_ids,summary`，其中 `code` 和 `title` 必填，多个 `performer_ids` 使用 `|` 分隔。

预检检查未知/重复/缺失表头、字段长度、`YYYY-MM-DD` 日期、UUID、每行最多 20 个人物、行内重复人物和批内规范番号重复。错误可下载为 CSV；零错误后才能提交。

正式提交使用 `Idempotency-Key`，并在单个事务中写作品、reviewing revision、人物关联、审核任务、人工来源记录、标准化记录、批次摘要和审计。相同键与相同文件/理由返回原批次，相同键但内容不同返回 409，任一数据库约束失败则整批回滚。后台保留最近 100 个手工 CSV 批次。

## 5. 图片处理与 manifest

### 当前能力与未完成项

本模块已接入主图替换，2026-09-12 Ubuntu 真实 SQL 合同已通过；真实对象存储与完整跨区生命周期仍待验收。普通登记仅新增独立资产，不覆盖已有主图；替换必须使用专门接口和明确的旧资产 ID。重复下架的原 URL 保留、逐对象任务幂等与未发布资料下架保留原机制。当前整体 Schema 为 v21。

`POST /admin/v1/media/manifests` 在同一事务中写资产、对象、派生关系、关联、审计与 `cache_purge`。201 表示登记及刷新任务已提交，不表示所有页面/边缘缓存已更新；409 包含主图/不可变对象冲突及父资料已下架/合并。503 不表示成功，提交结果不明时先核对原 `asset_id`，不要更换 ID 反复提交。草稿和普通隐藏资料仍可提前登记图片，但 v17 公开视图只返回所属资料已公开的图片。

展示槽位必须按以下固定组合生成，后台不允许手改清单绕过：作品主图为 `--purpose cover --primary --position 0`；作品精选图为 `--purpose gallery --position 1|2|3` 且不加 `--primary`；人物头像为 `--entity-type performer --purpose avatar --primary --position 0`。同一作品的精选图位置不可重复，必须先有有效主图，最多三张。第四张、重复位置、只有精选图没有主图或用途/主图标识/位置不匹配均拒绝登记。公开 API 也会过滤历史异常行；没有有效主图时页面使用默认图，不把精选图顶成封面。

在已迁移的专用回环库 `self_deepsearch_migrate_test` 中，用受限 `platform_api_login` 设置私有环境变量 `PLATFORM_API_TEST_DATABASE_URL` 后执行以下读取和替换负例。该命令会创建合成资料并修改其图片槽位，仅适用于可丢弃测试库；不需要真实图片或对象存储。

```powershell
go test -count=1 -v ./services/platform-api/internal/database -run 'TestMediaReadsPostgres|TestPrimaryReplacementPostgresRejectsInvalidCurrentSlot'
```

检查各 SQL 子场景实际执行且无 SKIP：合法/异常/恢复后，资料存在性、封面/头像、收藏、关注、历史和后台主图结果一致；图库按槽位排序且缺封面不提升精选图；拒绝替换不会写入新资产，也不会改变旧对象或排入旧资产删除任务。测试代码及本地执行边界见[读取防御证据](./evidence/release-a-media-primary-read-guards-2026-09-12.md)。

数据库目录隔离不等于公开桶访问控制：已上传的派生图即使尚未登记，也可能经已知 URL 访问；只向本链路输入事先已确认允许公开的图片。不得把私密待审图片放入公开桶。

### 主图替换（本地代码已验证，真实环境待验收）

1. 使用媒体处理器生成不同 `asset_id` 的新主图清单，保持 `is_primary=true`，完成 S3 与北京副本准备；不要手改清单中的路径或对象标识。
2. 以 admin/owner 打开运营后台“图片资产”，导入清单。页面调用 `GET /admin/v1/media/primary?entity_type=work|performer&entity_id=UUID`，只读取父状态与当前主图关联，不读取私有母版路径。
3. 无主图时走普通登记。存在主图时，核对旧资产 ID、权利/副本，并额外勾选替换确认；提交 `POST /admin/v1/media/primary/replace`，请求为 `{ "expected_asset_id": "旧 UUID", "manifest": { "处理器生成内容": "保持不变" } }`（此处仅示意结构，不是可提交的完整清单）。近期密码确认机制同其他高风险写入。
4. 201 表示新旧关联切换、审计、缓存刷新与必要的删除任务在同一事务提交。旧图仍有其他 published 关联则保留；否则公开对象立即排队，私有母版在返回的 UTC `private_retained_until` 后才可领取删除。权利下架可提前；Worker 停止或失败时不保证准点删除。
5. 409 必须刷新当前主图、重新确认，不能自动改预期 ID 重试。503、网络断开或响应不完整均不显示成功，清单保留；若刷新发现新资产已是主图，不再次提交，按 `media.primary_replace` / `media.manifest_publish` 审计及 outbox 核实。
6. 核验 `media-publish:{新资产ID}` 与 `media-delete:{旧对象ID}`，等待真实回执后检查公开页面、旧 URL、私有 bucket、北京副本与容量统计。成功提示不是物理删除回执，也不代表父资料已经公开。

容量仍按新旧对象的实际存量计算：替换需要先上传新图，旧母版保留期间不会提前释放其额度。达到容量上限时先处理对账/到期清理或调整容量，不通过隐藏记录假装已腾出空间。

真实测试库须执行 `TestPrimaryReplacementPostgres` 四组合同：作品/人物 CAS 与期限、共享保留/冲突回滚、并发仅一方成功、非法当前槽位拒绝且无持久变更；同时执行此前五组退出合同，验证权利下架提前、dead/租约保留和旧 URL 恢复。本机缺库 SKIP 不算通过。当前本地证据见[主图替换验证](./evidence/release-a-media-primary-2026-09-10.md)。

### Schema v21 配套升级（含迁移 17 隔离、18 检查、19 用量观察、20 复核、21 上传回执）（未在真实库执行）

PostgreSQL 引擎保持 16。隔离测试环境先执行 v1-v21 fresh/roundtrip 及 `media_visibility_contracts.sql`，再用受限 API 登录执行 `TestMediaRegistrationPostgres` 三组合同、上述媒体读取合同和[上传队列合同](./MEDIA_UPLOAD_QUEUE.md)；缺库 SKIP 不算通过。已有 v16 环境须在维护窗口关闭公开入口，依次执行迁移 17、18、19、20、21，替换配套 API/Worker 并确认 readiness 为 v21，同步 seed/监控/测试配置。`CREATE OR REPLACE VIEW` 保留原列和授权，不扩大公开读者底表权限。

迁移不会自动清掉旧 HTML/边缘缓存。确认新 API 正常后，在受控迁移连接中登记一次全站目录刷新任务，等待 Worker 的真实成功回执，再按已配置规则清理 Cloudflare 目录 HTML；不能手工标记事件 completed：

```sql
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ('site', gen_random_uuid(), 'cache_purge',
        '{"reason":"schema17_public_media_parent_visibility"}'::jsonb,
        'schema17:public-media-parent-visibility')
ON CONFLICT (event_type, dedupe_key) DO NOTHING;

SELECT event_id, status, last_error_code
FROM platform.outbox_events
WHERE event_type = 'cache_purge' AND dedupe_key = 'schema17:public-media-parent-visibility';
```

首次 fresh 环境无旧公开缓存时不需要此维护事件。Release A 仍按单 Next 实例；多实例必须逐实例验证失效，不能把单实例回执当成全局保证。用合成草稿/隐藏/下架资料验证公开图片视图、页面、搜索和缓存后才能恢复入口。优先 roll-forward；v17 Down 会恢复旧隔离缺口，只供一次性数据库 roundtrip，不能在公开入口开启时回退。

### 一次性 R2 Analytics 核对

该命令不启动常驻 Worker、不连接 PostgreSQL，也不更改图片模式。先离线查看帮助；真实 token 只放受控终端环境变量，不写聊天、文档或命令参数：

```powershell
go run ./services/platform-worker/cmd/worker media-usage --help
```

现场确认账户、账期和只读权限后，使用编译后的 Worker：

```text
platform-worker media-usage --live --period-start <已确认账期起点RFC3339>
```

v2 报告同时查询账户操作量和 Storage Analytics。同一存储时点跨全部已观测 bucket 求和，按 UTC 日取峰值并输出 30 日制 GB-month 微单位估算；缺日/缺 bucket/partial/截断/溢出时整份存储观测失败。只有核实该账户所有 bucket 都是 Standard 时才追加 `--standard-only-confirmed`，混合或未知类别不得追加。即使退出 0，`billing_verified` 和 `provider_watermark_verified` 仍为 false，不能用于自动恢复或代替控制台账单。完整边界见[只读用量观察](./MEDIA_USAGE_OBSERVATION.md)。

### 受控图片准备

Release A 不由 API 下载图片，也不信任来源 URL 的内容。管理员先把已核验权利的本地图片交给北京 `media-python`；处理器按实际解码结果接受 JPEG/PNG/WebP，拒绝畸形、动画、解压炸弹、超过 10 MiB 或 4000 万像素的输入，去除元数据后生成私有 WebP 母版和 `w320/w640/w960` 三档公开图。每个对象先原子写入北京副本，再上传 S3/R2，并用对象字节数和 `sha256` 元数据执行 HEAD 校验；任一步失败会尽力回滚本批对象。

```bash
python -m media_worker.cli prepare \
  --input /srv/media-inbox/cover.jpg \
  --entity-type work \
  --entity-id 10000000-0000-4000-8000-000000000001 \
  --purpose cover --primary --position 0 \
  --reason "rights and source reviewed" \
  --backup-root /var/lib/self-deepsearch/media \
  --upload-lock-file /var/lib/self-deepsearch/media-state/upload.lock \
  --output /var/lib/self-deepsearch/media-state/manifests/cover.json
```

S3/R2 连接从 `S3_ENDPOINT_URL`、`S3_REGION`、`S3_PRIVATE_BUCKET`、`S3_PUBLIC_BUCKET`、`S3_PUBLIC_BASE_URL`、`AWS_ACCESS_KEY_ID` 和 `AWS_SECRET_ACCESS_KEY` 读取。两个 bucket 必须不同：母版进入不绑定任何公开入口的私有桶，三档派生图进入“公开内容范围”桶，但该桶启用网关后也必须关闭匿名 `r2.dev`/旧 custom domain，只通过 Cloudflare Worker 私有 binding 读取；不能只靠 key 前缀保护母版。`S3_PUBLIC_BASE_URL` 指向受控 media origin。生成的 JSON 同时包含预生成 `asset_id`、来源、存储范围、私有母版、三档派生图、S3 key、北京相对路径、公开 URL、SHA-256、尺寸和字节数。运营后台只导入该文件并只读核对，不再人工填写对象元数据。数据库保存同一 scope/key/path/hash 映射和母版到派生图关系。没有图片或公开图片不可用时，公开站或边缘网关继续使用默认图。

`MEDIA_STORAGE_LIMIT_BYTES` 默认是 `10737418240`（10 GiB）。每次 `prepare` 持有共享操作系统锁，对两桶全部当前对象和未完成 multipart 已上传部件分页汇总（包括非媒体前缀），加上本批四对象预计超限时在写副本/上传前拒绝；缺少完整两桶 ListObjects/ListMultipartUploads/ListParts 权限、分页/竞态异常或整次扫描超过 256 个列表请求也拒绝。锁覆盖到 manifest 持久化；PUT 超时、进程中断或清单写入失败会保留 pending，阻止后续上传。已确认删除的对象才清理对应备份，未知写入和失败删除保留备份；本流程不自动中止未知 multipart。删除/权利下架不受上传锁限制。

全部上传命令须共享北京同一宿主机/Compose 项目的 `media-state` 卷；不能把锁放进 `/tmp` 或从另一主机直接上传。新镜像预建 UID 10001 私有目录，旧卷所有者需要部署前核对。`upload-status` 查看未确认批次，`acknowledge-upload` 仅在人工确认远端写入已停止并完成两桶/副本核对后按精确 run-id 解除；它不是 S3 自动验证器。详见[容量与恢复手册](./MEDIA_UPLOAD_CAPACITY.md)。

70/85/95% 提示不等于供应商账单硬限流；[定时全量对账](./MEDIA_TASK_ADMISSION.md)及[逐操作上传](./MEDIA_UPLOAD_ADMISSION.md)已接独立用量准入开关。`MEDIA_DELIVERY_MODE=default_only` 可人工暂停新准入和页面图片；日本 API/公开站可另设 `MEDIA_DELIVERY_DYNAMIC_MODE=enforce`，要求 observe，并按每次 API/文档请求读取持久操作量状态。动态状态不安全或依赖错误时，API 去图、Next CSP 让旧 ISR HTML 使用本地默认图，edge 尊重 no-store。另可按[边缘网关手册](./MEDIA_EDGE_GATEWAY.md)启用 API edge policy 和 Cloudflare Worker，使已知 URL和旧对象缓存也在最多 5 秒策略 TTL 后返回默认图。首次启用需配套替换 API/公开站/edge，并私有化派生 bucket；之后状态变化无需重启，但 HTML 停图/恢复仍应执行目录缓存失效。Next 内部策略不得公网开放，edge policy 仅允许独立 Bearer。人工复核/账期切换是清除粘性保护的授权路径，真实 SQL 待验收；一次性存储 Analytics 已能估算日峰值/GB-month，但可信账单、存储类别、版本历史/共享应用完整性和网关真实验收仍未完成，已下载到旧标签页的字节无法撤回。上线前核对 GB/GiB、版本/分段/其他桶占用并预留额度，不承诺免费费用上限。

`S3_PUBLIC_BASE_URL` 也必须配置到日本 API；API 只接受与该基地址和对象 key 精确一致的公开 URL，避免发布域名与 Cloudflare purge 区域不一致。

## 6. 权利下架

当前删除队列采用 `media-delete:{media_object_id}`，不同权利工单复用同一对象任务；重复提交同一个已 completed 工单仍为 409。未发布资料不需要先发布即可下架。工单 completed 表示数据库可见性与任务登记完成，不代表远端删除完成，仍必须检查 Worker 回执、对象 deleted 状态和异常队列。

原公开地址会保留在删除 payload；已撤下对象按资产、存储范围、key 和北京路径精确匹配既有任务恢复地址。若当前记录与历史任务均没有原地址，API 返回 503 并回滚本次执行，管理员须依据不可变 manifest/对象核对修复，不能用来源页地址、猜测域名或伪造 completed 绕过。已有公开元数据损坏的情况下，先保持相关入口不可见，再按受控数据修复流程处理。

权利下架可把待执行私有保留任务提前到当前时间，但不推迟任务、不改 payload、不重置 attempts、不抢占 running 任务，dead 也不会被新工单隐式复活；dead 应在排除失败原因后走独立人工复核/重试。新格式增加 `media_object_id`，现有 Worker 忽略此附加字段，仍按 scope/key 与实际回执结算。先替换全部 API，避免旧实例继续产生空地址任务；本轮无需迁移，当前整体 Schema 为 v21。

旧版按工单生成的任务不自动删除或改写。一旦重新执行相关资产下架，最多新增每对象一条规范任务，旧任务仍可能执行一次重复删除，由远端幂等保证安全；旧 dead 需单独审查，不能为美化指标删除历史。可选 SQL 合同用前文受限测试库运行 `go test ./services/platform-api/internal/database -run TestMediaRetirementPostgres -count=1 -v`，包含重复工单/租约、保留期限提前、旧格式恢复、缺失身份回滚和草稿图片下架，本机目前全部 SKIP。

在后台“权利下架”登记邮件工单标识、实体 ID 和证据引用，再由 admin/owner 执行。执行后公开发布、搜索和媒体链接立即消失，数据库中的公开媒体 URL 被置空，并留下审计和 `media_delete` 事件。

下架事务先保存原公开 URL 到 outbox，再撤掉数据库公开 URL 并把对象置为 `hidden`，不会提前从容量指标中扣除。日本 Worker 使用 `MEDIA_HMAC_SECRET` 签名精确请求体并调用北京 `MEDIA_DELETE_URL`；北京服务只接受 5 分钟时间窗内、scope/key/备份路径/公开 URL 一一对应的请求。它删除并 HEAD 确认 S3/R2 不可访问；公开派生图还必须使用 `CLOUDFLARE_ZONE_ID` 和仅具单文件 purge 权限的 `CLOUDFLARE_API_TOKEN` 清除边缘缓存，随后才删除北京副本。Worker 最后把对象置为 `deleted` 并完成 outbox。任何 R2、purge 或本地删除失败都保留事件重试。北京端口只对日本服务器防火墙放行；生产优先使用 VPN/私网或 TLS，HMAC 密钥不得复用缓存密钥。

删除成功必须返回 HTTP 200、`application/json` 和下列回执，且小于等于 4096 字节；`event_id`、`storage_scope`、`storage_key` 都必须与任务相同。回执由实际删除服务在 S3/缓存/副本步骤全部结束后生成，不是接口收到请求就预先返回成功。若只有旧式空 200 或状态/对象不匹配，Worker 写入 `media_delete_invalid_response` 并保留重试，不完成 outbox、不把对象记为 deleted，也不提前释放数据库中的容量占用。

```json
{
  "status": "deleted",
  "event_id": "11111111-1111-4111-8111-111111111111",
  "storage_scope": "public",
  "storage_key": "media-public/contract/image.webp"
}
```

此次变更无需数据库迁移，升级顺序为：先部署北京 `media-python`（增加 scope/key 回执），再部署日本 `platform-worker`（强制核对回执）。旧 Worker 可接受新回执，但新 Worker 不接受只含 status/event_id 的旧回执；若顺序相反，任务会重试，达到现有次数上限后进入 dead。升级后应检查异常队列并按运维流程重试，不能手工伪造 completed；回滚也必须保持两端回执兼容。回执确认不替代 TLS/VPN 的传输安全。

Worker 的 dispatcher 必须显式提供业务处理器；漏配时拒绝执行，不能退化为“仅验证事件格式后标记成功”。本地可运行真实 Go→Python HTTP 合同：

```powershell
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go test ./services/platform-worker/internal/outbox -run TestMediaDeletionPythonHTTPContractRetryAndRecovery -count=1 -v
```

该合同启用真实签名、HTTP handler 与删除服务，仅使用临时文件替代 S3 和 Cloudflare；先验证 purge 失败不完成，再验证恢复后同事件幂等删除并完成。它不连接 PostgreSQL，也不代表真实 R2/Cloudflare 已验收。CI 的 Go/Go race 作业已通过 Python 3.12 和 `MEDIA_CONTRACT_PYTHON=python3` 启用该合同；未提供解释器时普通 Go 测试会显式跳过它。

启用 `MEDIA_RECONCILE_ENABLED=true` 后，日本 Worker 每 24 小时从 PostgreSQL 读取最多 10,000 个未删除对象，生成带 run ID 的清单并调用 `MEDIA_RECONCILE_URL`。北京服务核对私有/公开 bucket、S3 HEAD 字节和 SHA-256、北京副本字节和 SHA-256，并分别列出数据库未引用的 S3 对象与北京孤儿文件；报告写入 `audit.media_reconciliation_runs`，只报告不自动删除。数据库调度锁和最近运行时间可避免 Worker 重启时反复消耗 R2 读取额度；清单超过上限、服务不可达或报告不完整都会留下失败运行和告警依据。

对账确认现在要求完整的 HTTP 200/JSON 报告：全部计数显式存在、run ID 与清单数一致、样本类别/计数/路径合理。缺计数/null/重复字段、尾随数据、超限或无效 UTF-8 都返回失败并记录 `invalid_report`，不会补成全 0 后完成。正常服务失败记录 `reconcile_unavailable`。样本每类最多 50 条且转义后 8 KiB，可能少于 50 条，异常总数仍完整；不能将样本为空理解成没有异常。

建议先升级北京媒体服务，再升级日本 Worker，该响应加固本身无新迁移；当前套件需 v21（每日检查迁移 18、观察状态迁移 19、复核回执迁移 20）。Worker 调度在 advisory lock 获得后读取最新运行快照；真实 SQL 并发合同等待两个连接都被锁阻塞后才放行，要求只有一条运行。CI 已把它排在 outbox SQL 后串行执行；2026-09-12 Ubuntu 真实 PostgreSQL 16.15 调度与并发合同已通过。使用同一专用回环 `self_deepsearch_worker_test` 库时，设置 `MEDIA_RECONCILE_CONTRACT_DATABASE_URL`（受限 worker）、`MEDIA_RECONCILE_CONTRACT_ADMIN_DATABASE_URL`（仅核对空库/Schema/测试锁）和 `CONFIRM_MEDIA_RECONCILE_CONTRACT=disposable-database`，再运行：

```powershell
go test ./services/platform-worker/internal/mediareconcile -run TestPostgresReconciliationScheduleContract -v -count=1
```

不得与 outbox 合同并行或指向真实业务库。无数据库时可用 `MEDIA_CONTRACT_PYTHON` 执行 `TestReconciliationPythonHTTPRetainedMasterAndRecovery`，仅启动临时本机 HTTP 与文件替身，不代表真实数据库或供应商通过。

当前检查会把尚未物理删除的保留母版视为应存在对象，隐藏不释放容量。远端已删但数据库尚未确认的短暂窗口仍可能报缺失，应结合删除 outbox 状态核对，不能直接忽略隐藏图片。每日注册公开状态与默认图定时检查已独立接入（见每日检查手册）；现有对账完成不证明 bucket 权限或全部图片健康。详见[对账验证](./evidence/release-a-reconciliation-2026-09-10.md)。

## 7. 数据库与测试

Ubuntu 真实验收结果见[2026-09-12 记录](./evidence/release-a-ubuntu-acceptance-2026-09-12.md)。`postgres_roundtrip.sh` 的 Down 会删除集群级角色，应使用独立 PostgreSQL 实例；不要与 API/Worker/core-e2e 库共用同一集群执行回滚。API/Worker/core-e2e 在同一临时实例的不同数据库执行时，运行账号密码必须保持一致，因为登录角色属于集群级。

不启动 Docker 的统一本地预检：

```powershell
$env:PYTHON='C:\path\to\python.exe' # Python 已在 PATH 时可省略
npm run preflight:release-a
```

该命令执行 Go、前端、Python、production build 和 standalone HTTP 冒烟；增加 `--with-e2e` 时会在当前 production build 上执行当前完整 Playwright 套件（39 个场景、桌面/移动 78 项）。使用 `--with-compose` 时才增加三套 Compose 静态渲染，仍不会启动容器。

```powershell
go test ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
npm run lint
npm run typecheck
npm run build
docker compose config --quiet
docker compose --profile monitoring config --quiet
```

网络受限但仓库本地缓存已准备好时，可让 Go 校验完全离线运行：

```powershell
$env:GOMODCACHE=(Join-Path (Get-Location) '.cache\go-mod')
$env:GOCACHE=(Join-Path (Get-Location) '.cache\go-build')
$env:GOPROXY='off'
go test ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
```

有 Bash 和独立 PostgreSQL 16 测试库时：

```sh
python -m pip install -r requirements-test.txt
ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
python -m unittest discover -s db/tests -p 'test_*.py'
python -m unittest discover -s infra/backup/tests -p 'test_*.py'
python -m unittest discover -s workers/collector-python/tests -p 'test_*.py'
python -m unittest discover -s workers/media-python/tests -p 'test_*.py'
DATABASE_URL='postgres://platform:password@127.0.0.1:5432/self_deepsearch_test?sslmode=disable' sh db/tests/postgres_roundtrip.sh
```

### 7.1 真实核心服务 CI 验收

CI 新增独立 `core-e2e` 作业：使用一次性 PostgreSQL 16 和 Mailpit，执行真实迁移、配置受限运行账号、编译 Go API 和 owner 引导工具，然后运行 `scripts/release_a_core_e2e.py`。该门槛失败时不能进入五镜像构建交付；它不使用前端 mock API。

流程为：随机 owner 引导 → API/数据库/Mailpit ready → 注册验证码 → 密码重置及旧 session 失效 → 关闭账号 → 普通用户邀请 → 邮件邀请 editor → editor 录入 → owner 审核发布 → 番号搜索/详情/sitemap → 隐藏后不可见。所有密码随机生成，通过进程环境或 HTTP 请求体传递，不放入命令行；邀请与发布都执行真实近期密码确认。

也可在已有本地 PostgreSQL/Mailpit 的专用环境重跑，不需要运行器启动 Docker。前置条件：

- 一个全新的空测试库，名称必须是 `self_deepsearch_core_e2e_test`；不要加载演示 seed 或复用真实资料库。
- `ADMIN_DATABASE_URL`、两种运行账号密码已在受控终端环境准备；`CORE_E2E_DATABASE_URL` 使用 `platform_api_login`、同一库名和 `127.0.0.1` 或 `::1`，不接受远程域名、管理员登录或 host 覆盖查询参数。
- Mailpit 在回环 SMTP `1025`、HTTP `8025` 就绪；Go、Python 3.12 和 `psql` 已安装。

```sh
MIGRATION_DIR=db/migrations sh infra/database/migrate.sh
sh infra/database/provision_runtime_logins.sh
mkdir -p .cache/core-e2e
go build -trimpath -o .cache/core-e2e/platform-api ./services/platform-api/cmd/api
go build -trimpath -o .cache/core-e2e/create-owner ./services/platform-api/cmd/create-owner
CONFIRM_RELEASE_A_CORE_E2E=disposable-database \
  python scripts/release_a_core_e2e.py --output docs/evidence/release-a-core-e2e-local.json
```

Windows 构建的两个文件加 `.exe`，运行器会自动选择这个后缀。脚本不会创建、清空或重置数据库，也不会启动 PostgreSQL/Mailpit；只启动自己的 API 进程，正常结束和流程失败都会停止并回收该进程。每次产生随机 owner，重复使用同一库通常会被已有 owner 的保护规则拒绝，应重新提供独立空测试库，而不是删除真实用户绕过保护。

CI 只上传 `release-a-core-e2e-ci.json`，包含阶段、检查结果、时间和错误类型；不上传原始进程日志、响应体、Cookie、密码或验证码。测试账户、隐藏作品和审计数据留在一次性数据库内，随 CI 服务销毁；不要把它用于共享运营库。配置缺失时在启动进程和访问网络前拒绝。

证据边界：即使该作业通过，也只证明真实 Go API/PostgreSQL/Mailpit 核心链路；不证明 Worker/outbox、两个前端与 API 的真实集成、Turnstile、S3/R2、Cloudflare 或日本/北京目标环境已验收。2026-09-12 已在 Ubuntu Docker 的隔离 PostgreSQL 16/Mailpit 环境执行并通过，见[本轮真实核心证据](./evidence/release-a-core-e2e-ubuntu-2026-09-12.json)。

### 7.2 两站真实浏览器与 Worker CI 验收

独立 `real-stack-e2e` 作业在 7.1 的基础上增加真实日本 Worker、两个生产构建的 Next 站点及 Chromium。它不使用目录 API 替身，也不直接调用内部 revalidate：owner 浏览器发邀请，editor 通过 Mailpit 验证码接受邀请并录入带来源作品，另一个 owner 审核发布，匿名桌面/移动浏览器检查搜索与详情，然后 owner 隐藏作品。

发布前和隐藏前分别请求首页、搜索、详情、sitemap；Worker 消费真实 outbox 后，四种公开结果必须在 60 秒内变化（默认缓存 TTL 为 300 秒），不能等待 TTL 自然过期来冒充失效成功。首页、详情和 sitemap 是缓存失效检查；搜索 API 使用 `no-store`，只用于验证公开可见性，不称作缓存命中或失效证据。该流程不上传图片、不调用外部 SMTP、Turnstile、R2 或 Cloudflare。

必须使用单独新建的 `self_deepsearch_core_e2e_test` 空库，不得与 7.1 复用已写入 owner 的库；CI 的两个作业运行在各自独立的服务容器。沿用 7.1 的迁移、运行账号及 Mailpit 准备，额外提供 `REAL_STACK_WORKER_DATABASE_URL`，使用 `platform_worker_login`，并与 API URL 指向同一个回环端口和数据库。运行器不会自行删除或重建数据库。

```sh
go build -trimpath -o .cache/core-e2e/platform-worker ./services/platform-worker/cmd/worker
npm ci
npm run build
npx playwright install --with-deps chromium
CONFIRM_RELEASE_A_REAL_STACK_E2E=disposable-database \
  python scripts/release_a_real_stack_e2e.py --output docs/evidence/release-a-real-stack-local.json
```

需先按 7.1 构建 API 和 create-owner。运行器随机选择自身服务端口，限制浏览器只访问两个回环站点，并隔离子进程环境；只提交固定检查结果、脱敏失败阶段及缓存耗时，不保存账号、验证码、Cookie 或原始服务日志。完成后回收本次进程和前端隔离目录；数据库、Mailpit 由创建方销毁。本机 Ubuntu 验收不等于目标域名、跨区故障或真实负载通过。

### 7.3 历史归档与只读恢复核验

`migrations` CI 在专用 `self_deepsearch_worker_test` 库中串行执行历史归档 PostgreSQL 合同；使用受限 Worker 账号，覆盖软删除筛选、分批/行锁、幂等、manifest、文件校验、临时表逐字段恢复和失败回滚。历史源记录必须仍然不可见，不通过恢复操作撤销用户的清除选择。

独立运行需提供 `HISTORY_ARCHIVE_CONTRACT_ADMIN_DATABASE_URL`、`HISTORY_ARCHIVE_CONTRACT_DATABASE_URL` 和 `CONFIRM_HISTORY_ARCHIVE_CONTRACT=disposable-database`，并先完成 Schema v21 迁移与角色配置。所有 URL 只允许固定名称的本地一次性测试库，不得使用生产库。

```sh
go test -count=1 -timeout=90s -v ./services/platform-worker/internal/historyarchive \
  -run '^TestPostgresHistoryArchiveContract$'
```

离线归档校验工具 `worker history-archive-verify` 不连接数据库。它使用可信 manifest 导出核对 SHA-256、大小、gzip、严格 JSONL、用户范围和精确时间；具体参数及私有离线目录要求见[历史归档使用说明](../services/platform-worker/internal/historyarchive/README.md)。恢复演练只证明本地文件与数据库合同，目标存储的持久性、复制及运维演练仍需独立验收。

## 8. 备份策略

低流量测试期每 7 天一次，正常保留最近 4 份 `pg_dump -Fc`、SHA-256 和北京副本；最后一份已验证恢复点与未验证文件受保护，必要时会暂时超过 4 份。正式生产前改为每日 7 份加每周 4 份，每月恢复到临时库并记录 RPO/RTO。未完成复制和恢复校验时，不得把备份状态标为 verified。

日本服务器生成备份：

```sh
DATABASE_URL="$DATABASE_URL" AUDIT_DATABASE_URL="$DATABASE_URL" \
  BACKUP_DIR=/srv/backups/self-deepsearch \
  RETENTION_COUNT=4 sh infra/backup/weekly_backup.sh
```

脚本默认 `RETENTION_COUNT=4`，只接受 1～100 的整数字面值（不接受空值、0 或前导零）。新 `.dump`、SHA-256 sidecar 和 `audit.backup_runs=completed` 全部成功后，才生成完整清理计划。按受控 UTC 文件名排序，保留最新 N 组、最新一份本地文件完好且有复制/恢复验证证据的恢复点，以及所有未验证、证据不完整或校验失败的文件；时钟回拨时也保留本次新文件。只有更旧且验证证据匹配的文件对才会被清理。

清理不仅检查文件名和 sidecar，还通过只读 `audit_backup.sh retention-check` 比对数据库中的 weekly、日本→北京、verified 状态、绝对文件路径、实际字节、SHA-256 和复制/恢复证据。生成计划时任一 SQL 失败，不执行任何旧文件删除；删除前再次核对本地文件。无关文件、符号链接、遗留 `.partial` 和不符合命名格式的文件不参与轮转，也不计入下列数量。日本脚本只轮转日本目录，不会自动删除北京副本；北京清理应单独确认对应源记录和恢复证据。

输出增加 `retention_status`、`retention_removed_count`、`retention_extra_count`。受保护文件使数量超过 N 时，状态为 `deferred` 并输出警告；备份本身成功仍返回 0，以便继续复制。`retention_extra_count` 是受控命名文件中超出目标的保护份数，不是整个目录的文件或空间统计。SQL/文件校验错误则返回非零；新备份已 completed 后，清理失败不会把它改记为 failed。

如需稳定维持 4 份，待清理文件必须先补齐北京复制和空临时库恢复验证；只每月抽验时，未验证文件可能持续积累。收到 `deferred` 应安排补验或人工排查，不要直接按年龄硬删。磁盘预算不足时，先暂停新增备份调度并告警，保留已有恢复点；不能把本脚本当成磁盘配额控制器。

备份目录必须由专用运行账号控制，禁止其他任务修改其中的文件；所用 Linux 工具需要支持 `ln -T`，并允许同文件系统硬链接。脚本通过私有 `.weekly-backup.lock` 目录排除并发：已有锁时返回 75，不自动抢锁；同秒名称已有 dump/sidecar（包括链接或目录）时返回 73，且不访问数据库、不覆盖原文件。正常退出会清理自己的暂存与锁；强制终止或主机掉电后，管理员应先确认没有备份进程或调度正在运行，核对目录绝对路径和锁内残留，再处理遗留锁后重试。不要根据锁的年龄自动删除它。

命令输出 `backup_run_id`、文件、字节数和 SHA-256，此时状态只能是 `completed`。把 `.dump` 和 `.dump.sha256` 复制到北京后，在北京运行下列命令；它只读文件，不需要数据库凭据：

```sh
BACKUP_FILE=/srv/backups/self-deepsearch/self-deepsearch-YYYYMMDDTHHMMSSZ.dump \
  SOURCE_SHA256='<日本输出的 sha256>' \
  DESTINATION_REFERENCE='beijing:/srv/backups/self-deepsearch/self-deepsearch-YYYYMMDDTHHMMSSZ.dump' \
  sh infra/backup/verify_copy.sh
```

将北京输出的 `destination_reference`、`observed_bytes` 和 `observed_sha256` 带回日本，在日本登记复制证据。不要把日本数据库凭据放到北京：

```sh
AUDIT_DATABASE_URL="$DATABASE_URL" BACKUP_RUN_ID='<日本输出的 backup_run_id>' \
  DESTINATION_REFERENCE='<北京输出的 destination_reference>' \
  OBSERVED_BYTES='<北京输出的 observed_bytes>' \
  OBSERVED_SHA256='<北京输出的 observed_sha256>' \
  sh infra/backup/record_copy.sh
```

每月先创建一个全新的空临时数据库，再执行恢复验证：

```sh
BACKUP_FILE=/srv/backups/self-deepsearch/self-deepsearch-YYYYMMDDTHHMMSSZ.dump \
  BACKUP_RUN_ID='<backup_run_id>' AUDIT_DATABASE_URL="$DATABASE_URL" \
  VERIFY_DATABASE_URL='postgres://.../self_deepsearch_restore_test?sslmode=require' \
  sh infra/backup/verify_restore.sh
```

恢复脚本要求 sidecar 哈希存在，先确认目标库没有用户对象，再恢复并检查 `collector`、`platform`、`audit` 和 Schema 版本；它不会 drop/clean 目标库。只有数据库中已经有匹配的北京复制证据，且恢复文件 SHA-256 与原记录一致，状态才从 `completed` 变为 `verified`。任何步骤失败时，“从未验证备份”或“备份过旧”告警应继续触发。

### 8.1 浏览历史归档

日本 Worker 可启用 `HISTORY_ARCHIVE_ENABLED=true`，将用户已清除、且 `deleted_at` 早于 `HISTORY_ARCHIVE_MIN_AGE` 的历史按批次写成 gzip JSONL。默认生产示例为关闭；确认归档目录已纳入北京副本与恢复演练后再开启。`HISTORY_ARCHIVE_INTERVAL` 控制扫描间隔，`HISTORY_ARCHIVE_BATCH_SIZE` 最大 10000，目录由 `HISTORY_ARCHIVE_DIR` 指定。

每个文件以 0600 权限原子写入，文件名包含首尾 history ID 与完整 SHA-256；同一事务写入 `audit.archived_history_manifests` 并设置原行 `archived_at`。文件包含用户 UUID、内容类型/UUID、`viewed_at` 和 `deleted_at`，不包含邮箱、IP 或 User-Agent。Release A 不删除 `platform.view_history` 原行；任何物理清理都必须另行确认保留政策、恢复工具和对账证据。

联调时至少验证：未清除记录不归档、未达到门槛不归档、重复运行不重复归档、gzip 可解压、SHA-256/字节数/条数/时间范围与 manifest 一致、数据库提交失败时行不会被标为已归档。Schema v7 为 `platform_worker` 增加 `view_history` 的 SELECT/UPDATE 权限，但不授予用户邮箱表读取权限。

## 9. 明确关闭

- `COLLECTION_ENABLED=false`；
- 不配置真实来源域名、解析规则或采集计划；
- 不加载广告联盟脚本，页面广告位仅为布局占位；
- 不启用头像/人脸识别；
- 不提供任何资源链接、下载或转发能力。

## 10. 监控与告警

API 和日本 Worker 的 `/metrics` 使用 Bearer 令牌，未配置服务端令牌时返回 404，令牌错误返回 401。指标标签只包含服务、版本、区域、HTTP 方法、规范路由模板和状态码类别，不包含邮箱、用户 ID、IP、查询词、真实 slug、User-Agent 或内容 ID。

当前规则覆盖 API/日本 Worker 不可达、Worker 心跳过期或连续 3 次 poll 失败、数据库不可用、持续 5xx、邮件额度/熔断、outbox 死信与积压、8 天无已验证备份、Schema 版本不为 21、媒体对账从未成功/超过 2 天/发现问题，以及未物理删除媒体达到 10 GiB 预算的 70%/85%/95% 分级状态。Prometheus 会把告警发送到 Alertmanager；生产模板将 critical 每 30 分钟重复通知，warning/info 默认每 4 小时重复，并在恢复时通知。测试期可从 `http://127.0.0.1:9090/alerts` 和 `http://127.0.0.1:9093` 查看状态。备份只有在跨区复制和恢复校验均留痕后才能在 `audit.backup_runs` 标为 `verified`，否则“从未验证备份”告警必须保持触发。admin/owner 还可在运营概览查看 outbox、媒体字节、已验证备份和媒体对账状态；editor 不读取这些运维指标。

日本模板启动监控：

```sh
cp infra/compose/japan/.env.example infra/compose/japan/.env
# 把 Alertmanager 模板复制到 .env 指定的服务器私有路径，替换全部 .invalid 值；
# SMTP 密码只写入独立密码文件。随后检查私有文件和配置语法：
python3 scripts/release_a_env_check.py --japan-env infra/compose/japan/.env --mode core --check-files
docker compose --env-file infra/compose/japan/.env -f infra/compose/japan/compose.yaml --profile monitoring \
  run --rm --no-deps --entrypoint /bin/amtool alertmanager check-config /etc/alertmanager/alertmanager.yml
docker compose --env-file infra/compose/japan/.env -f infra/compose/japan/compose.yaml --profile monitoring config --quiet
docker compose --env-file infra/compose/japan/.env -f infra/compose/japan/compose.yaml --profile monitoring up -d
```

启动后必须制造一条可识别的 warning 测试告警和一条 resolved 通知，确认值守邮箱实际收到、主题和严重度正确，再删除测试告警。仅看到 Prometheus 规则为 firing 或 Alertmanager 页面有记录不等于通知链路已验收。
