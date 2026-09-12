# Release A 账号与会话生命周期加固

日期：2026-09-10（Asia/Shanghai）。本轮没有启动 Docker、连接真实 PostgreSQL/SMTP/S3/Cloudflare 或触发远程 CI。

## 发现的风险

以下来自代码与 Schema 对照，**没有在真实 PostgreSQL 上复现**：

1. 登录在验证密码后直接按用户 UUID 创建 session。若验证与写入之间发生密码重置或账号关闭，旧凭据可能在撤销操作之后生成新会话。
2. 重置挑战已保存用户 UUID，但改密语句只按邮箱选账号。关闭账号后，同邮箱可以注册成新 UUID，旧挑战不应作用于这个新账号。
3. 撤销时间使用请求开始时捕获的时间，并发登录生成的 session 可能更晚。直接写入较早的 `revoked_at` 会违反 `revoked_at >= created_at` 约束，可能使整次改密或关闭事务回滚。

## 已实现合同

- 登录将刚完成密码验证的账号快照通过内部 `identity.SessionRequest` 传给数据库，包含用户 UUID、邮箱、密码摘要和角色；不把快照或密码摘要放入 HTTP 响应。
- 数据库在单条 SQL 中使用 materialized CTE 和 `SELECT ... FOR UPDATE` 锁定账号，重新核对快照、active 状态和邮箱已验证，再创建 session。零行写入返回统一的无效凭据错误；领域层返回空用户/空 token 并记录失败登录审计。HTTP 保持通用 401，不发送 Cookie 或 token。
- 密码重置在哈希计算前的挑战检查和最终改密事务中核对挑战绑定的用户身份；最终更新同时匹配邮箱、active 状态和挑战中的 UUID。已关闭账号的旧挑战不能用于同邮箱新账号。验证码消费、改密和旧会话撤销仍在一个事务中完成。
- 改密 SQL 仅将 `pgx.ErrNoRows` 转为无效验证码；实际数据库错误不会再伪装成输错验证码。
- 退出、密码重置、关闭账号、owner 密码引导轮换及后台停用账号的撤销时间统一使用 `greatest(request_time, created_at)`。这只保护 session 时间约束，不改变用户操作审计的原始精确时间。

普通用户新签发会话仍为 30 天，owner/admin/editor 为 8 小时；验证码、邮箱验证、账号软关闭及历史保留规则不变。此次没有修改角色变更对既有会话的处理策略。

## 本机实际验证

| 检查 | 结果与边界 |
| --- | --- |
| Go API/Worker `go vet`、`go test -count=1` | 全部包通过；显式设置 `MEDIA_CONTRACT_PYTHON`，真实 Go→Python HTTP 合同继续执行通过；需要 PostgreSQL 的合同仍按环境跳过 |
| 登录快照专项 | `TestLoginSessionCarriesVerifiedAccountSnapshot` 通过；覆盖 user/admin 快照、token 哈希、时间/有效期及拒绝时的空结果与失败审计 |
| HTTP 凭据错误专项 | `TestLoginUsesGenericCredentialError` 通过；即便测试替身在报错时附带 token，HTTP 也仅返回通用 401，不下发 Cookie/token |
| 数据库地址守卫 | `TestIdentityContractDatabaseGuard` 通过 |
| 真实 SQL 生命周期合同 | `TestPostgresIdentityLifecycleContract` 明确 SKIP：`PLATFORM_API_TEST_DATABASE_URL is not set` |
| API 交付编译 | `go build -trimpath` 成功生成 `.cache/core-e2e/platform-api.exe`；未启动该二进制 |
| 全套 Python | 171 passed, 73 subtests passed；79.62 秒 |
| CI 安全合同 | 11 passed；0.46 秒；包含受限 API 登录和生命周期测试不可被 CI 跳过的接线约束 |
| ruff | `infra/security` 通过 |
| 文档收尾复验 | 产品边界与 CI 安全合同合计 49 passed, 2 subtests passed；10.67 秒；6 份本轮文档中的 32 个本地 Markdown 文件链接均存在 |

