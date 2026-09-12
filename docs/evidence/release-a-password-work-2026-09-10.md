# Release A 密码计算资源护栏验证

日期：2026-09-10（Asia/Shanghai）。范围仅限日本 Go API 认证资源保护与邀请接受预检；未启动 Docker、PostgreSQL、SMTP、S3/Cloudflare、采集或广告网络，未触发远程 CI。

## 修复及边界

日本 `platform-api` 的 Compose 限额为 384 MiB、0.40 CPU。此前邮箱/IP 限流不能限制多个账号同时进行 Argon2，也不能保护先计算密码再检查验证码的邀请接受路径。

- 进程共享同一个计算器：最多 1 个计算、2 个排队，排队最长 500ms。覆盖注册、登录（含不存在账号的模拟校验）、近期密码确认、重置、邀请接受，以及初始化模拟摘要和 owner 引导工具。
- 队满或等待超时返回 `503 AUTH_BUSY`、`Retry-After: 1`，不发 Cookie、不写密码/session、不消费验证码；登录尝试限流仍可计数，但不会把超载审计成“密码错误”。前端沿用接口错误提示，手动重试，不自动重放认证写请求。
- Argon2 无法中断：请求取消后仍占有名额，直到同步计算真正返回；结果丢弃，不提交凭据。取消前、排队中、计算中和计算器异常退出均有测试。
- 新摘要仍为 Argon2id `m=65536 KiB,t=3,p=1`，16 字节随机 salt、32 字节摘要；既有摘要允许的参数范围未缩小。没有为了吞吐降低加密强度。
- 注册、重置、邀请与登录最终写入，以及近期密码 token 时间，取计算结束后的时钟值，不沿用排队前时间。数据库最终校验仍必须执行。
- 邀请先检查 pending、有效期、验证码、5 次错误上限及每天同 IP 10 次接受额度；无效码的尝试在预检事务提交，正确预检不消费邀请、不预占额度、不持有跨计算数据库锁。最终接受事务再次校验、计额、创建用户/session、消费邀请并追加审计。
- 私有 `/metrics` 新增 `self_deepsearch_password_work_` 前缀的 active/waiting、两类上限、wait_timeout_seconds、rejected/canceled/completed 聚合指标；不含账号、邮箱、IP 或哈希标签，不依赖数据库可用。completed 包含已计算完成但因取消被丢弃的结果，不代表认证成功。

进程限并发不等于限制 RSS，也不等于防住所有注册滥用。现有校验允许最高 256 MiB 摘要，Go GC、目录请求及其他组件仍会占用内存；尚未添加 GOMEMLIMIT，未在目标 0.40 CPU/384 MiB 限额下验证峰值。多 API 进程会各有自己的名额，不能误称集群总并发为 1。

## Red → Green 与本地证据

先加入 `TestAcceptInvitationPrechecksBeforePreparingCredentials`，原实现实际失败：`invalid invitation reached account/session preparation without precheck`。实现预检后恢复通过，并进一步用不允许被调用的测试计算器验证无效码与额度拒绝都不会进入 KDF。

真实 Argon2 的 8 请求突发实测：峰值 1；一次执行完成 2、拒绝 6。比例受机器调度影响，不作为固定断言；固定门槛是峰值 1、总结果 8、队列/名额无泄漏。这个测试没有容器资源限制，不是目标服务器压测。

- Go API/Worker vet/test 已通过，包含显式启用的真实 Go→Python HTTP 合同。
- 专项覆盖 12 个服务级超载/取消子场景、5 个计算后时间子场景、5 个 HTTP 503/Retry-After/no-store/不签发 Cookie 子场景、私有聚合指标，以及排队/取消/异常清理与真实 Argon2 突发。
- OpenAPI 类型重新生成与漂移检查通过；5 个昂贵接口均声明 503 和 Retry-After。
- 最终 Go vet/test 再次通过；排队、取消、服务级拒绝与邀请预检用例重复 20 次通过。最新 API 已用 `go build -trimpath` 写入 `.cache/core-e2e/platform-api.exe`，未启动。
- 两站/合同包 TypeScript、两站 ESLint、13 项前端单元烟测和修改的 Python 合同 ruff 通过；没有改动前端实现。
- 最终安全/反向代理子集 79 项 + 11 个 subtests 通过，相关六份文档的 39 个本地链接有效。两站现有同源代理响应头白名单均已包含 `retry-after`，本轮无需修改。
- 全套 Python 串行执行：174 passed、78 subtests passed，97.86 秒。包含新增错误合同和邀请测试库安全守卫合同。
- 前端实现未改动；上一轮两站 production build、32 项浏览器回归、7 场景缓存实测和退出截图仍为历史证据，不冒充本轮重跑。

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-api/internal/identity -run 'TestPasswordWorker|TestPasswordComputation|TestPasswordFinal|TestPasswordServices|TestAcceptInvitationPrechecks' -v -count=1
go test ./services/platform-api/internal/httpapi -run 'TestPasswordWorkOverload|TestPasswordMetrics' -v -count=1
npm run check:api-types
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
```

## PostgreSQL 合同：本机未执行

新增 `TestInvitationPrecheckDoesNotConsumeOrDoubleCount`、`TestInvitationPrecheckCountsWrongCodeAndLocks`、`TestInvitationFinalValidationRejectsPostPrecheckChanges`；最后一组包含过期、撤销、额度耗尽三个子场景。SQL 不变更 Schema，仍为 v16。

同时修正旧邀请测试夹具：只允许回环固定一次性测试库、受限 `platform_api_login`、无 superuser/BYPASSRLS；改用随机命名空间，保留合成记录和追加式审计，不再尝试忽略权限错误的 DELETE 清理。复跑不依赖删除历史测试行。现有 CI migrations 会执行整个 database 包，无需新权限或 CI 作业。

```powershell
# 仅在手册规定的一次性 PostgreSQL 16 测试库已准备好时设置私有环境变量。
go test ./services/platform-api/internal/database -run 'TestInvitationPrecheck|TestInvitationFinalValidation' -v -count=1
```

没有 `PLATFORM_API_TEST_DATABASE_URL` 时必须 SKIP。当前没有真实数据库并发、锁释放、最终去重或受限权限运行证据，不能把测试代码存在视为这些验收已通过。Go race 仍待具备 CGO/C 编译器的 CI 或服务器，本轮未重试既有工具链限制。

## 升级与剩余工作

部署更新全部日本 API 与同版本 owner 引导工具；不新增迁移，不需要修改前端错误响应结构。Release A 保持单实例，避免新旧实例混跑绕过进程护栏。前端已有登录/注册/重置提示可显示该错误，不需要新的页面。

下一步本地开发是角色变化后的存量 session 策略。部署验收另需在隔离环境测量混合正常请求与认证突发下的 RSS、CPU、响应时间、取消释放和 AUTH_BUSY 恢复；同时使用受限 PostgreSQL 执行新增邀请 SQL 合同。真实域名、邮件、存储、下架链路与公网开放条件不因本次修复而通过。
