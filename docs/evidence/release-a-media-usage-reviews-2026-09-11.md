# Release A 用量复核与账期切换验证

日期：2026-09-11（Asia/Shanghai）。结论：**本轮本地功能回归通过，真实数据库/供应商验收未完成；Release A/M5 仍未完成，公网 no-go。**

本轮没有启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 或真实 Analytics，没有提交代码或触发远程 CI。浏览器/HTTP 使用临时回环进程及合成数据；Go→Python 合同运行真实本地 HTTP，但其存储供应商为替身。测试进程已结束。

## 实现范围

- Schema v20，60 张表、5 个公开视图；新 `audit.media_usage_reviews` 保存不可变请求、处理回执和前后状态，状态 `last_review_id` 绑定回执。
- `GET /admin/v1/media/usage` 与 `POST /admin/v1/media/usage/reviews`：owner/admin、近期密码、显式确认、CAS、UUID 幂等请求和原子请求审计。202 为 pending，相同请求重放为 200；接口始终标注 `enforcement: not_connected`。
- 日本现有 Worker 校验五分钟授权、角色、原状态及当前配置；异常复核不降低计数，账期切换保留旧证据并继续 hold，不能直接恢复媒体。观察器默认关闭，不增加服务或连接池。
- 后台 `/media/usage`，角色隔离、有效期/状态显示、明确账期、不确定结果同键重试、手动刷新。四组新场景在桌面/移动共 8 项，加入原有全量回归。
- 当前 OpenAPI 为 85 个操作、74 个路径、100 个组件 Schema；19 个高风险操作要求近期密码。生成类型由脚本产生，未手改。

详细约束、替代方案与升级见[用量复核手册](../MEDIA_USAGE_REVIEWS.md)。

## 最终结果

| 范围 | 本轮执行结果与边界 |
| --- | --- |
| Go API/Worker | 全包 `go vet` 和 `go test -count=1` 通过，显式设置 `MEDIA_CONTRACT_PYTHON`；两个二进制重建成功，未启动真实 API/Worker 服务 |
| 新数据库契约 | `TestPostgresMediaUsageAPIRepositoryContract`、`TestPostgresMediaUsageStateContract`、`TestPostgresMediaUsageReviewContract` 均因未配置专用数据库明确 SKIP；不计为 SQL 通过 |
| Python | 最终完整串行回归 **225 passed、138 subtests passed、1 skipped，80.76 秒**；跳过项为本机符号链接权限相关测试 |
| 静态检查 | 全范围 ruff、15 个生产源文件 mypy、OpenAPI TypeScript 漂移检查通过 |
| 前端 | 两站 typecheck/lint 通过；运营站生产构建通过并包含新路由；公开站沿用原制品，本轮未重新构建 |
| 浏览器 | 最终完整 **54/54 passed，3.8 分钟**；初始新增专项 8/8 通过后仍修正了视觉核对发现的旧提示，最终全量覆盖修正版本 |
| 工具与 HTTP | 15 项前端烟测、56 项工具合同、两站 standalone HTTP 冒烟通过；后者验证首页/登录注册、页脚/18+、占位广告边界、JSON-LD、安全头/CSS 和代理 503/no-store |
| 文档 | 架构、数据模型、进度、就绪审计与运维手册同步 Schema v20/当前实现边界；检查 19 份文档的 154 个本地链接全部存在，随后增加的链接均指向已核对的本轮证据/复核手册 |
| 未重跑的历史检查 | 公开站构建、生产依赖 registry 审计、LCP/CLS/HTTP P95 性能探针、Go race、容器/真实源站检查不属于本轮新结果 |

## 本轮发现并修正

1. API 原始 `SELECT ... FOR UPDATE` 与只读状态表权限冲突。改为固定 search_path 的 SECURITY DEFINER 锁函数，仅返回单例的四个 CAS 字段、仅 API 可执行，不授予状态 UPDATE。新增真实仓储 SQL 契约直接调用生产方法，覆盖 NOWAIT 锁竞争、最小权限、未初始化、CAS、重放/改载荷/pending 冲突和唯一请求审计。测试已加入现有 CI 配置，本机未执行真实 SQL。
2. OpenAPI 3.1 中误用 `nullable`，生成类型未含 null。改为 3.1 的 null 联合/anyOf，并重新生成；显示日期显式处理 null。
3. 初次 React lint 拒绝 effect 中同步 setState 链。初始加载改用异步回调及卸载保护；手动刷新保留在事件处理器。
4. 路由合同中的旧 83 操作计数未同步，导致 Python 失败。更新三处为 85 并加入精确近期认证集合，最终全量通过。
5. 通用时间格式合同误命中计数 `toLocaleString`；计数改用 `Intl.NumberFormat`/BigInt，时间仍走统一北京时间到秒格式器。
6. 截图核对发现状态刷新后仍显示旧“尚未处理”。手动刷新清除旧提示，提交后的即时刷新保留排队消息；浏览器补断言并重新跑完 54 项。

这些失败均已记录，未将首次失败描述为通过。

## 视觉证据

最终浏览器生成的截图已从临时 test-results 复制到本目录的固定子目录，后续测试不会覆盖本轮证据。已目视核对手机待处理、桌面切换后保持保护：没有横向溢出；注入样本文字按文本显示；pending 与 applied 明确区分，applied 后旧待处理提示消失；无有效统计时不将 0 计数当作已验证用量。

- [手机：待处理](./release-a-media-usage-reviews-2026-09-11/mobile-pending.png)
- [桌面：待处理](./release-a-media-usage-reviews-2026-09-11/desktop-pending.png)
- [手机：切换后仍保护](./release-a-media-usage-reviews-2026-09-11/mobile-period-held.png)
- [桌面：切换后仍保护](./release-a-media-usage-reviews-2026-09-11/desktop-period-held.png)

## 复跑命令（不自动连接真实服务）

在项目根目录、未设置真实 SQL 契约环境变量的终端运行。依赖已安装，Go 使用本地缓存；每条命令须独立确认退出码。

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go build -o .cache/core-e2e/platform-api.exe ./services/platform-api/cmd/api
go build -o .cache/core-e2e/platform-worker.exe ./services/platform-worker/cmd/worker
go test ./services/platform-api/internal/database ./services/platform-worker/internal/mediausage -run '^TestPostgresMediaUsage.*Contract$' -count=1 -v
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q
.venv/Scripts/ruff.exe check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/Scripts/mypy.exe workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
npm run typecheck
npm run lint
npm run build --workspace @self-deepsearch/ops-web
npm run test:frontend:smoke
npm run test:preflight
npm run smoke:frontend:release-a
npm run test:e2e:release-a
```

## 仍需完成

- PostgreSQL 16 v1-v20 fresh/upgrade/down、受限 API 实际仓储调用、Worker 状态转换/延迟约束及角色并发变更；缺数据库 SKIP 不算通过。
- 实际账户/账期/Analytics 权限、延迟和存储 GB-month 口径；不能将 A/B 操作分析当账单或零费用保证。
- 动态策略下发、非必要任务限流、北京/日本实际停图/恢复与执行回执、所有图片源站及 Cloudflare 缓存/直连入口控制。
- 真实邮件、存储、图片删除、跨区副本和目标 2C4G/2C8G 部署资源验收；观察/复核不扩展本轮范围到自动采集或广告联盟。
