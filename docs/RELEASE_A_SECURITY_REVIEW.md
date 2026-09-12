# Release A 安全审计

更新时间：2026-09-11（Asia/Shanghai）  
范围：公开资料站、运营后台、Go API/Worker、PostgreSQL、邮件、图片处理与部署入口。自动采集、广告联盟和头像识别不在 Release A 范围。

## 发布建议

- 本地代码与静态部署包：**conditional-go**。此前生产依赖审计 high/critical 为 0，认证、权限、输入边界、日志脱敏和浏览器安全头已有自动门槛；退出确认、密码计算护栏、角色变化后旧会话撤销已完成本地回归，密码资源及角色/邀请 SQL 仍待目标验证，不能等同于整体验收完成。
- 真实测试环境：**可进入**。必须使用测试凭据和非生产资料，按运行手册逐项验收。
- 公网开放：**no-go**。最终域名、Cloudflare、后台固定 IP/VPN/Access、真实邮件与存储链路、安全联系人和事件响应演练尚未完成。

最新图片复核：在既有登记/公开隔离/删除幂等基础上，已接入主图查询与预期旧资产校验替换、后台独立确认、共享保留及私有母版 30 天期限。主图替换接入时是第 18 个近期密码保护操作；后续用量复核与上传控制命令加入后，当前合计 20 个。事务失败/状态变化不误报成功，客户端不自动覆盖。三组替换与五组退出 SQL、真实存储删除均未验收，P1 不关闭。v17 仅隔离数据库目录；新[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)代码要求派生桶关闭匿名入口，并在对象缓存前执行策略，但真实 bucket/route/binding/purge 尚未验收。见[主图替换证据](./evidence/release-a-media-primary-2026-09-10.md)。

2026-09-11 页面增量：上传控制入口与服务端页面按 admin/owner 隔离，真正权限仍由 Go API 强制执行。客户端按完整字段/有界成功正文校验回执，精确保留微秒快照；过期、未知或错误状态不能发起新操作，恢复需独立确认，未知提交只能复用原键。近期认证沿用同源路径并补截止信号，未向前端注入跨区密钥或新增持久凭据。原因转义、角色隔离、原请求重试和桌面/手机时序已加入回归，最终结果见[页面证据](./evidence/release-a-media-upload-ui-2026-09-11.md)。本地测试不关闭真实授权/供应商/源站验证门槛，也没有重新执行联网依赖审计。

2026-09-11 CSP 增量：运营后台全部业务页，以及公开站登录、注册、重置、邀请、退出和账号中心，现由 Next Proxy 为每个 HTML 请求生成独立 128 位 nonce，采用 `script-src 'self' 'nonce-…' 'strict-dynamic'` 并保持 `private, no-store`；登录与邀请从预渲染改为动态。公开目录继续使用框架所需脚本 `unsafe-inline`；全站及 Nginx 增加 `script-src-attr 'none'`，阻止内联事件处理器。首页后续为隔离构建期目录改为请求时渲染，但 CSP 兼容策略不因此自动变成 nonce 策略。production HTML 响应轮换、脚本 nonce、无浏览器 CSP 拒绝和完整 desktop/mobile 78 项均通过；内联 style 与公开目录脚本风险仍保留，因此 P1 只缩小、不关闭。详见[账号页面 CSP 证据](./evidence/release-a-ops-csp-nonce-2026-09-11.md)与[运行时目录证据](./evidence/release-a-runtime-catalog-2026-09-11.md)。

2026-09-11 Turnstile 增量：旧实现只信任 Siteverify 的 `success=true`，现已把注册/密码重置分别绑定 `signup_code`/`password_code`，并要求响应 hostname 与私密 `TURNSTILE_EXPECTED_HOSTNAME` 匹配；生产配置和部署检查拒绝 URL/端口/非法 hostname，并要求与 `SITE_URL` 一致。外部调用限制 5 秒、2 KiB token、16 KiB JSON 响应且禁止重定向，无效 IP 不发送；错误码、token 和 secret 不写日志。Go、前端和环境合同已通过，但真实 Cloudflare site key/最终域名未调用，公网门槛不关闭。见[Turnstile 绑定证据](./evidence/release-a-turnstile-binding-2026-09-11.md)。

