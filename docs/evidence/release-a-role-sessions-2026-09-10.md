# Release A 角色变更与会话失效验证

日期：2026-09-10（Asia/Shanghai）。本轮不启动 Docker、PostgreSQL、SMTP、S3/Cloudflare、采集或广告，不触发远程 CI。

## 规则与实现

只有 owner 且完成近期密码确认，才能调整其他非 owner 账号的角色。真实角色变化时，更新角色、撤销该账号所有未撤销 session、记录审计在同一事务完成；撤销原因为 `role_change`，时间为 `greatest(request_time, created_at)`，不覆盖已有撤销证据。审计快照保存新旧角色与 `sessions_revoked` 聚合数，不保存 token。相同角色返回 409，不撤销新会话、不重复写审计。

目标账号必须重新登录，普通用户新会话为 30 天，editor/admin 新会话为 8 小时。近期密码确认仍绑定旧 session，不能绕过登录会话失效。操作者及其他账号的会话不受影响，不改变 owner、自改角色和关闭账号的既有保护。

角色事务与 `CreateSession` 共用账号行锁：先完成的旧角色登录会被本次变更撤销；在角色事务后等待的旧角色快照，必须重新检查后拒绝。这里保证的是事务提交后的会话校验，**不声称撤回已经通过鉴权并正在执行的请求**。

运营后台在操作前展示所有设备需重新登录的说明；仅在收到成功响应后展示“旧登录会话已失效”。接口失败时角色保持原值，不展示成功；关闭账号不再显示角色编辑控件。公开站没有页面改动。

无需 Schema 迁移，仍为 v16。OpenAPI 增加行为说明及 409/503 合同，成功响应结构不变。部署时先更新所有 API，再更新运营站；新旧 API 混跑时不能依赖新会话失效保证。

## 本地验证

- Red：抽出可注入事务入口但不改变业务逻辑后，新增测试实际复现 `[target actor update audit commit rollback]`，缺少撤销步骤；撤销故障用例与相同角色保护也失败。
- Green：事务单测验证 5 种角色转换、目标行锁、更新→撤销→审计→提交顺序、target/actor/update/revoke/audit/commit 六类失败，以及相同角色/越权/非法角色无副作用。此处 pgx 事务为测试替身，不是 PostgreSQL 引擎运行证据。
- HTTP 回归验证撤销失败 503、相同角色 409、不泄漏内部错误/不发 Cookie/no-store；已失效 session 不能配合仍有效的近期确认 token 调用角色变更。
- 登录期限回归扩展为 user/editor/admin，保持 30 天/8 小时/8 小时。
- Go API/Worker vet/test 通过，显式启用真实 Go→Python HTTP 合同。最新 API 已用 `-trimpath` 编译为 `.cache/core-e2e/platform-api.exe`，未启动。
- 运营站 production build、两站/合同包 TypeScript、两站 ESLint 通过。完整 Playwright 16 场景 × 桌面/移动 = 32 项通过（2.5 分钟），角色场景新增操作前说明、503 后不误报成功、成功反馈和相同角色 409。浏览器使用合成 API，**不证明真实 session 已在 PostgreSQL 被撤销**。公开站复用上一轮构建，源码未变。
- 最终串行 Python 175 项 + 78 个 subtests 全部通过（80.75 秒），修改的 Python 合同 ruff 通过；13 项前端单元烟测与两站 standalone HTTP 冒烟通过。未重新执行 registry 依赖审计、Compose、Go race 或缓存失效专项，相关历史证据及工具链限制保持原状。

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go test ./services/platform-api/internal/database -run '^TestRoleChange' -count=1
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
npm run build --workspace @self-deepsearch/ops-web
npm run check:api-types
npm run typecheck
npm run lint
npm run test:e2e:release-a
```

## 实际 PostgreSQL 验收：尚未执行

新增两组合同，沿用回环固定一次性库、v16、无 superuser/BYPASSRLS 的 `platform_api_login`、随机夹具与追加式审计；不执行 DELETE 清理，不增加权限。现有 CI migrations 已执行整个 database 包，无需新作业，但本轮没有触发 CI。

1. `TestPostgresRoleChangeRevocationContract`：user→editor→admin→user；撤销多设备及请求后创建的会话，保留原退出原因与时间，owner 会话不受影响，拒绝旧快照和旧 session 近期确认；真实服务重新登录后核对期限，相同角色重试不踢新会话且仅一份角色审计。
2. `TestPostgresRoleChangeBlocksStaleConcurrentLogin`：用真实事务暂停提交，观察旧快照登录在账号行锁等待，提交后必须返回无效凭据且不留下 token 行。

```powershell
# 仅使用运行手册规定的受限一次性 PostgreSQL 16 数据库环境变量。
go test ./services/platform-api/internal/database -run '^TestPostgresRoleChange' -v -count=1
```

本机两组均明确 SKIP。尚未得到真实 SQL 并发/撤销证据，不能把本地 Go 套件的整体成功当作数据库验证完成。最终入口、邮件、存储、图片下架与目标主机资源验收也仍未完成；Release A 目标保持进行中，公网仍 no-go。
