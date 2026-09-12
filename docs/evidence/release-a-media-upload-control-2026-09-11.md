# Release A：跨区上传控制本地证据

日期：2026-09-11；工作区 `C:/Users/jayso/Documents/self-deepsearch`。范围为日本 Go 手动命令 → 北京 Python 私有控制接口 → 本地持久化/SDK 上传准入，不是后台/自动额度联动或真实供应商验收。

## 实现结果

- 默认 off，显式 live、japan 区域、精确 epoch/generation、UUID v4、原因和恢复确认才可改变状态。
- 独立控制密钥；请求及响应域分离并绑定路径、nonce、请求正文、HTTP 状态与回执。Go 不接受无签名、旧响应、伪造 200、错误 UUID/epoch/generation/mode、额外/重复/缺失字段、非 JSON、超大/截断或重定向。
- 北京追加有界日志、严格链校验、fsync 后才回执。缺日志先暂停、旧恢复冲突、最新同请求重试不重复追加；部分写入/损坏拒绝。enabled 指令为未来暂停保留 16 KiB。
- 准入/每页 Listing/每次 PUT 重核；PUT 占锁时 pause 返回 busy。暂停发生在同批上传中间时，后续 PUT 不发送，已确认对象可核对回滚，pending 保留。恢复控制不清 pending，清 pending 不解除控制暂停。
- 现有 Docker 模板新增可选环境接线但没有启动；所有北京上传器必须共用状态卷。私有密钥不注入 API、前端或北京 Go Worker。

Schema 保持 v20，60 表/5 公开视图；公开/管理 OpenAPI 保持 85 操作/74 路径/100 Schema、19 个近期密码操作。新增的是北京私有执行协议，不是新的浏览器管理 API。

## 测试结果

| 检查 | 最终结果 | 证据边界 |
| --- | --- | --- |
| Go API + Worker `go vet` / `go test -count=1` | 通过 | 显式启用 `MEDIA_CONTRACT_PYTHON`；真实 SQL 仍无数据库而跳过 |
| 新 `mediauploadcontrol` 模块 | 6 个顶层测试、40 个子场景、0 skip；覆盖率 89.7% | 真实 HTTP/签名/文件状态；SDK/S3/Cloudflare 为替身 |
| Python 全量 | **250 passed、193 subtests、1 skipped，86.11 秒** | Windows 无创建 symlink 权限的已有用例 skip；硬链接与真实 OS 锁检查已执行 |
| 新执行端/HTTP + 环境/部署定向 | 34 passed、73 subtests | 含后补的日志容量预留和真实 pipeline 中途暂停回滚 |
| 全范围 ruff | 通过 | 与已有预检范围一致 |
| mypy | 17 个生产源文件通过 | 较上一轮新增两个 Python 模块 |
| OpenAPI TypeScript 漂移 | 通过 | 没有改变公开/管理接口合同 |
| Worker 二进制重建 | 通过 | 更新 `.cache/core-e2e/platform-worker.exe`，未启动常驻 Worker |
| 二进制 help/无 live 命令 | 帮助退出 0；无 live 退出 2 | 后者未发送网络请求，没有数据库初始化 |
| 文档与格式复核 | 15 份文档、178 个本地文件链接有效；gofmt 无待格式化文件，ruff 再次通过 | 校验文件目标存在，不声称 Markdown 锚点或远程链接全部已验证 |

首轮全量 Python 为 248/193 subtests（88.24 秒）；之后补两项边界用例，再完整重跑得到上表 250 项。没有把定向测试与全量计数相加。

实际 Go→Python 合同启动临时回环 HTTP 服务，顺序验证：无状态 → pause 初始化 → 精确重试 replayed → 独立 Python 进程的生产 S3 适配器没有 PUT → 显式 resume → SDK 收到一次 PUT → 再 pause → 旧 resume 冲突 → 新进程 SDK 不再 PUT → 仍可通过既有 Go 删除处理器删除临时对象和北京副本。最后控制日志只有三条状态变化，重试不增条目。

Python 另以真实线程/OS 锁验证 SDK 调用未返回时 pause 是 busy；以实际图片 pipeline + 合成灰图/内存 SDK 验证中途暂停、回滚和 pending。没有读取或生成实际成人图片。

## 先失败后修复

1. 加入默认关闭的控制配置后，旧模板测试把刻意留空的 URL/secret 当成必须报错的占位项，出现 1 项失败（其余 72 项通过）。现仅对控制 off 的空项豁免，enforce 仍测试缺失、同密钥、跨区不一致和非 origin URL；最终完整套件通过。
2. 新补 pipeline 测试的局部 import 未满足 ruff 排序。已调整并重新运行全范围 ruff，无遗留失败。
3. 两次文档批量补丁因未匹配标题被整批拒绝；读取真实标题后重做，未留下半更新内容。最终文档以落盘版本和链接检查为准。

## 可复跑命令

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-worker/internal/mediauploadcontrol/... -count=1 -cover
go build -o .cache/core-e2e/platform-worker.exe ./services/platform-worker/cmd/worker

$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q
.venv/Scripts/python.exe -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/Scripts/python.exe -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
.cache/core-e2e/platform-worker.exe media-upload-control --help
```

终端逐条检查退出码，不把后一个命令成功当成前一个失败已修复。Go 跨语言合同会在未设置解释器时显式 skip；本轮已设置且没有跳过该合同。

## 未执行／仍待开发

- 未启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare/Analytics；未运行远程 CI、暂存/提交/推送或启用部署开关。
- 未修改前端、重建前端或复跑浏览器/视觉/性能；上一轮 54 个浏览器测试保留原日期/适用范围，不计入本轮。
- 真实 Linux 卷/锁/断电、私有目录权限、双区公网传输、实际供应商 SDK 在途写入/重试仍待验收。HMAC 不提供明文 HTTP 保密性。
- 后台用户授权、近期认证、持久命令队列/远端回执落库、自动额度触发尚未接入；CLI 日志不能证明登录操作者身份。
- 页面/API 动态停图、源站直连/边缘缓存全入口控制、账户存储/GB-month/账单口径未完成。`enabled` 回执也不代表 pending/静态模式/容量守卫均已放行。

结论：本地代码可继续进入隔离联调；Release A/M5 与完整目标未完成，公网 **no-go**。操作流程见[手动上传控制](../MEDIA_UPLOAD_CONTROL.md)，总体进度见[PROGRESS](../PROGRESS.md)。
