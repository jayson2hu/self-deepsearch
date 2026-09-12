# Release A 图片对账确认与调度复核

日期：2026-09-10（Asia/Shanghai）。范围：Go Worker 对账、Python 报告、调度快照与 CI 合同。Schema 仍为 v17，无公开 API 或前端改动。

## 复现与修复

1. **不完整报告被记为干净成功，已复现。** 本机 HTTP 返回只有 `run_id`、`expected_count`、`issue_samples:{}` 的 JSON；旧 Worker 把六个缺失计数当作 0，调用 Complete，未记录失败。新增测试先失败：`completed=&{... 1 0 0 0 0 0 0 map[]} failed=""`。另外 13 个 HTTP 场景中，202、错误 Content-Type、尾随 JSON/垃圾、超限、非法 UTF-8、缺失/null/重复计数、null 样本等 11 个负向场景在旧实现中被错误接受。
2. **现在严格确认完整报告。** 要求 HTTP 200、application/json、UTF-8、完整且不超过 64 KiB 的 JSON；九个顶层字段必须恰好各出现一次，计数不可 null；六类样本数组必须存在，拒绝未知/重复类别。核对 run ID、对象数、非负/int32 范围、每个存储内缺失+损坏不超过清单数、样本不超过总数或 50 条、样本路径合法且对应正确的预期/孤儿集合。总异常数使用 int64，避免相加溢出。无效报告记录 `invalid_report`，真实服务/HTTP 失败记录 `reconcile_unavailable`，都不能 Complete。
3. **合法长路径报告也必须可接收。** Python 每类样本最多 50 条且 JSON 转义后不超过 8 KiB；六类总样本预算 48 KiB，给结构/计数留余量。只截断样本，不改变异常总数，不伪造或缩短对象标识。Unicode/长路径测试保留 160 个孤儿总数，并保证 HTTP 体小于 64 KiB。
4. **调度锁后的旧快照风险，代码已修正，真实 SQL 待验。** 原 Repeatable Read 在等待 advisory lock 前可能已经取得快照；锁返回后仍看不到另一调度器刚提交的运行，造成同周期重复扫描。改为 Read Committed，先获得事务级调度锁，再用新语句读最近运行；库存自身仍由一条 SELECT 提供一致快照。明确关闭库存游标后再写计数/提交。

代码：[报告校验](../../services/platform-worker/internal/mediareconcile/report.go)、[调度准备](../../services/platform-worker/internal/mediareconcile/postgres.go)、[Python 对账](../../workers/media-python/media_worker/server.py)。

## 本地证据

- Go API/Worker 全量 `go vet`、`go test -count=1` 通过，Worker 二进制已重新编译，未启动部署服务。
- 新负向报告测试 red→green，涵盖 13 个 HTTP 场景、12 个语义异常、缺字段不误报完成、空库存与异常数溢出边界；库存替身测试核对拿锁后读状态、not-due 不建任务、失败回滚、超 10,000 对象记 failed。
- **真实 Go→Python HTTP** 通过：当前公开图与保留私有母版均在预期清单中；检查服务故障不能记成功；保留母版丢失产生 S3/备份缺失；孤儿两侧均被检测；调用真实 Go 删除发送器和 Python 删除服务清除临时文件后，用模拟的已确认数据库清单重新对账干净。只有对象存储/purge 使用隔离临时文件，本合同不证明 PostgreSQL、真实 S3 或 Cloudflare 已验收。
- 新 PostgreSQL 合同用受限 worker 登录、固定回环专用库、同库 admin 只读前置守卫和显式确认。实际观察两个连接都在 advisory lock 上等待后才释放，要求一个成功、一个 ErrNotDue，且仅一条持久运行。CI 配置在 outbox 合同后串行执行；本机缺专用库，明确 **SKIP**，未运行或触发远程 CI。
- Python 服务专项 9 项、CI 安全合同 13 项、相关 ruff、媒体生产代码 mypy、OpenAPI 漂移通过。最终完整 Python 结果在下方更新。

### 最终完整回归

- 串行全量 Python：**184 passed、88 subtests passed，82.10 秒**。
- Go API/Worker vet/test、实际 Go→Python 删除及对账 HTTP 合同均通过，Worker 已编译；新 PostgreSQL 调度合同明确 SKIP，不计入实际 SQL 通过数。
- 相关 ruff、媒体 Python 五个生产文件 mypy、OpenAPI 漂移检查通过。本轮未修改前端，未重新构建或执行浏览器套件，上一轮 42 项证据保留历史含义。
- 本轮更新的八份 Markdown 共 80 个本地链接检查通过，无断链。

## 运行与升级边界

先部署兼容的北京媒体服务，再部署日本 Worker。正常 Python 响应原本就具备全部字段；新版本限制长路径样本体积，Worker 拒绝不完整旧替身响应。没有数据库迁移，仍是 Schema v17。

本机跨语言专项命令：

```powershell
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go test ./services/platform-worker/internal/mediareconcile -run TestReconciliationPythonHTTPRetainedMasterAndRecovery -v -count=1
```

SQL 合同只接受 `self_deepsearch_worker_test` 专用库，并要求 `MEDIA_RECONCILE_CONTRACT_DATABASE_URL`、`MEDIA_RECONCILE_CONTRACT_ADMIN_DATABASE_URL` 指向相同回环端口，`CONFIRM_MEDIA_RECONCILE_CONTRACT=disposable-database`。admin 仅用于核对空库/Schema 和测试互斥，不用于 repository 调度。禁止与 outbox 合同并行，不更改真实数据库调度记录。

## 与完整目标的差距

- 当前库存/容量 SQL 仍包含所有未物理删除对象，旧图隐藏不会提前释放配额；保留母版仍按应存在对象核对。远端删除发生而数据库回执尚未提交的窗口可能产生短暂缺失报告，不能为消除该窗口而忽略全部隐藏对象。
- 当前自动对账范围是预期对象/孤儿、S3 HEAD 字节与 sha256 元数据、北京文件实际字节/hash；不是对整个远端对象内容重新下载计算哈希，也不是 bucket 权限证明。
- 架构要求的每日“公开前缀与发布状态一致性”和“默认图文件/入口可读性”还未接入定时对账。现有公开视图和前端默认图测试不能替代这两项，保留为 M5 开发待办，不缩减验收范围。
- 未启动 Docker、真实 PostgreSQL/邮件/S3/R2/Cloudflare，未触发远程 CI；本轮无前端改动，未重建前端或重跑浏览器。测试临时 HTTP 进程由夹具清理。

M5 及整个 Release A 仍未完成整体验收，公网 no-go 不变。
