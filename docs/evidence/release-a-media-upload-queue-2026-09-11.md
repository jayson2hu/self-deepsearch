# Release A 上传控制授权队列验证

日期：2026-09-11（Asia/Shanghai）。范围：Go API/Worker、Python 私有控制协议、Schema v21、合同和运维文档。未启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 或 Analytics；没有部署、执行远程 CI、暂存或提交。目标仍未完成。

## 已执行

| 检查 | 结果与边界 |
| --- | --- |
| Go API/Worker 全量 vet/test | 通过；设置 `MEDIA_CONTRACT_PYTHON`，含真实本地 Go→Python HTTP，DB/SDK/供应商仍为替身 |
| Go API/Worker build | 两份二进制重建成功，只编译未启动 |
| 上传控制模块 `-cover` | 通过，86.9%；包含协议、CLI、队列、事务失败、回执和指标；真实 PostgreSQL 合同跳过 |
| 完整 Python pytest | 255 passed、204 subtests passed、1 skipped，107.20 秒；1 项符号链接权限测试在本机跳过 |
| Python ruff / mypy | 全范围 ruff 通过；17 个生产源文件 mypy 通过 |
| OpenAPI 生成漂移 | `--check` 通过；87 操作/76 路径/106 组件 Schema，20 项近期认证操作 |
| 两站/合同 TypeScript、两站 ESLint | 通过；没有上传控制 UI 实现 |
| 工具/前端单元 | 56/56 工具合同，15/15 前端单元通过 |
| 文档收尾后回归 | DB/安全/环境定向检查 132 passed、108 subtests；OpenAPI 类型仍一致；23 份文档的 204 个本地文件链接均存在（不验证页内锚点） |
| 新增实际 API/Worker SQL 合同 | 两项均明确 SKIP，未配置测试库；不等同于迁移、权限或约束通过 |

## 可复跑命令

仓库根目录 PowerShell；使用已安装离线依赖，不下载模块：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go build -o .cache/platform-worker.exe ./services/platform-worker/cmd/worker
go build -o .cache/platform-api.exe ./services/platform-api/cmd/api
go test ./services/platform-worker/internal/mediauploadcontrol -count=1 -cover

$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q
.venv/Scripts/python.exe -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/Scripts/python.exe -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
npm run typecheck
npm run lint
npm run test:preflight
npm run test:frontend:smoke
```

本机明确运行并观察到 SKIP：

```powershell
go test ./services/platform-api/internal/database -run '^TestPostgresMediaUploadAPIRepositoryContract$' -count=1 -v
go test ./services/platform-worker/internal/mediauploadcontrol -run '^TestPostgresMediaUploadQueueContract$' -count=1 -v
```

专用库/受限角色/显式确认条件及后续执行方式见[队列手册](../MEDIA_UPLOAD_QUEUE.md)。CI 配置已增加确认变量和串行执行项，但本轮未触发远程 CI。

## 重点验证与修正

- API：匿名/普通用户/editor 拒绝，近期认证与同源要求、空/null/非整数 generation、无确认、初始恢复、异常字段、无状态、依赖失败、202 与幂等 200、私有 no-store 和超时。
- 事务：CAS 精确到微秒，actor 锁与状态锁、同键不同载荷冲突、复用原请求不读取新快照、请求/审计原子提交，begin/查询/写入/commit 失败不返回虚假成功。
- 队列：派发记录在 HTTP 前持久化；真正丢失远端成功回执后，下次签名状态确认 applied，apply 次数仍为 1；旧租约、过期、权限撤销、状态变化、用量准入失败均不发不安全恢复。
- 协议：有效期不超过五分钟、严格 UTC 秒，临近失效/过期不继续正常派发；已派发的未变快照保留时差确认窗口。北京检查最新时钟，不沿用早先 HTTP 认证时间；精确重放只确认原结果。
- 修正首次状态初始化被略快的应用时钟阻止领取的问题；补入远端状态回退保护、回执结构检查、数据库派发时角色/期限二次防线。
- 首轮检查发现数据模型/CI/OpenAPI 数量断言仍为旧版，已同步；新增测试中 lambda lint、健康 fixture 缺 capabilities、连接池静态断言与实际变量不符已修正并复验。SQL 触发器分支在检查中修正，但未由实际 PostgreSQL 执行证明。

## 不代表什么

没有运行真实 PG fresh/upgrade/down、真实 SMTP/对象存储/Analytics、Linux 卷权限/断电、公网传输或 2C4G 资源测试；Windows 单元和本机 HTTP 不能替代这些验证。没有 Next 构建、浏览器 E2E、standalone HTTP、性能或新的联网依赖审计，历史结果保留原轮次适用范围。

后台上传控制 UI、自动额度下发、API/Next 动态停图、账单/存储计量和源站/边缘全入口限制尚未完成。此轮是后台授权执行闭环的离线实现，不是免费账户零费用保证，不关闭 Release A/M5 或公网 no-go。
