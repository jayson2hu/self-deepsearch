# Release A 图片上传容量保护验证

日期：2026-09-11（Asia/Shanghai）。延续 M5，未启用真实采集、联盟广告或头像识别；无 Schema/API 合同变更，整体仍 v18。

## 缺陷与修复

- 并发准入测试先复现两个上传从同一旧用量均成功（断言 `2 != 1`），增加覆盖解码到末次上传/manifest 持久化的共享锁后只有一个成功，最终字节不超过批次预算。
- 原容量仅计两个媒体前缀，遗漏同桶其他当前对象。现在逐桶完整 Listing，检查完成标记、Contents、非负整数 Size、KeyCount、令牌唯一/长度和页数上限；读不全就拒绝。
- 丢失 PUT 响应时不能假设远端未写入。生产适配器 PUT 前持久化有界 pending，异常保留拦截。真实子进程被终止后 OS 锁释放，但 pending 仍拒绝新上传。只对收到成功回执的 PUT 尝试回滚；删除/HEAD 失败及未知 PUT 保留北京副本；已存在对象冲突不删除旧对象。
- CLI 输出 manifest 纳入准入，写盘/交接失败保留四对象、副本与 journal。manifest 不覆盖既有文件，写入 flush/fsync 后原子链接；Linux 同步父目录。正常完成后才清 pending。
- `upload-status` 和精确 run-id 的显式人工确认不创建 S3 客户端。确认前先保存复核审计；错 ID、旧 ID、无确认、无有效原因、损坏记录和活跃写入都不能清除保护。SDK 传输错误输出固定结构，不泄露远端 URL。
- 共享卷与 UID 10001 私有目录接入根级/北京部署模板；环境检查要求同一固定锁路径，防止临时目录或独立锁绕开保护。没有添加日本上传器或公开管理端口。

类型检查最初在 Windows 对 `fcntl`/`O_DIRECTORY` 报错，改为 mypy 可识别的 `sys.platform` 分支，Windows 与 Linux 静态类型检查均通过；没有 blanket ignore。文件描述符在校验或解锁异常时也会关闭。

## 实际执行

| 验证 | 结果 |
| --- | --- |
| 媒体 Python 全部测试 + 环境校验专项 | 46 passed、33 subtests；1 symlink 项明确 skipped，3.92 秒 |
| 完整串行 Python 回归 | **210 passed、123 subtests、1 skipped，79.91 秒**，包含新增两项部署静态合同 |
| 真实 Windows 子进程锁 | 活跃写入拒绝第二进程/确认；正常退出释放；终止进程后持久 pending 阻断均通过 |
| SDK 形状内存替身 | PUT 提交后超时、失败回滚、同键冲突、无准入写入、journal 磁盘失败、超限无副本、清单写失败均通过，不调用云服务 |
| Listing | 全桶非前缀对象、空分页、两桶合计、错误字节/计数/完成标记、缺失/重复令牌、页数上限/供应商错误拒绝通过 |
| Python ruff | 项目 Python 全范围通过；仅机械整理新增 import |
| mypy 默认平台 | 15 个生产源文件通过 |
| mypy Linux 平台 | 媒体 6 个生产源文件通过；仅静态检查，不代表已执行 Linux 文件锁 |
| Go API/Worker | 全包 `go vet` / `go test -count=1` 通过；显式设置 MEDIA_CONTRACT_PYTHON，包含实际 Go→Python 删除/对账 HTTP |
| Compose/Dockerfile 合同 | YAML 检查根级/北京共享状态卷、固定路径、环境和非 root 私有目录，日本无新增上传服务；不是容器运行测试 |
| 最终文档/合同复核 | OpenAPI TypeScript 无漂移；10 份文档共 94 个本地链接有效；ruff 和 15 源文件 mypy 再次通过 |

复现与全套运行命令（PowerShell）：

```powershell
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
./.venv/Scripts/python.exe -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
./.venv/Scripts/python.exe -m mypy --platform linux --cache-dir .cache/mypy-linux workers/media-python/media_worker
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
```

## 边界与后续

本机 symlink 测试因账户不能创建符号链接明确 skip，不把它并入通过数；路径缺失/相对路径、真实进程互斥/中断已执行。新状态卷初始化、目标 Linux 文件系统和真实 S3 权限/一致性/重试均未运行。既有 PostgreSQL 合同因没有专用数据库而未执行，Go 套件退出成功不代表 SQL 验收。

最终单独以 `-v` 重跑实际 Go→Python 删除/对账，两项通过；每日检查的两个 PostgreSQL 合同明确输出 `SKIP: dedicated inspection PostgreSQL contract database is not configured`。

没有启动 Docker、PostgreSQL、SMTP、真实 S3/R2/Cloudflare、远程 CI；没有暂存/提交/推送，也没有构建新镜像或改动前端。本轮不重复声明旧浏览器/性能测试为新证据，测试产生的本机子进程和临时 HTTP 已结束。

容量只管两个专用桶当前对象，不包含版本、分段、其他桶、账户月存储计费或 A/B 操作。**A/B 月次数额度、非必要作业自动限流与额度耗尽后全站主动停图/直连入口限制仍待开发**。不能用 API 请求计数估作图片域名真实请求，也不能承诺“10 GiB 护栏保证 10 GB 套餐零费用”。

后续部署须停旧上传器、确认无远端在途写入，核对两桶/备份后升级共享卷与镜像，禁止新旧或跨主机上传器混跑。正常异常恢复先人工核对再精确确认；详见[容量与恢复手册](../MEDIA_UPLOAD_CAPACITY.md)。**M5 和 Release A 未完成，公网 no-go。**
