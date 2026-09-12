# Release A：账户操作量只读观察验证

日期：2026-09-11（Asia/Shanghai）。结论：新增观察模块本地验证通过；完整额度保护/Release A/M5 未完成，公网 **no-go**。

## 本轮实现

- 现有 Go Worker 二进制增加 `media-usage`，在数据库和常驻 Worker 初始化之前处理。只有显式 `--live` 才调用 Analytics；未改 Compose/默认调度、Python、前端或数据库 Schema v18。
- 固定 Cloudflare HTTPS GraphQL 端点；独立环境变量只读凭据，无重定向/自动重试。查询整账户已知 A/B/免费操作，不只看两张业务桶。
- 最多 31 个不超过 24 小时的串行查询、8 秒单请求/30 秒整体期限、2 MiB 单响应上限；分段共用端点，保守重复而不漏毫秒边界。任一部分失败，丢弃整次累计，不输出伪完整数值。
- 拒绝部分 GraphQL errors、缺失/null、截断、重复/歧义 JSON、未知操作、非整数或溢出。整期没有数据判为 unknown。
- 70%/85%/5% 预留的风险建议；陈旧或错误账期不能得出低用量结论。报告始终说明非账单、无已确认完整水位、未含存储、未接执行、不自动恢复。

代码：

- [operations.go](../../services/platform-worker/internal/mediausage/operations.go)：读取、分段、分类、响应与错误保护。
- [policy.go](../../services/platform-worker/internal/mediausage/policy.go)：纯函数风险判定，不修改运行状态。
- [command.go](../../services/platform-worker/internal/mediausage/command.go)：显式只读命令、JSON 报告、退出码和凭据保护。
- [Worker 入口](../../services/platform-worker/cmd/worker/main.go)：在常驻初始化前分发子命令。

## 先失败、后修复的证据

补测真实本地 HTTP 返回的毫秒事件：首次分段用“前段结束整秒、后段 +1 秒开始”，3 个边界事件只统计到 2 个，测试失败。修复为使用已公开的 `geq/leq` 共用端点后，3 个事件都被覆盖，恰在端点的那个保守重复，总计 4；报告/文档明确标注不精确，不能解释为账单或无误差上界。

补测 JSON 重复 `errors`、`requests` 和 `Requests/requests`：Go 默认解析会采信后值，曾把高计数改成 1 或忽略前面的错误。3 个负向用例在修复前失败；新增有深度上限的字段歧义检查后全部通过，不保留部分计数。

## 最终执行结果

| 检查 | 结果与边界 |
| --- | --- |
| 新 Go 模块 | **17 个顶层测试、82 个子测试通过，0 失败**；覆盖 26 个定价操作分类、分段/31 天范围/毫秒边界、空数据/部分失败、格式/数值/超限、重定向隔离、取消/超时、阈值/过期、命令授权与输出隐私 |
| 覆盖率 | `go test ... -coverprofile`：**95.8% statements**；不是供应商或账单验证比例 |
| Go API + Worker | 最终 `go vet`、`go test -count=1` 全部通过；显式设置 `MEDIA_CONTRACT_PYTHON`，包含真实本地 Go→Python HTTP 合同；未配置真实 PostgreSQL 合同依赖，不把 SQL skip 当执行通过 |
| Worker 二进制 | 已重新编译 `.cache/core-e2e/platform-worker.exe`；真实执行 `media-usage --help` 返回 0，无参数返回预期 2，均不启动常驻进程/数据库连接；未执行 `--live` |
| 受影响周边合同 | Python `infra/security infra/reverse-proxy scripts/tests`：**129 passed、30 subtests passed，20.14 秒**；不是全范围 Python 回归 |
| 格式 | 新模块全部 Go 文件和 Worker main 的 `gofmt -l` 无输出 |
| 文档 | 本轮同步的 12 份文档共 122 个本地链接全部有效，0 个缺失目标 |
| 前端/浏览器 | 本轮未修改，未重建或重新执行；上一轮 46 项浏览器证据不算作本轮结果 |
| Go race/目标资源 | 本轮未执行；本机已知缺 CGO 工具链，需 CI/目标主机验证，尤其不能据此声称日本限额通过 |

实际命令（本轮仅以下本地验证，不含真实账户查询）：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-worker/internal/mediausage -count=1 '-coverprofile=C:/Users/jayso/Documents/self-deepsearch/.cache/media-usage-coverage.out'
go build -o .cache/core-e2e/platform-worker.exe ./services/platform-worker/cmd/worker
& ./.cache/core-e2e/platform-worker.exe media-usage --help
# 下一个命令预期退出 2，不代表验证失败：
& ./.cache/core-e2e/platform-worker.exe media-usage
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider infra/security infra/reverse-proxy scripts/tests
```

测试 JSON 事件另核对得到 17/82/0。相对 `-coverprofile` 的首次命令虽测试通过，但后续 cover 工具未找到文件；改成带引号的绝对路径重跑后成功读取覆盖结果，未将前一次工具失败隐去。

## 外部信息与未完成范围

只读获取了 Cloudflare 的 R2 定价、Analytics、GraphQL 总览和公开 bucket 官方说明，链接/页面更新日期见[观察手册](../MEDIA_USAGE_OBSERVATION.md)。Windows Schannel 客户端读取公开文档曾失败，换用 Node 默认 TLS 验证成功；没有关闭证书检查。没有使用账号凭据或访问真实业务 API。

官方文档明确：Analytics 不应作为计费用量依据。该命令还没有实际账户返回值、权限/时间范围/操作名兼容证据，也没有供应商完整性水位；不能声称真实计费量已接通。未知实际操作名会先失败，须查证后再维护映射，不通过静默归免费来“修好”报告。

仍需：实际供应商/套餐/账期确认、存储 GB-month 与账户其他消耗、持久快照/统计回退/跨月策略、定时读取与动态跨区执行、非必要任务控制、全部图片源站/已缓存入口控制，以及真实数据库、SMTP、R2/S3、Cloudflare、备份和目标资源验收。

本轮没有启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 业务服务；没有暂存、提交、推送、远程 CI 或子代理；本地测试监听和子命令已退出。完整目标保持 active/incomplete。