本轮未改前端，也未重跑 Next 构建、浏览器或缓存合同；其历史证据见[缓存运行态验证](./release-a-cache-contract-2026-09-10.md)与[Worker 租约验证](./release-a-worker-lease-2026-09-10.md)，不将历史结果计为本轮重新执行。

可复验命令（仓库根目录 PowerShell）：

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-api/internal/identity -run '^TestLoginSessionCarriesVerifiedAccountSnapshot$' -count=1 -v
go test ./services/platform-api/internal/httpapi -run '^TestLoginUsesGenericCredentialError$' -count=1 -v
go test ./services/platform-api/internal/database -run 'TestIdentityContractDatabaseGuard|TestPostgresIdentityLifecycleContract' -count=1 -v
go build -trimpath -o .cache/core-e2e/platform-api.exe ./services/platform-api/cmd/api
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider infra/security/tests/test_dependency_security_contract.py
./.venv/Scripts/python.exe -m ruff check infra/security
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider infra/security/tests/test_release_a_product_contract.py infra/security/tests/test_dependency_security_contract.py
```

## 真实 PostgreSQL 门槛（尚未执行）

测试只接受 `PLATFORM_API_TEST_DATABASE_URL` 指向回环 `127.0.0.1`、显式端口、固定专用库 `self_deepsearch_migrate_test` 和 `platform_api_login`。仅允许不带查询或 `sslmode=disable`，拒绝 host 覆盖、片段、其他主机/库/登录账号；库须已迁移到 v16，登录不得为超级用户或具备 BYPASSRLS。不要指向业务库或包含真实用户的长期测试库。

4 组待执行 SQL 合同：

1. 改密和关闭使旧会话失效，旧密码/已关闭账号快照不能再签发会话，已消费验证码不能重放；模拟 session 创建晚于改密/关闭请求开始的时间。
2. 角色、邮箱、locked/suspended 或 pending 未验证状态改变后，旧快照不能写入 session，拒绝时数据库无遗留 session 行。
3. 关闭旧账号后同邮箱创建新 UUID；旧重置验证码在预检查和最终改密时均失败，新账号密码不变。
4. 一个事务持有账号密码更新行锁，另一个连接实际调用 `CreateSession`；通过同账号 `pg_stat_activity` 观察锁等待，提交后旧快照必须失败且不产生 session。

现有 CI `migrations` 作业已配置用上述受限登录运行完整 API database 包，新合同自动纳入该必过步骤；无需新增 CI 作业或修改工作流。新增静态安全测试固定该接线。合同会写随机合成账号、挑战、session 和追加式审计，保留在一次性 CI 库中；不授予 DELETE 权限、不删除审计、不停用触发器来做测试清理。

这些测试代码及 CI 配置存在不等于真实 SQL 并发、权限或邮件端到端已经通过；本机及远程均未执行该真实库合同。

## 升级与尚未关闭项

不新增数据库迁移、HTTP 路由、OpenAPI 字段或外部依赖。需替换全部日本 API 实例（含镜像中的 owner 引导工具）；旧 API 仍在运行时不能依赖新的签发保证。北京媒体服务、Python 工具、Worker 和前端本轮不需配对升级。回退旧 API 会恢复上述风险，不应作为已加固状态对外放行。

本轮复核另发现以下待完成项，尚未修复，也没有进行故障/负载复现：

- P1：Go logout 忽略撤销错误，两个前端退出路由也忽略上游失败并清 Cookie/跳转；可能仅退出当前浏览器，服务端 session 仍有效。需要 API 与两站一起补错误、超时、重试和成功确认合同。
- P1：Argon2id 新密码计算每次约 64 MiB，现有校验还接受更高的已存哈希参数，没有进程级并发护栏；接受邀请在数据库验证挑战/限流前先算哈希。需按日本 2C4G/API 预算限制计算并发及等待，补过载回归。
- P1：角色变更会更新账号角色，但不撤销或收紧既有 session；普通账号提升后可能沿用原 30 天会话。需统一角色变化与存量会话策略并补验证，不能把本轮“新签发快照校验”当作已解决该问题。

负责人和放行条件见[安全审计](../RELEASE_A_SECURITY_REVIEW.md)。Release A 仍未完成，真实 PostgreSQL/邮件/对象存储/Cloudflare、CGO race 与完整部署验收继续待执行。
