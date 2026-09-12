# Release A Worker 完成回执验证

日期：2026-09-10（Asia/Shanghai）。

## 已复现的问题

此前缓存失效和图片删除处理器只检查 HTTP 2xx，并丢弃返回内容。使用 HTTP 200 的 HTML 登录页作响应时，两个实际处理器均调用了测试仓库的 Complete：`completed=[合成事件]`、`failed=[]`。这会让错误地址/代理响应被当作副作用已完成；图片还可能因此被错误标记为已删除并从容量中扣除。

红色回归命令（修复前两类 html 用例均失败）：

```powershell
go test ./services/platform-worker/internal/outbox -run 'TestProcessorsRequireBoundCompletionAcknowledgement/(revalidate|media_delete)/html$' -count=1
```

## 修复

- 缓存：要求 HTTP 200、application/json、`revalidated=true`、相同事件 ID。
- 图片：要求 HTTP 200、application/json、`status=deleted`、相同事件 ID、存储范围和对象 key。
- 回执最大 4096 字节，拒绝不完整 JSON、非法 UTF-8、超大响应、空 200、HTML、202 和错配回执；错误码分别为 `revalidate_invalid_response`、`media_delete_invalid_response`，不把原始正文写入错误。
- Python 删除服务完成存储删除、缓存 purge 和本地副本删除后才生成回执，返回实际使用的规范对象位置。
- Dispatcher 不再默认用“校验事件格式”替代真实处理器；漏配时在 Claim 前拒绝。

## 本机通过证据

| 检查 | 实际结果 |
| --- | --- |
| Go API/Worker `go vet` | 通过 |
| Go API/Worker `go test -count=1`，设置 MEDIA_CONTRACT_PYTHON | 全部包通过；数据库条件测试仍需外部测试库 |
| 两类处理器完成回执 | 30 个场景通过，含真成功、空响应、HTML、错 Content-Type、错事件、错对象、错范围、UTF-8/JSON/体积异常和异步受理 |
| 缺少处理器 | 不 Claim、不 Complete、不 Fail 原事件；拒绝启动 |
| Go→Python HTTP 删除与恢复 | 真实执行通过，0.30 秒；先 purge 失败并保留副本，再恢复、重放同事件并完成 |
| Python 媒体 HTTP/服务专项 | 8 项通过 |
| 全套 Python | 168 passed, 73 subtests passed，98.91 秒 |
| ruff / 媒体生产代码 mypy | All checks passed；5 个源文件无类型问题 |

复验命令：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.\.venv\Scripts\python.exe -m pytest -q -p no:cacheprovider
.\.venv\Scripts\python.exe -m ruff check workers/media-python infra/security
.\.venv\Scripts\python.exe -m mypy workers/media-python/media_worker
```

## 跨语言合同的边界

实际使用生产 Go 签名/发送器、Python HTTP handler 和 DeleteService，本机回环端口传输。S3 用独立临时 objects 目录替代，Cloudflare 用可控制的失败标识替代；副本目录同样是临时文件。outbox 仓库是测试替身，验证调用 Fail/Complete 的分支，没有执行真实 PostgreSQL Claim、租约、退避或状态事务。测试不读取生产配置，不持有数据库/对象存储凭据；结束后回收自己的 Python 进程和临时目录。

CI Go 和 Go race 作业已配置 Python 3.12 与 `MEDIA_CONTRACT_PYTHON=python3`，确保该合同不被默认跳过；本轮未触发远程 CI，本机仍未复跑 CGO race。真实 R2/Cloudflare、PostgreSQL、Next 缓存失效和两地部署验收仍未完成。

## 升级要求

无需数据库迁移。先部署北京 media-python，再部署日本 platform-worker。新版 Worker 会拒绝旧的两字段图片回执并重试；若耗尽次数进入 dead，应检查原因后按运维流程重新处理，不能手工伪标 completed。回滚也需保持两端回执兼容。业务回执不替代跨区 TLS/VPN。