2026-09-11 站点身份增量：公开首页、robots 和 sitemap 已改为运行时生成；生产 `siteURL()` 不读取构建期 `NEXT_PUBLIC_SITE_URL`，只接受无凭据、无路径/查询/片段的 `SITE_URL` HTTP(S) origin，公网非 HTTPS 拒绝。缺配置或非法配置时 robots/sitemap 以 500 fail-closed，不回退或输出 `127.0.0.1`/构建期域名。健康与故障两套 standalone HTTP 场景、构建产物扫描和缓存合同通过；最终 HTTPS/Cloudflare 尚未验证，公网门槛不关闭。见[运行时目录证据](./evidence/release-a-runtime-catalog-2026-09-11.md)。

2026-09-11 CSV 导出增量：作品预检错误报告曾只做 CSV 引号转义，恶意番号以 `= + - @` 或控制空白开头时，管理员用表格软件打开可能解释为公式。现对所有导出单元格做公式前缀中和，再执行双引号转义；文件仍只含服务端校验错误，不自动打开、不访问 URL、不提交原文件。行为单测覆盖危险前缀、逗号、双引号、BOM 和通过行排除，桌面/移动浏览器另从实际上传预检页面触发下载并读取文件复验，见[CSV 错误导出证据](./evidence/release-a-csv-error-export-2026-09-11.md)。

## 威胁模型

2026-09-11 动态停图增量：人工 `default_only` 仍是最高优先级；动态 `enforce` 只允许 API 以现有只读权限查询 `platform.media_usage_state`。无状态、过期/未来时间、复核/停止锁存、未知状态、仓储错误或 750ms 截止均停图。Next 只通过固定私有路径读取 `normal|default_only`，要求严格 JSON、匹配响应头和 `no-store`，1 秒内任一失败同样停图。该路径不进公开 OpenAPI，生产 API Nginx 对公网 `/internal/` 返回 404；配置只注入日本 API/公开站，不发送 Analytics 凭据。旧 HTML 不在 Proxy 中改写正文，而由完整 CSP 阻断远程图片请求；缓存失效后才清除 HTML/OG/JSON-LD/RSC 中的旧 URL。首页请求时渲染仍保留 300 秒 API 数据缓存与公共入口缓存，该取舍不能代替图片源站访问控制。

2026-09-11 准入增量：[逐操作上传保护](./MEDIA_UPLOAD_ADMISSION.md)使用独立密钥和固定 IP 私有通道，请求/响应分域签名并绑定 nonce；回复严格限时/限量/限字段，任何未知结果不发 SDK 操作。北京 v2 日志明确系统来源，不伪造 actor；同一 CAS 链使旧恢复失效，反向请求后复验原期限，pending 与必要删除保持独立。两端配置默认 off，最多两个并发检查，无新公开路由/服务；回环隧道上游仍需后续建立私有通道。95.4% 新 Go 模块覆盖与全量回归结果见[本轮证据](./evidence/release-a-media-upload-admission-2026-09-11.md)。真实 TLS/时钟、旧 reader 升级、Linux 卷和供应商延迟仍为未验收风险，不关闭公网门槛。

2026-09-11 上传控制增量：新增[私有跨区执行端](./MEDIA_UPLOAD_CONTROL.md)，专用密钥与删除协议隔离；请求/响应分别绑定协议域、路径、nonce、正文与状态，客户端拒绝 unsigned/篡改/旧响应/重定向/错误命令回执。北京日志严格 CAS、幂等、持久化后确认；SDK 调用占锁时不抢先确认暂停，pending 永不由恢复指令清除。实际本地 Go→Python 与文件/SDK 替身已验证，详见[本轮证据](./evidence/release-a-media-upload-control-2026-09-11.md)。

v21 增量已加入[后台 RBAC/近期认证队列](./MEDIA_UPLOAD_QUEUE.md)：命令 ID 关联 actor、原快照、期限与审计，API 无执行状态 UPDATE，Worker 不能改原请求；短事务派发前持久记录，丢失响应保留 uncertain，旧租约不落完成。Python 追加前核对五分钟期限；数据库禁止目标/epoch/generation 回退和改写最终证据。恢复必须通过用量准入，复核不自动恢复。新增实际 SQL 合同已配置但本机 SKIP，不能视为权限/约束验收通过。

