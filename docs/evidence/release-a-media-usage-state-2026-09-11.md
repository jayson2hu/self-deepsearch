# Release A：账户用量持久观察验证

日期：2026-09-11（Asia/Shanghai）。本地实现与离线回归通过；**完整额度保护、M5 和 Release A 尚未完成，公网 no-go**。本轮没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare、真实 Analytics 查询或远程 CI；没有暂存/提交代码。

## 已交付范围

- [迁移 19](../../db/migrations/00019_media_usage_observation.sql) 新增 `platform.media_usage_state`、`audit.media_usage_runs`，当前 59 张表、5 个公开视图；引擎仍 PostgreSQL 16。
- [状态规则](../../services/platform-worker/internal/mediausage/state.go) 绑定账户摘要、明确账期与策略摘要，保存有效观察高水位和首次复核证据。失败、倒退、过期、中断保留旧证据；后来有效或较低风险结果不自动清除复核/停用建议。
- [PostgreSQL 仓储](../../services/platform-worker/internal/mediausage/postgres.go) 用 READ COMMITTED/advisory lock 及锁后新读领取两分钟租约，提交后才发 HTTP。完成校验配置/run/window 与本机及数据库时钟，原子提交审计/状态/释放租约；丢失活动审计时拒绝清除状态。
- [定时器](../../services/platform-worker/internal/mediausage/runner.go) 复用日本 Worker，默认 off；明确 observe 后以 5～15 分钟周期查询，默认 15 分钟。分钟 tick 只检查持久调度，HTTP 不占数据库连接；不增加常驻服务或数据库连接池。
- [私有指标](../../services/platform-worker/internal/mediausage/metrics.go) 经原 Bearer 入口、两秒只读期限导出；读取时重新判定过期，不把缺失状态当零用量。三条告警区分不可读、复核/hold 和停用建议，**不声称已经执行停止**。
- API readiness、seed、受限 SQL 合同、监控、CI、数据模型、部署/升级手册和前端合成 fixture 的版本门槛同步 v19。只将 Analytics 配置注入日本 Worker，不给北京、API、前端或 Python。

运行配置、架构取舍与升级细节见[持久观察手册](../MEDIA_USAGE_STATE.md)。沿用现有 Worker/PG 是为了控制日本 2C4G 的服务数量与恢复复杂度，不代表已测得目标资源占用。

## 本地执行结果

| 检查 | 结果与边界 |
| --- | --- |
| Go API/Worker | 最终 `go vet`、`go test -count=1` 通过；显式设置 `MEDIA_CONTRACT_PYTHON`，包含真实本地 Go→Python HTTP 合同。真实 SQL 环境未配置，相关合同 SKIP |
| `mediausage` 整包 | JSON 事件核对 **31 个顶层测试 + 121 个子测试通过**；包含此前查询逻辑和本轮状态/仓储替身/定时器/指标测试；`TestPostgresMediaUsageStateContract` 明确 SKIP |
| 覆盖率 | **91.8% statements**，带引号的绝对 `-coverprofile` 路径已由 `go tool cover` 读取；不能将仓储替身覆盖当真实 PostgreSQL 执行证明 |
| Go 构建 | API 与 Worker 已重新编译至 `.cache/core-e2e/`；Worker `media-usage --help` 退出 0，未授权调用退出预期 2；没有启动常驻 API/Worker，没有执行 `--live` |
| 全量 Python | **222 passed、136 subtests passed、1 skipped，82.52 秒**；跳过项为本机账户不能创建 symlink 的上传锁测试，不是失败 |
| 文档同步后定向 Python 回归 | `db/tests infra/security infra/reverse-proxy scripts/tests`：**162 passed、103 subtests passed，18.41 秒**；包含数据模型数量、迁移静态约束、配置与权限/CI 合同 |
| Python 静态检查 | 全范围 ruff 通过；collector/media 和六个发布脚本的 mypy **15 个源文件通过** |
| OpenAPI | TypeScript 生成漂移检查通过；本轮未增加公开/后台 API 路径 |
| 格式与文档 | 相关 Go 文件 `gofmt -l` 无输出；本轮同步的 18 份文档中 142 个本地链接全部有效，0 个缺失目标 |
| 前端单元与工具 | **15 项前端烟测、56 项工具合同通过** |
| standalone HTTP | 两站通过首页、账号入口、页脚/18+、广告占位/默认图、三类 JSON-LD/XSS、安全头/CSS 与上游不可用 503/no-store 检查；使用已有且本轮未改变的 Next 构建及合成 mock，测试进程已结束 |
| 浏览器/构建边界 | 本轮未改 UI、未重建 Next、未重跑 Playwright/视觉/性能/Go→Next 缓存专项；此前 46 项浏览器证据保留原轮次，不计为本轮执行 |
| SQL/部署/供应商/race | 未执行真实 SQL、Linux 卷/目标资源、镜像运行、供应商或 Go race 验证；不做上线成功声明 |

