# Release A 退出登录确认与恢复验证

日期：2026-09-10（Asia/Shanghai）。本轮没有启动 Docker，未连接真实 PostgreSQL、SMTP、S3/R2 或 Cloudflare，也未触发远程 CI。

## 已复现的问题与修复过程

- Go HTTP 红色回归证明：服务未配置、撤销错误、超时或取消原来仍返回 204；传给撤销服务的上下文也没有两秒期限。
- 当前旧版 Next production build 的 HTTP 回归证明：上游返回 503 后，退出路由仍跳到首页，且地址使用了内部 `localhost`，没有进入未确认状态。
- 加入严格 Origin 校验后的首轮完整浏览器回归为 20 passed / 12 failed。请求 trace 显示后台原生表单在保留的 `no-referrer` 策略下发送 `Origin: null`、`Sec-Fetch-Site: same-origin`、`Sec-Fetch-Mode: navigate`，被新校验误拒绝。修复采用下面的窄例外，不放行一般的空 Origin 或跨站请求。
- 同轮告警测试还碰到 Next 路由播报器的第二个 `role=alert`；定位现限定在主内容区。两处切换运营账号的旧用例增加退出完成等待。后台普通用户自动退出的错误导航改用 Next Router，消除新增 lint 警告。

## 最终合同

1. Go API 只有确认撤销成功，或请求本来没有 session 时，才返回 204 并清除 session/近期密码两枚 Cookie。撤销上下文最多 2 秒；服务不可用、数据库错误、超时或取消返回 `503 LOGOUT_UNAVAILABLE`，不清 Cookie。错误日志只保存 request ID 和分类，不保存 token、邮箱或底层错误文本。
2. 两站 POST `/auth/logout` 上游等待最多 3 秒，禁止跟随重定向，只接受 204。200 HTML/JSON、202、3xx、503、断连和不响应都不能被当成退出完成。
3. 失败使用 303 转到 `/logout?error=unconfirmed`，明确显示“退出尚未确认”、保留凭据以便重试、不自动重复请求。此处的 303 只表示跳到错误页面，不是业务成功确认。成功后公开站回首页、后台回登录页，才清两枚 Cookie。
4. Origin 正常情况下必须与实际 Host/入口协议完全匹配。Host 不取自 Next 归一化后的 `localhost` 或 `X-Forwarded-Host`。对于 `no-referrer` 原生表单，只有 Origin 精确为 `null` 且浏览器 Fetch Metadata 同时为 `same-origin`/`navigate` 才接受；缺失 Origin、一般 null、same-site、cross-site 仍拒绝且不联系 API。浏览器脚本不能自行设置 `Sec-Fetch-*`；入口不得伪造这些头。
5. 错误页面为动态、noindex/no-store，不依赖 `/me` 可用。Nginx 增加 `/logout` 缓存绕过，公开站 robots 增加排除。GET `/auth/logout` 不执行退出；无 session 的重复退出不依赖 API。
6. 普通用户误入后台后的自动退出同样检查 204；失败也进入可重试页面，不仅显示权限不足而忽略仍保留的会话。

远端可能已经撤销而回执丢失，因此错误页面只说“尚未确认”，不能断言会话一定仍有效。重复退出同一 token 必须幂等，不能覆盖首次撤销时间/理由。

## 实际本机结果

| 检查 | 结果 |
| --- | --- |
| Go API/Worker | `go vet` 与 `go test -count=1` 通过；显式启用的真实 Go→Python HTTP 合同继续通过 |
| API 交付编译 | `go build -trimpath` 生成最新 `.cache/core-e2e/platform-api.exe`，未启动该进程 |
| 退出 HTTP 专项 | 8 个子场景通过：无 Cookie、无服务且无 Cookie、确认成功、服务缺失、数据库错误、超时、取消、上下文已取消却返回 nil |
| PostgreSQL 生命周期 | 地址守卫通过；真实 SQL 明确 SKIP：`PLATFORM_API_TEST_DATABASE_URL is not set` |
| 前端 | 两站 production build、ESLint 无警告、TypeScript 检查和 OpenAPI 类型漂移检查通过 |
| 浏览器 | 完整 16 场景 × 桌面/移动，32 passed；1.9 分钟；包含 4 个新增退出场景组和原有注册、个人功能、邀请、异人审核、角色、反馈、下架流程 |
| HTTP 冒烟 | 两站入口、资源、安全头、广告边界、JSON-LD 和代理失败 no-store 通过 |
| Go→Next 缓存 | 当前 production build 的 7 个场景全部通过；[机器报告](./release-a-cache-after-logout-2026-09-10.json)，仍使用独立 standalone 副本和合成目录 |
| 工具合同 | 54 项工具合同、13 项前端烟测通过 |
| Python | 最终串行复验 172 passed, 73 subtests passed；56.22 秒；包含 12 项 CI 安全合同和反向代理缓存合同 |
| ruff | 相关安全与反向代理代码通过 |
| 文档收尾 | 产品/CI 安全/反向代理合同 63 passed, 2 subtests passed（7.31 秒）；OpenAPI 漂移检查通过，6 份本轮文档的 40 个本地文件链接均存在 |