剩余风险：HTTP 没有机密性，手动 CLI 密钥持有者仍是受信任运维、CLI 日志本身不证明后台用户身份；同 UID 状态目录不视为不可信沙箱。须固定 IP 防火墙 + VPN/TLS、同步时钟、停止旧上传器、私有卷最小权限和日志恢复点。后台 UI 的本地合成验证不替代真实联调；逐操作上传准入、API/Next 动态默认图和 Cloudflare 私有 R2 网关均已接入代码（默认 off）。Next 私有策略限内网，edge policy 使用独立 Bearer；Worker 在对象缓存前 fail-closed，可控制新直连请求和旧边缘对象，但派生桶匿名入口关闭、真实 Worker/Cache API/purge 尚未证明，旧已打开标签页中已下载的字节无法撤回。Linux 断电、真实 SDK 在途写入及公网传输也未验收，公网仍 no-go。

本轮对账补充：不完整 JSON 曾被当作全 0 完成，已用本机 HTTP 复现并修复；当前只接受完整/有界且身份、计数、样本一致的报告。Python 限制样本体积但不减少异常计数，Worker 调度改为拿锁后新快照。实际 Go→Python 合同通过，真实 SQL 并发待验；每日注册公开状态/默认图检查已独立接入：固定 origin/路径、禁止重定向、5 秒/64 KiB 与发布文件哈希约束，不访问记录 URL，不自动改图。真实 SQL 与 bucket 权限未验收，风险不关闭。见[对账证据](./evidence/release-a-reconciliation-2026-09-10.md)。

核心资产包括账号与 session、邮箱验证码、运营审核权限、未发布资料、来源证据、图片母版、数据库、审计记录、备份及服务密钥。主要信任边界是互联网与 Cloudflare、日本入口与内部服务、公开站与运营后台、API 与 PostgreSQL、日本与北京 HMAC 服务、S3/R2 私有母版桶、私有访问的公开派生范围桶及 Cloudflare Worker binding。

重点攻击目标：批量注册或验证码轰炸、账号接管、越权审核发布、恶意 CSV/图片输入、SSRF、XSS/嵌入钓鱼、日志或错误信息泄漏、伪造北京删除回执、对象存储越权，以及绕过下架继续展示资料。

## 已验证控制

| 范围 | 控制与证据 | 当前结论 |
| --- | --- | --- |
| 身份认证 | Argon2id、HMAC 验证码、哈希 session、注册/重置/关闭邮箱验证、统一枚举防护、Turnstile hostname/action 精确绑定、邮箱/IP 限流；登录快照原子签发、重置 UUID 绑定、撤销时间保护；退出失败 503 留 Cookie，两站只接受 204且可重试 | 登录、退出和 Turnstile 本地 HTTP/外联边界回归通过；5 组 SQL 生命周期合同进入 CI，但本机 skip；待真实 PostgreSQL、Mailpit/SMTP 与最终域名 Turnstile E2E |
| 授权 | owner/admin/editor/user 分权；20 个高风险操作要求近期密码；审核领取人绑定和显式改派；审计查询及主图管理只允许 admin/owner，editor 返回 403 | Go、OpenAPI 与浏览器合同通过；待真实 PostgreSQL E2E |
| 输入与外联 | JSON/CSV 大小和字段限制；CSV 错误导出中和表格公式并完整引用；图片安全解码、去元数据；反馈证据 URL 不自动访问；采集关闭 | 离线测试通过；真实 PostgreSQL 导入事务仍待目标验收 |
| 数据与日志 | 三 Schema、独立运行账号、审计追加写；按 Request ID 查询最多 200 条，只披露操作者、动作、对象、理由和时间，不返回 revision 前后快照或原始 metadata；访问日志不记录路径、查询词、邮箱、IP 或底层错误 | 静态、单元与浏览器合同通过；待目标库权限和查询计划复验 |
| 浏览器与缓存 | CSP、frame 防护、nosniff、Referrer、Permissions Policy；运营后台和公开账号页请求级 script nonce + `strict-dynamic`，全站 `script-src-attr 'none'`；首页/robots/sitemap 运行时生成，目录 API 数据和成功公共 GET/HEAD 响应仍按 5 分钟缓存，账号/API/搜索与退出页私有 no-store；搜索绕过 Next 缓存；退出拒绝假成功回执、跨站、重定向和无界等待 | 两站 build、HTTP 冒烟、Nginx 合同、9 项缓存合同及 Playwright 39 场景/桌面移动 78 项通过；公开目录框架脚本 CSP 与内联 style 风险仍单独登记 |
| 依赖 | 根 override 固定 `nanoid 3.3.18`，修复 GHSA-2v37-7h3g-55p8；两个前端固定 `next/eslint-config-next 16.3.3`、`sharp 0.35.4`，避开 Next.js/sharp 已知 advisory；CI 执行 high/critical 生产依赖门槛 | `npm audit --omit=dev --audit-level=high` 为 0 |
| 密钥与网络 | Compose 采用服务级环境变量白名单；`.dockerignore` 排除 env、证书/私钥、metrics token、缓存与运行数据；前端镜像使用 lockfile + `npm ci` 且以非 root 运行；API/Worker/前端/媒体镜像均声明回环健康检查；E2E 运营密码只从环境变量读取；部署探针的 metrics/Cloudflare Access 凭据也只从环境变量读取、Access 头仅发送到后台且禁止自动重定向；Nginx 覆盖可信 `CF-Connecting-IP`，Next 同源代理继续传给仅在生产信任代理头的 Go API；后台规划固定 IP/VPN/Cloudflare Access | 静态、Go HTTP 与反向代理合同通过；Cloudflare 出口防火墙和真实入口尚未验收 |
| 下架与恢复 | 权利下架撤销发布和搜索，远端确认后物理删除；备份需北京复制和空库恢复后才能 verified | 代码与离线测试通过；真实环境待验收 |

