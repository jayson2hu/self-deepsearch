# Release A 真实核心 CI 门槛：本地开发验证

日期：2026-09-10（Asia/Shanghai）。这是运行器与 CI 配置的本地验证，不是真实服务 E2E 通过报告。

## 本轮交付

- 新增 `scripts/release_a_core_e2e.py`：限定固定名称的回环专用空库，使用受限 `platform_api_login`；引导随机 owner、启动编译后的 Go API，以 Mailpit 串联账号验证、邀请 editor、异人审核发布和隐藏。
- 修正旧邮件 E2E 遗漏的近期密码确认；创建邀请前必须 reauth 成功。内部邀请 helper 支持 user/editor/admin 并核对接受后的角色，不允许邀请 owner。
- CI 新增 PostgreSQL 16/Mailpit 的 `core-e2e` 作业，真实迁移/运行账号后执行流程；五镜像交付依赖该作业成功。只上传脱敏 JSON，不上传进程日志、原始响应或凭据。
- 运行器不会创建、重置数据库或启动 PostgreSQL/Mailpit；对自己的 API 进程做成功/失败退出清理。

## 实际本地结果

| 检查 | 结果 |
| --- | --- |
| 全套 Python 回归 | 165 passed, 73 subtests passed，162.50 秒 |
| 全套 Node 发布工具合同 | 46 passed |
| `ruff check scripts infra/security` | All checks passed |
| 三个 E2E 工具 mypy | Success: no issues found in 3 source files |
| Go API / create-owner 编译 | 两个 Windows 二进制均编译成功；未启动 |
| 运行器 `--help` | 退出 0 |
| 未提供一次性确认的真实 CLI 调用 | 退出 1；stage=configuration、error_type=ValueError、checks 为空；未启动进程或访问数据库 |

主要验证命令：

```powershell
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.\.venv\Scripts\python.exe -m pytest -q -p no:cacheprovider
npm run test:preflight
.\.venv\Scripts\python.exe -m ruff check scripts infra/security
.\.venv\Scripts\python.exe -m mypy scripts/release_a_core_e2e.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
go build -trimpath -o .cache/core-e2e/platform-api.exe ./services/platform-api/cmd/api
go build -trimpath -o .cache/core-e2e/create-owner.exe ./services/platform-api/cmd/create-owner
```

新增离线覆盖包括：无确认不启动、拒绝远程/非专用库/管理员账号/host 覆盖、子进程不继承无关凭据、错版本/已退出 API 不算 ready、API/数据库与 Mailpit 就绪门槛、慢退出终止并回收、通过邀请创建独立 editor、失败路径停止 API、报告不包含异常正文或密码，以及 CI 交付依赖与专用迁移路径。

首轮编排用例清空 Windows 环境后，真实 HTTP 客户端初始化 TLS 失败；这不是服务端 TLS 缺陷。该离线用例现显式替换客户端构造器，避免编排测试依赖系统 TLS 状态；真实运行器仍使用真实 HTTP 客户端，没有关闭证书校验。修正后上述全套回归通过。

## 未完成的真实证据

本机命令/标准安装路径检查未发现 PostgreSQL/psql/Mailpit；按用户要求没有启动 Docker，也没有触发远程 CI。因此没有声称真实邮件、数据库事务或新 `core-e2e` 作业已经通过。

后续应检查 CI 产出的 `release-a-core-e2e-<commit>` 中 JSON 状态为 passed，并核对提交版本及每个检查项。该报告即使通过，仍不覆盖 Worker/outbox、两个前端的真实 API 集成、Turnstile、S3/R2、Cloudflare、备份恢复和日本/北京目标服务器验收。

当前结论保持：本地代码可进入真实测试环境；完整真实环境验收与公网开放尚未完成。