配置测试先发现旧规则要求所有模板占位项都必须失败，与观察器 off 时允许空账户/账期相冲突。修复只豁免 Japan/off 的四个观察配置项，并新增 observe 模式逐项缺失拒绝测试；其他生产必填项保持原校验。针对失败/租约丢失/配置变更的测试同时防止错误原文或令牌写入日志。

## 可复跑命令

以下为本地、无需 Docker 的验证命令。执行时保持真实依赖测试环境变量未配置，不通过填入业务数据库来消除 SKIP。

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
$env:PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'

go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-worker/internal/mediausage -count=1 '-coverprofile=C:/Users/jayso/Documents/self-deepsearch/.cache/media-usage-state-coverage.out'
go tool cover '-func=C:/Users/jayso/Documents/self-deepsearch/.cache/media-usage-state-coverage.out'
go build -o .cache/core-e2e/platform-api.exe ./services/platform-api/cmd/api
go build -o .cache/core-e2e/platform-worker.exe ./services/platform-worker/cmd/worker
& ./.cache/core-e2e/platform-worker.exe media-usage --help
# 下一条预期退出 2，表示缺少显式授权，不是供应商验证失败：
& ./.cache/core-e2e/platform-worker.exe media-usage

./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
./.venv/Scripts/python.exe -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
./.venv/Scripts/python.exe -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
npm run check:api-types
npm run test:frontend:smoke
npm run test:preflight
npm run smoke:frontend:release-a
```

## 待执行 PostgreSQL 合同

[真实合同](../../services/platform-worker/internal/mediausage/postgres_contract_test.go) 已加入 CI 串行链：outbox → 文件对账 → 每日检查 → 用量状态。本机只执行了入口守卫及 SKIP，不创建数据库、不启动服务。

需一次性回环 `self_deepsearch_worker_test`、Schema v19、受限 `platform_worker_login` 与同一主机/端口 owner guard，显式 `CONFIRM_MEDIA_USAGE_CONTRACT=disposable-database`。拒绝既有业务数据；不以 DELETE/停用触发器恢复业务观察状态。合同覆盖两个真实 advisory-lock 等待者竞争、仅一方领取、失败保留、中断、过期租约拒绝、配置变化、完成审计不可变以及 API/公开读者/Worker 权限。

## 仍未完成

持久观察只回答“最近看到什么、是否建议复核/停止”，不回答“图片入口是否已被封住”。尚需授权复核/恢复与账期轮换、可信存储 GB-month/账户口径、动态跨区策略、非必要作业限流、API/Next/北京执行回执、所有源站入口与边缘缓存控制，以及真实业务生命周期验证。

Analytics 不是账单；5% 预留并非经过真实吞吐/延迟验证的充足储备。提供凭据不能替代剩余开发，当前不能保证零费用。Release B 自动采集、Release C 联盟广告、头像识别仍不启用。