2026-09-10 账号生命周期审查发现的旧快照签发、同邮箱新旧账号验证码边界和撤销时间约束风险，已完成代码加固和本地测试；**未在真实 PostgreSQL 复现或验证 SQL 并发**。Go vet/test、171 项 Python + 73 个 subtests、11 项 CI 安全合同通过。具体边界、复验命令和 API 替换要求见[账号生命周期证据](./evidence/release-a-identity-lifecycle-2026-09-10.md)。

随后完成退出确认修复：Go 8 个 HTTP 子场景、两站 32 项浏览器回归、当前 Go→Next 7 场景及最终 Python 172 项 + 73 个 subtests 通过；保留旧会话直到收到明确确认，错误页面不含账号信息。真实 PostgreSQL 幂等退出与最终入口仍未验收；具体 red→green 记录、Origin 例外边界、截图和配套升级顺序见[退出确认验证](./evidence/release-a-logout-confirmation-2026-09-10.md)。

## 风险登记

密码计算护栏已在本地实现：单进程 1 个计算、2 个等待、500ms 上限；队满/超时返回 503 AUTH_BUSY，不将超载当作密码错误；取消保留在途名额直到同步计算返回。邀请先校验再计算、落库重验，私有指标不带用户标签。Go/API 合同、真实 Argon2 突发峰值 1、Python 174 项 + 78 个 subtests 通过；实际数据库与目标内存边界未验证。详见[密码计算护栏证据](./evidence/release-a-password-work-2026-09-10.md)。