Python 首跑在并行构建/浏览器负载下为 171 passed / 1 failed：备份哈希审计匹配用例触发原有 30 秒子进程超时。该用例单独执行为 1 passed（8.36 秒），随后整套串行通过。没有改备份实现、放宽超时、删除测试或跳过失败；本轮无法据此认定目标服务器负载已达标。

浏览器使用真实两站构建，但 API 为回环内存替身：故障控制仅通过测试进程对象传入，没有新增对外控制 API。替身会记录撤销状态，使旧 token 在重试成功后访问 `/me` 得到 401；这不是真实 PostgreSQL 会话状态的证明。

## 页面证据

四张全页截图均已逐张视觉复核，重试按钮和状态说明清晰可见，无横向溢出。桌面视口为 1366×900，移动视口为 390×844；全页图片可高于视口。

| 截图 | SHA-256 |
| --- | --- |
| [公开站桌面](./release-a-logout-2026-09-10/display-desktop.png) | `CF85B0DF6886ACB8AD2EF362E7FE6E93539A18A2B06676E56EC4FF1D7C5EE78C` |
| [后台桌面](./release-a-logout-2026-09-10/ops-desktop.png) | `502BD4889202B105C6730D2D201F08B85A96D11E1339A8F590110F562AC83371` |
| [公开站移动](./release-a-logout-2026-09-10/display-mobile.png) | `0979300049EE357AC73B8B528A3712B377E462CC783CB6081486F11A04230D8D` |
| [后台移动](./release-a-logout-2026-09-10/ops-mobile.png) | `CC4F2C979CD5B0FDB446720272007770D635513F5B5E999D032F1DB03B74F8EA` |

## 复验与部署

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-api/internal/httpapi -run '^TestLogoutRequiresRevocationConfirmation$' -count=1 -v
go test ./services/platform-api/internal/database -run 'TestIdentityContractDatabaseGuard|TestPostgresIdentityLifecycleContract' -count=1 -v
go build -trimpath -o .cache/core-e2e/platform-api.exe ./services/platform-api/cmd/api
npm run generate:api-types
npm run check:api-types
npm run lint
npm run typecheck
npm run build
npm run test:e2e:release-a -- --max-failures=1
npm run smoke:frontend:release-a
npm run test:cache:release-a -- --output docs/evidence/release-a-cache-after-logout-local.json
npm run test:preflight
npm run test:frontend:smoke
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
```

没有新增数据库迁移或 API 路径；OpenAPI 补充既有 logout 的 503 错误响应及 Cookie 保留语义。真实 SQL 合同新增幂等退出、无关 token 隔离、首次撤销时间/理由不变，现在共 5 组；继续只允许受限 API 登录和回环一次性 v16 库，不删除追加式审计，本机未执行实际 SQL。

发布顺序：入口 `/logout`/`/auth/` 禁止缓存规则 → 替换全部日本 API → 替换两站前端。旧 API 的假 204 或旧前端的错误吞噬都会破坏确认合同，混合版本不可作为放行状态。北京服务与 Worker 无需因本轮改动升级；前端代码不含生产凭据。

真实 Go API/PostgreSQL/浏览器完整链路、最终 Nginx/Cloudflare/TLS、CGO race 和部署资源仍待验收；HTTPS Host/协议用例只模拟可信入口头，不代表真实 TLS 已验收。生产依赖没有变更，本轮未重新访问 registry 做审计。密码计算资源护栏与角色变化后的存量 session 策略仍待本地开发，Release A 整体保持未完成。
