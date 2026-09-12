# Release A Worker 租约与重试加固

日期：2026-09-10（Asia/Shanghai）。本轮没有启动 Docker 或真实 PostgreSQL。

## 已复现与发现的风险

可控时钟将首条副作用执行后的时间推进 31 秒，超过 30 秒租约。修复前实际测试输出为 `processed=2 completed=[first second] failed=[]`，Dispatcher 没有报错；另一测试确认传给处理器的是没有 deadline 的 Context。两项红色回归均失败，修复后通过。

```powershell
go test ./services/platform-worker/internal/outbox -run 'TestExpiredBatchCannotCompleteOrStartMoreSideEffects|TestProcessorReceivesBoundedLeaseContext' -count=1
```

代码/Schema 对照还发现三处风险，**没有在真实 PostgreSQL 上复现**：

- 完成/失败只核对 Worker 名称，不核对已捕获的领取次数，同名 Worker 重领后可能接受旧结果。
- 媒体完成 CTE 读取任务行时不加锁，可能先修改媒体对象，再在最后更新任务时发现租约已换主，留下不一致。
- 崩溃重领会无限增加次数，而表限制 `attempt_count <= 20`，到第 21 次可能使整批 Claim 失败；原先只有明确 Fail 才会在第 10 次转 dead。

## 修复合同

1. Claim 返回数据库 `lease_expires_at` 和递增的 `attempt_count`。批内每条处理器只获得剩余租约减 1 秒的时间，剩余部分用于结算；过期/关闭后不再结算旧结果或启动下一条。
2. Complete/Fail 同时校验事件、Worker、原领取次数、应用时间及数据库 `clock_timestamp()` 下的有效租约；缺少或非正领取次数在访问数据库前拒绝。
3. 媒体完成使用 materialized `SELECT ... FOR UPDATE` 锁定并重新校验任务行，然后在同一 SQL 事务中修改媒体及 outbox，旧领取不得提前释放容量。
4. 到期可领取事件的次数已达到 10 时转 `dead`、清除租约并写 `attempts_exhausted`；不再增加次数。该分支和正常领取共享 SKIP LOCKED 候选集、互斥更新，耗尽任务不阻塞健康新任务。

这不提供 exactly-once：HTTP 超时或取消只结束本地等待，远端可能已经执行。缓存刷新和媒体删除必须继续按原合同支持安全重试。

## 本机证据

| 检查 | 实际结果 |
| --- | --- |
| Go API/Worker vet 和 `test -count=1` | 全部包通过；显式设置 MEDIA_CONTRACT_PYTHON，实际 Go→Python HTTP 合同继续通过 |
| Worker 交付编译 | `go build -trimpath` 成功生成本地 Worker 可执行文件；未启动该进程 |
| 租约专项 | 8 项通过：过期批次、处理截止时间、捕获次数传递、数据库返回旧期限、超时假成功、进程关闭、非正次数、测试库地址边界 |
| 真实 PostgreSQL 租约合同 | 明确 SKIP：dedicated worker PostgreSQL contract database is not configured |
| Go→Next production build 缓存合同 | 7/7 通过；[本轮机器报告](./release-a-cache-after-lease-2026-09-10.json) |
| 全套 Python | 170 passed, 73 subtests passed；90.47 秒 |
| CI 安全合同 | 10 passed，包含受限 Worker/独立测试库/不可跳过的 migrations 门槛 |
| ruff | 相关安全合同检查通过 |

运行命令：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
npm run test:cache:release-a -- --output docs/evidence/release-a-cache-after-lease-local.json
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
```

## 真实数据库门槛（已接线，尚未执行）

CI migrations 新增独立 `self_deepsearch_worker_test` 数据库，不混用 API fixtures。测试要求两个地址都指向同一回环端口、固定库名、显式确认短语和空数据库；advisory lock 防止多个合同同时操作队列。管理员连接只负责合成数据和锁竞态夹具，Claim/Complete/Fail 均通过受限 `platform_worker_login`。用于锁竞态的管理员连接还需要查看 `pg_stat_activity` 中 Worker 的锁等待；CI 的一次性管理员具备该权限。

5 个实际 SQL 场景：

- 相同 Worker 名称的过期/重领取：旧成功和旧失败都被拒绝，当前领取可完成，重复结算被拒绝。
- 失败进入退避、不可立即重领，第 10 次失败进入 dead。
- 崩溃任务已到 20 次时转 dead，不触发第 21 次 CHECK 错误，健康事件仍被领取。
- 两个并发 Claim 不返回同一条事件。
- 管理事务先锁住并更新领取次数，让媒体 Complete 真正等待行锁；解锁后旧结算必须失败、对象仍 hidden，当前领取才可将对象和事件一起完成。

本机未提供测试库，远程 CI 未触发；该清单是待执行合同，不是 SQL 并发已经验证的结论。

## 部署与边界

不新增迁移、API 路由或外部依赖，不改 HTTP 签名/回执格式。需更新日本 Worker；发布前先执行真实 PostgreSQL 合同，并停止旧 outbox 消费者再替换，避免旧逻辑继续落库。不能在旧执行仍可能存活时手工清零次数或把 dead 标记 completed。北京服务本轮不变，此前媒体回执配对升级要求继续有效。

真实 PostgreSQL、S3/Cloudflare、SMTP、多实例目标环境、CGO race 和完整上线验收仍未完成；Release A 整体保持未完成。