| 优先级 | 风险 | 负责人 | 截止条件 |
| --- | --- | --- | --- |
| P0 | 后台尚未启用固定 IP、VPN 或 Cloudflare Access | 部署负责人 | 公网开放前完成并验证未授权网络被拒绝 |
| P0 | 最终权利下架与安全事件联系人未配置 | 产品负责人 | 公网开放前替换 `.invalid` 占位并完成收件测试 |
| P0 | PostgreSQL、SMTP、S3/R2、北京副本和 Cloudflare 尚未真实 E2E；Turnstile 虽已本地绑定 hostname/action，但最终域名 site key 尚未验证 | 部署负责人 | 按 readiness 顺序全部通过并保留证据；最终 HTTPS 入口验证注册/重置的正确 action，并确认错误域名、错误 action、过期及重复 token 被拒绝 |
| P1 | 公开图片视图曾遗漏父资料发布状态；v17 已补 gate，但尚无真实 SQL/升级缓存证据 | 后端/CI 负责人 | 执行 22 种父资料/主站 publication 组合及隐藏/恢复的 public_reader 合同、v17 fresh/upgrade/down；维护窗口清理旧 Next/边缘缓存后验证，不把静态检查当作 SQL 通过 |
| P1 | 主图替换/30 天保留及逐对象退出已接入代码，但真实 SQL 并发与存储生命周期证据缺失 | 后端负责人 | 执行三组替换、五组退出 SQL，验证单一并发赢家、回滚、共享保护、期限提前、租约/旧 URL/草稿边界及真实幂等删除；dead 必须独立复核，不能用重复工单重置失败次数 |
| P1 | 对账报告误判已本地修复；调度锁后快照、每日公开状态与固定默认图巡检均已接入离线代码，但真实 SQL 调度、目标 HTTP、权限与告警尚未验收 | 后端/CI 负责人 | 串行执行新增调度 SQL，验证同周期单一运行；在目标入口验证公开状态矩阵、固定默认图内容/哈希及告警，不以文件清单或本地替身替代权限、发布边界和健康验收 |
| P1 | Argon2 进程级护栏及邀请预检已实现，但目标 384 MiB/0.40 CPU 资源边界未验证；新密码约 64 MiB，既有摘要校验最高允许 256 MiB，GC/其他请求仍占内存 | 后端/部署负责人 | 隔离环境验证混合负载 RSS/CPU、超载与取消恢复、高参数兼容；执行新增邀请预检/最终事务 SQL 合同。不能把单并发当作 RSS 保证，当前未复现 OOM |
| P1 | 角色变更旧 session 撤销已实现并完成本地事务/HTTP/浏览器回归，但真实 PostgreSQL 撤销与并发锁仍缺证据 | 后端/CI 负责人 | 受限一次性库执行新增两组角色合同：多设备撤销、原撤销证据保留、同角色 409、旧快照等待后拒绝、新登录 user 30 天/运营 8 小时。不能以测试替身或 SKIP 当作真实库通过 |
| P1 | 新账号生命周期 SQL 合同尚未实际执行，锁等待后重新验证、邮箱复用、时间约束和幂等退出仍缺数据库运行证据 | 后端/CI 负责人 | 在固定一次性 PostgreSQL 16 库用受限 API 登录执行 5 组合同；不授予 DELETE、不停用审计触发器，不以 SKIP 视为通过 |
| P1 | 运营后台与公开账号页已移除脚本 `unsafe-inline` 并使用请求级 nonce，全站禁止脚本属性事件处理器；公开目录的框架引导脚本仍含 `unsafe-inline`，两站内联 style 也尚未移除；首页改为运行时渲染并不自动消除此边界 | 前端负责人 | Release A 禁止任意 HTML 注入；在不破坏公开目录缓存与 Turnstile 的前提下评估构建期 hash、框架支持或边缘 HTML nonce，并在最终入口验证账号页多 CSP/Nginx/Cloudflare 行为；不能为追求表面合规退化 SEO/性能 |
| P1 | Alertmanager 服务和分级邮件路由已配置，但真实值守邮箱与 SMTP 送达尚未验收 | 运维负责人 | 公网开放前验证 warning/critical 与 resolved 通知均可送达 |
| P1 | 本机未启用 CGO，未本地执行 Go race test | CI 负责人 | CI 或服务器 `go test -race` 通过 |

## 事件响应

角色修复与证据边界见[角色会话验证](./evidence/release-a-role-sessions-2026-09-10.md)。角色更新、撤销、审计必须同事务；已撤销 session 的近期密码 token 不可绕过登录校验。此控制针对提交后的会话验证，不声称中止此前已鉴权的在途请求。

测试期按以下最小流程处理：发现异常后先停止相关写入或入口，保留 `request_id`、审计记录和精确时间；若涉及账号则撤销 session，涉及密钥则轮换并检查使用记录，涉及资料或图片权利则立即执行下架流程；恢复前复验受影响合同和真实链路。事件严重度分为 P0（账号/密钥/数据库或越权发布）、P1（持续滥用或核心链路失效）、P2（低影响异常）。

公网开放前必须指定一名安全/运维负责人、一名产品与权利联系人，并完成一次“撤销账号 session”和一次“资料及图片下架”的演练。敏感值只进入服务器私有 `.env` 或后续密钥服务，不进入聊天、Git、日志或工单正文。

## 复核命令

```powershell
npm run audit:production
npm run lint
npm run typecheck
npm run build
npm run test:frontend:smoke
npm run smoke:frontend:release-a
npm run test:e2e:release-a
python -m unittest discover -s infra/security/tests -p "test_*.py"
python -m unittest discover -s infra/reverse-proxy/tests -p "test_*.py"
```

依赖审计需要访问 npm registry；CI 不允许对 advisory 使用宽泛跳过或允许所有 high 漏洞。
