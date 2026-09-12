# Release A 逐操作上传准入与自动暂停验证

日期：2026-09-11（Asia/Shanghai）。范围：日本 Go 私有准入接口、北京 Python 签名客户端与持久控制日志、配置/部署检查、监控规则和当前架构/运行文档。

本轮不新增数据库迁移、公开 API、常驻服务或运行依赖。Schema 保持 v21、62 表/5 公开视图，OpenAPI 87 操作/76 路径/106 Schema，20 个近期密码操作。默认全部关闭；自动采集、广告联盟继续关闭。

## 最终验证结果

| 检查 | 结果及适用范围 |
| --- | --- |
| Go API/Worker 全量 `vet`、`test -count=1` | 通过；显式指定本机 Python，实际执行跨语言 HTTP 合同。需要真实数据库的既有合同仍 SKIP，不代表迁移/权限验收 |
| Worker 二进制构建 | 通过；只构建到本地 `.cache/platform-worker.exe`，没有启动常驻 Worker |
| 新准入 Go 模块覆盖率 | **95.4%**；含实际 Python→Go HTTP 合同，不包含真实 PostgreSQL/供应商 |
| 完整 Python 回归 | 最终 **268 passed、272 subtests passed、1 skipped（93.35 秒）**；先前一遍为 267 项，增加 CI 依赖合同后全量复跑，未混用计数 |
| Python 专项 | 自动暂停/手动控制/环境检查/静态准入合同 42 项、135 subtests 通过；此为中间定向结果，不另加到全量计数 |
| Python ruff / mypy | 全范围 ruff 通过；18 个生产文件 mypy 通过。修正新客户端 Buffer 覆盖签名、测试 import 排列后重跑 |
| OpenAPI 确定性生成 | `scripts/generate_openapi_types.py --check` 通过，无公开合同漂移 |
| CI 配置 | Go 作业补安装已有媒体 Python 依赖，静态验证安装先于 HTTP 合同执行；没有触发远程 CI |
| 文档收尾复跑 | DB/安全/环境离线合同 **137 passed、142 subtests（14.80 秒）**；ruff、OpenAPI 无漂移，Go 格式检查无输出 |
| 本地文档链接 | 本轮更新的 19 份文档、235 个本地文件链接全部存在；未验证页内锚点/外部网址 |

本轮未改前端、未重建 Next、未重跑浏览器/截图/性能/联网依赖审计；上一轮 70/70 浏览器验证仍见[页面证据](./release-a-media-upload-ui-2026-09-11.md)，不冒充本轮结果。Python 一个符号链接权限条件跳过保留，不算通过；Windows 上的回归不代替 Linux 文件锁/断电验收。

## 关键覆盖与实际 HTTP 证据

- Go 拒绝错误方法/路径/query/raw path、缺失/重复头、错误编码/长度、错误签名/nonce、过期/未来时间；签名响应绑定本次请求，策略/数据库错误不泄露正文。
- 实际 TaskGuard 覆盖低风险、85%/95%、review/stop、样本失效/缺失与读取失败；两个并发检查上限，过载签名 deny；截止时间/时钟回退不返回 allow。关闭时无路由，指标仍受私有认证保护。
- Python 实际回环 HTTP 覆盖有效许可/拒绝、旧响应重放、重定向、未签名/篡改、重复字段/头、错误类型/UTF-8、超大正文/响应头、慢响应头、连接拒绝和非法 IP origin；3 秒总截止不依赖单次 socket read 超时重置。
- 自动暂停推进原 epoch/generation，v2 日志明确 system source，不写 actor。重启、关闭开关或新统计变好不解除已持久化暂停；旧快照恢复被拒，过期恢复在反向检查后再次被拦截。
- fsync 失败不确认成功；日志损坏/伪造来源/错误版本拒绝读取。每页 Listing 和每次 PUT 之前重新检查，待确认上传 journal 保留，必要 DELETE/HEAD 不被自动暂停阻断。
- **实际 Python→Go 集成**：Go 使用真实 TaskGuard 与合成状态读取器，Python 使用真实 HTTP 客户端、文件锁/日志、S3 适配器和合成 SDK。前三次检查健康，随后升至 95%：仅第一次 PUT 发出，下一次被拒并进入 generation 3 paused；旧恢复失败，风险未解除时新恢复也失败；必要删除执行一次且 pending 不变。测试端模拟复核后不自动恢复，显式恢复进入 generation 4，pending 仍阻止新批次。合计 6 次 Go 准入检查。测试专用 reviewed 路由只存在 `httptest`，没有进入生产。
- 配置合同验证两地必须同时 enforce、日本 observe/dispatch、北京共享 control、独立匹配密钥、IP origin 与 HTTP 明确允许；不打印秘密。默认 Compose 无新增公网 Worker 端口，隧道叠加文件仅绑定 127.0.0.1。

## 可复跑命令（本机离线）

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go build -o .cache/platform-worker.exe ./services/platform-worker/cmd/worker
go test ./services/platform-worker/internal/mediauploadadmission -count=1 -cover

$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q
.venv/Scripts/python.exe -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/Scripts/python.exe -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
```

执行时逐条检查退出码，任一失败即停止，不以最后一条成功覆盖前序失败。

## 边界与后续

按后端/架构/安全/运维/QA 检查，将自动事件与人工队列分离；补齐反向请求限时、密钥分离、旧 reader 回滚风险、私有隧道入口和“拒绝不等于全站停止”的告警语义。详见[协议与操作手册](../MEDIA_UPLOAD_ADMISSION.md)。

没有启动 Docker、真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，没有部署、修改远端/防火墙、暂存/提交或触发远程 CI。仅使用本机临时端口、临时状态文件、合成 SDK/状态；无真实图片资源上传。

新增上传准入按操作触发，不主动暂停空闲进程，不撤销在途 SDK/内部重试，不清 pending，不自动恢复。读取的是最近持久观察，不是供应商实时计费；每次检查增加跨区 RTT，真实 Linux/两机资源开销待测。

仍须完成 API/Next 动态默认图、直连图片/源站/边缘全入口控制、存储 GB-month/账单口径及目标环境业务验收。代码与离线合同可继续进入隔离测试；完整 Release A/M5 与公网 **no-go**，目标保持进行中。
