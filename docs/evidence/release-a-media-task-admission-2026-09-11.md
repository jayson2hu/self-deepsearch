# Release A 定时对账用量准入验证

日期：2026-09-11（Asia/Shanghai）。本轮完成定时全量扫描的实际准入接入和本地回归；**完整额度控制、真实依赖及 Release A/M5 整体验收未完成，公网仍 no-go。**

没有启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 或真实 Analytics，没有触发远程 CI、暂存或提交。Go→Python HTTP 使用真实本地签名/报告/删除代码和临时文件存储替身，不代表供应商或数据库验收。前端未修改，本轮没有前端构建/浏览器/性能新证据。

## 本轮交付

- `MEDIA_RECONCILE_USAGE_GUARD=off|enforce`，默认 off，仅日本 Worker 注入；enforce 必须配合 observe 和对账启用，错误依赖拒绝启动/环境检查。
- `mediausage.TaskGuard` 读取已有状态与配置摘要，不写状态、不查询 Analytics，不缓存放行；读取有 2 秒上限，读取后取时钟，按整数高水位重算阈值。高/预留、复核/停用、过期/未来/缺失、读失败/取消、配置变化均拒绝。
- 对账 Runner 在库存准备前/跨区 HTTP 前各检查一次。第一处拒绝不创建任务；第二处保留 failed/usage_guard_denied，不发 HTTP、不能伪造 completed。只有此失败码可重新评估而不消耗日间隔，普通失败保持原间隔。
- Enforce 时每分钟本地检查，到期/并发领取仍交由原 PostgreSQL 事务；off 保留原调度频率。新增私有有界指标及 5 分钟 warning，不阻断权利删除/缓存 outbox/默认图检查/账号/邮件。
- 无新增 Schema、API、服务、连接池或令牌分发。Schema 仍 v20、60 表/5 公开视图；OpenAPI 仍为 85 操作/74 路径/100 组件 Schema。

设计取舍、操作规则和回退风险见[准入手册](../MEDIA_TASK_ADMISSION.md)。

## 验证结果

| 验证 | 结果与证明范围 |
| --- | --- |
| Go API/Worker | 全包 vet/test 通过；Worker 二进制重建成功，只编译未启动 |
| 准入专项 | 4 组、30 个子场景通过：整数阈值/溢出/自定义预留、复核/停止、缺失/旧/未来时间、账期、取消、读取后时钟、无缓存、并发指标、真实仓储方法只读及提交失败拒绝 |
| 调度/配置/私有指标 | 前置拒绝、发送前拒绝、拒绝回执失败、到期与延期后再评估、默认 off/依赖校验、保护指标不改变健康检查通过 |
| 实际 Go→Python HTTP | 扩展既有合同：缺失状态时对账 HTTP 调用数不增加且不准备库存；有效低风险允许实际 Python 对账；高水位 85% 即使低风险标签仍拦截；此时实际本地签名删除仍成功 |
| 真实 SQL | 扩展 `TestPostgresMediaUsageStateContract` 与 `TestPostgresReconciliationScheduleContract`，覆盖受限状态/重启/配置及并发领取、拒绝不消耗日间隔、普通失败仍限频与原证据保留。本机均明确 SKIP，未算 SQL 通过 |
| Python | 最终 **228 passed、141 subtests passed、1 skipped，82.58 秒**；跳过为本机符号链接权限相关测试 |
| 静态检查 | 全范围 ruff、15 个生产源文件 mypy、OpenAPI TypeScript 漂移通过；Compose 的新开关隔离和默认值经 YAML 合同验证，不冒充 Docker 渲染/启动 |
| 文档 | 进度、架构、扫描手册、观察/复核、准备清单和就绪记录同步当前已接/未接边界；14 份文档的 155 个本地链接全部存在；历史证据保留原适用范围 |

## 发现与修正

1. 扩展 SQL 合同时，与已有数据库连接变量重名导致 Go 编译失败。改为 `taskGuard` 后，全包 Go 回归通过。
2. 新指标字段让 gofmt 改变结构体对齐空格，旧 Python 静态合同依赖一个固定空格而失败。改为匹配同一个 Japan 认证条件、允许格式空白变化；7 项专项及最终全量通过，没有放宽真实 readiness 条件。
3. 同步文档时发现 README 将观察状态误标为迁移 20，已按 SQL 修正：观察是迁移 19，复核/切换回执是迁移 20。
4. 一次多文档 patch 因不存在的标题校验失败，未产生部分修改；重新读取实际锚点后完成修改。

## 可复跑命令

从项目根目录运行；不要设置真实数据库契约环境变量。每条命令需独立核对退出码。依赖使用本地已安装环境，不要求 Docker。

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-worker/internal/mediausage -run 'TestTask' -count=1 -v
go build -o .cache/core-e2e/platform-worker.exe ./services/platform-worker/cmd/worker
go test ./services/platform-worker/internal/mediausage ./services/platform-worker/internal/mediareconcile -run '^TestPostgres(MediaUsageState|ReconciliationSchedule)Contract$' -count=1 -v
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q
.venv/Scripts/ruff.exe check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/Scripts/mypy.exe workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
```

## 未完成边界

- 仅控制日本 Worker 新发起的定时全量对账。已发送请求、手工 CLI 或其他签名调用者不受这个发起点原子撤销；真实供应商计量可能延迟，不能保证总费用不超。
- 上传动态准入、API/Next 运行中停图、全部图片源站/直连 URL、Cloudflare 旧缓存、授权恢复和实际执行回执仍待开发/验证。
- 真实 PostgreSQL 与两地链路/SDK、Analytics 账户权限/账期/存储 GB-month、新旧告警送达、2C4G/2C8G 资源验收仍待完成。
- 所有日本实例须一致升级；回退或 off 会移除扫描准入，需先暂停自动对账，不能把 off 当作安全恢复。自动采集/广告联盟仍为 Release B/C，未启用。
