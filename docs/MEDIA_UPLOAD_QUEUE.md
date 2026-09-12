# 上传控制：授权请求、执行队列与回执

更新：2026-09-11。Release A 授权队列与后台操作页面已实现；运营后台入口 `/media/upload-control`，仅 admin/owner 可访问，执行器仍默认关闭。当前 Schema v21、62 表/5 个公开视图；OpenAPI 87 操作/76 路径/106 组件 Schema，20 个高风险操作要求近期认证。本次 UI 没有新增服务、迁移或接口。真实 PostgreSQL 与目标部署未验收，不改变公网 no-go。

## 范围与架构

```text
运营后台 /media/upload-control：admin/owner + 独立确认 + 近期密码认证
  → Go API：核对快照、确认、理由与幂等键；事务保存请求和操作者审计
  → 日本 PostgreSQL：audit.media_upload_commands（单一待处理请求）
  → 日本现有 Go Worker：领租约 → 查北京状态 → 保存派发记录
  → 北京现有 Python：核对签名/期限/epoch/generation → 持久化控制日志
  ← 双向签名回执；Worker 保存最终结果及最新可信状态
```

不新增服务、数据库或连接池。北京仍不直连 PG；API/前端没有跨区控制密钥。队列复用[手动执行协议](./MEDIA_UPLOAD_CONTROL.md)，与用量观察/账期复核分离：复核通过不能自动恢复上传。

新增[上传逐操作准入](./MEDIA_UPLOAD_ADMISSION.md)，默认 off：北京独立向日本现有 Worker 查询最新持久用量状态，拒绝/不可用时写同一 CAS 链的系统暂停。它不经过人工队列、不伪造 actor，也不受一条 uncertain 人工请求阻塞；旧恢复快照失效。队列后续可信观察可显示新的暂停状态，人工请求历史仍只显示真实人工操作，系统证据位于北京日志。恢复在日本 ResumeGuard 之外还要通过北京反向准入，并再次检查原有效期；不会清除 pending。

在小规格两机部署上，沿用现有 Worker + PostgreSQL 比增加消息队列和独立调度服务更便于维护；让 API 等待跨区执行则无法可靠处理响应丢失和重启，因此请求受理与执行结果分开。仅持一项开放请求，按分钟处理，不适合高频自动调节。

本轮只控制新上传的准入、Listing、PUT。不会删除图片、清除未知上传 pending、改变网站图片模式或限制直接图片 URL；权利删除、校验、账号和邮件仍独立运行。

## 接口与结果含义

- `GET /admin/v1/media/upload-control`：admin/owner 可读；未初始化 `state=null`，返回最近 10 项请求。状态的 `fresh` 只表示最近两分钟可信观察，不证明 Worker 此刻在线或整个网站停图。
- `POST /admin/v1/media/upload-control/commands`：必须登录、admin/owner、近期密码认证、同源校验、明确确认和理由。`idempotency_key` 为小写 UUIDv4；提供 GET 原样返回的目标摘要、epoch、generation 和 `last_success_at`，对应 `expected_observed_at`，不能截断微秒。
- `mode=paused|enabled`。generation 0 只允许暂停初始化；恢复必须已有控制链。原因去首尾空白后为 2～1000 字符，不允许控制字符。
- `202` 仅表示请求已落库；同键同载荷重试返回原请求和 `200`，可能仍在等待。不同载荷、过期快照、活跃租约、已有开放请求等返回 `409`，无状态 `404`；依赖失败 `503`，不输出底层错误或凭据。

| 状态 | 含义 |
| --- | --- |
| pending | 待处理，尚无派发证据 |
| running | 已先保存派发记录；不代表远端执行成功 |
| uncertain | 未确认。可能已执行、请求在途或远端不可读，不能当作失败后自动发新恢复 |
| applied | 收到精确匹配该 command ID、epoch、generation+1、mode 的可信回执；也可由后续状态查询确认 |
| rejected | 未派发请求被拒绝；已派发的请求只有超过保守过期确认窗口且远端快照未变，才可判定未应用 |
| superseded | 曾派发，但当前远端状态已被更新指令取代；不声称历史上一定未执行 |

请求与最终证据不可改写；actor ID、reason、request ID、时间、原快照、首次派发及次数都保留。`applied` 不等于用量安全，也不解除单独的上传 pending 或 `MEDIA_DELIVERY_MODE=default_only`。

## 后台操作与故障处理

页面将“最近确认状态”“提交操作”和“最近 10 项请求回执”分区展示。最新观察不证明此刻在线，历史 applied 不证明当前仍处于该模式；时间显示北京时间到秒，提交 CAS 保留服务端原始微秒，不从显示时间反推。

1. admin/owner 登录运营后台，从“上传控制”进入。匿名跳转登录，editor 不显示入口且直接访问会回到后台首页；真正写权限仍由 Go API 校验。
2. 刷新状态，核对最近观察、目标、epoch 和代次。未连接、缺字段、异常、过去超过两分钟或未来时间均不能发起新操作；页面每秒重新判断有效期，不依赖手动刷新来禁用旧快照。
3. 选择暂停或恢复，填写 2～1000 字操作原因，再勾选对应影响确认。首次未初始化只允许暂停；切换操作、修改原因、刷新或标签页可见性变化后需要重新确认。恢复确认明确不清未知上传、不解除其他保护，Worker 仍可能因用量拒绝。
4. 按 API 要求完成近期密码验证。202 仅显示“请求已受理，尚未确认完成”，不能把最近远端状态直接改为目标模式。pending/running/uncertain 存在时禁止创建第二个请求；按需刷新查看真实回执。
5. 提交超时、连接丢失或回执不完整时，页面内存保留完整原请求，只允许“重试同一上传请求”。刷新状态失败也不丢弃它；不会更换幂等键或套用新快照重发。明确 409 才清除旧确认并要求刷新核对。受理后刷新失败会同时保留受理提示与读取错误，不声称操作失败。
6. 原请求不写 localStorage，不保存密码或跨区密钥。离开/重载页面会丢失本地重试副本，但不会撤销服务器已受理的操作；重新进入先查历史，仍未知时联系运维，不能删记录解堵或另建恢复请求。

客户端 GET/POST 配置 5/15 秒请求截止信号，近期密码验证继承调用截止信号（其他后台调用未设置时使用 15 秒）。响应禁缓存/重定向，成功响应完整读取上限 128 KiB，验证 UTF-8、字段类型、时间、UUID、状态和回执关联；不把 HTTP 200 或部分 JSON 当作执行证据，不展示底层响应错误正文。浏览器计时限制不替代服务端五分钟期限。原因按普通文本转义展示。

桌面与手机布局、键盘确认、原请求重试和权限边界使用本地合成 API 验证。真实 API/数据库/北京执行端联调仍待完成，详见[页面验证证据](./evidence/release-a-media-upload-ui-2026-09-11.md)。

## 并发、失联与过期

1. API 在短事务内使用统一 advisory lock、操作者锁、固定投影状态锁函数和快照比较；没有执行状态 UPDATE 权限。请求和审计原子提交。
2. Worker 使用同一 advisory lock、数据库时钟 30 秒租约、随机租约 token。写观察、派发、结果时都复核 token、精确截止时间以及本机/数据库两个时钟。过期或释放的租约不能完成请求，HTTP 期间不持数据库事务。
3. 先查询北京签名状态，再保存派发记录，最后发 apply。只记录“已尝试”不代表远端已收到；连接失败或丢失回执统一保持 uncertain。
4. 下次先查询状态：精确匹配已尝试命令时直接落库 applied，不重发。快照变化、角色撤销、用量准入拒绝时不发送恢复；同一请求重试保持原 command ID 和完整内容。
5. 队列请求最长五分钟，协议 `issued_at/expires_at` 为严格 UTC 秒。创建时间保留数据库微秒，协议起点向下取秒；期限不会随重试延长。北京持有控制锁，完成日志读取和 CAS 后、追加前再次读取时钟。已过期但精确匹配最后一条记录的重试只能确认已有结果，不能重新应用。
6. 对已派发但状态未变的过期命令，额外保留 **310 秒**确认窗口（300 秒签名允许时差 + 有界 HTTP 余量），且必须取得新的可信状态，才可判为未执行。不可读则继续 uncertain。两地/数据库必须持续同步时钟；这不是在任意时钟跳变、旧未升级进程或恶意主机下的数学零风险保证。
7. 数据库拒绝目标替换、已观察 epoch 改变、generation 回退或同一 generation 不同回执。旧备份覆盖/日志重建触发异常，不能在线清状态或任意换 target 来绕过。

## 配置、指标与升级

日本现有 Worker：

```dotenv
MEDIA_UPLOAD_QUEUE_MODE=off
MEDIA_UPLOAD_CONTROL_URL=
MEDIA_UPLOAD_CONTROL_SECRET=
MEDIA_UPLOAD_CONTROL_ALLOW_HTTP=false
```

部署时才显式改为 `dispatch`；只允许日本。URL 仅 origin、不含路径/查询/用户信息。北京所有上传器须升级为支持有效期的版本并设置 `MEDIA_UPLOAD_CONTROL_MODE=enforce`，共用永久控制日志/锁与独立匹配密钥。环境检查拒绝日本 dispatch + 北京 off；HTTP 必须显式 opt-in，仍需固定 IP 防火墙和 VPN/TLS，HMAC 不提供加密。

恢复必须有日本 `MEDIA_USAGE_MONITOR_MODE=observe` 产生的可信状态，并通过既有 `TaskGuard`：未初始化、旧/未来快照、配置/账期变化、高水位达到风险门槛、复核或停用锁存、读失败均拒绝。未启用 observer 仍允许紧急暂停，恢复被拒绝；新的合规统计和人工复核不会自行发恢复指令。

私有 metrics 导出 queue enabled、state readable、snapshot fresh、open/uncertain commands，DB 查询限一秒，没有用户、指令或目标标签。不可读/陈旧持续 5 分钟、uncertain 持续 10 分钟分别告警。队列 off 不发起轮询；一项 uncertain 可阻止后续操作，需按回执核对，不能删行解堵。

升级顺序（本机未执行）：

1. 保持开关 off，保存 PG 和北京控制日志恢复点；隔离 PostgreSQL 16 验证 v1～v21 fresh/upgrade/down、受限运行账号与新增 SQL 合同。
2. 停止旧北京上传/控制进程，保留共享卷和永久锁，统一升级支持期限的 Python。旧版不认识额外字段应拒绝，而不是去掉期限重试。
3. 迁移日本数据库至 v21，替换配套 API、Worker、监控与运营后台制品；readiness/seed 要求 21。运营后台新页面依赖 v21 接口，先升级后端，不能仅发布按钮。公开站本次没有页面变更。
4. 配置两地密钥和可信网络、时钟及共享状态卷，开启 enforce，再开启日本 dispatch。空链先 pause 初始化；检查真实 SDK 在途/未知写入和额度状态后才人工确认恢复。
5. 多个日本实例可共享同一队列，但目标、观察策略、账期、secret 必须一致。当前 singleton 不支持多个北京上传目的地；更换目标或重建 epoch 需要先暂停、停止全部执行器和独立审核恢复方案。

回滚优先停队列并维持北京暂停，再 roll-forward；把 flag 改 off 不能自动恢复。迁移 Down 会删除新队列/状态证据，只用于确认可丢弃的隔离测试库，不能作为业务库解锁操作。手动 CLI 不带队列期限、也没有后台 actor ID，仅供受信任运维维护，不能用它伪造后台完成。

## 验证和剩余工作

本地已跑真实 Go→Python HTTP 的限时请求和“远端已应用但响应丢失 → 下次状态确认”的恢复；数据库/供应商仍是替身。Go 单元覆盖权限、快照、原子提交失败、租约、过期、恢复准入、回执与私有指标。

新增两个明确 opt-in 的真实 SQL 合同，已接既有串行 CI 配置，**本机 SKIP，不算通过**：

- API：`TestPostgresMediaUploadAPIRepositoryContract`，使用现有固定回环 `self_deepsearch_migrate_test`、受限 `PLATFORM_API_TEST_DATABASE_URL`、同服务器 `CONTRACT_DATABASE_URL` 和 `CONFIRM_MEDIA_UPLOAD_API_CONTRACT=disposable-database`。验证实际仓储、锁竞争、无状态 UPDATE、CAS/幂等、唯一请求与审计。只清理本测试队列/状态，保留合成账号和追加审计。
- Worker：`TestPostgresMediaUploadQueueContract`，专用空回环 `self_deepsearch_worker_test`，受限 `MEDIA_UPLOAD_CONTRACT_DATABASE_URL`、同服务器 `MEDIA_UPLOAD_CONTRACT_ADMIN_DATABASE_URL` 和 `CONFIRM_MEDIA_UPLOAD_CONTRACT=disposable-database`。验证真实授权派发、租约抢占拒绝/旧租约失败、未知→确认、无第二次派发、证据不可改写、状态不回退、角色撤销和最小权限。只删除作用域内合成数据。

后台状态/独立确认/回执页面、逐操作用量暂停、API/Next 动态停图和默认 off 的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)均已实现离线代码。Release A 的对象存储请求已按[任务清单](./MEDIA_STORAGE_TASK_POLICY.md)完成分类，当前没有第二个未接准入的非必要自动任务。下一步是存储账单口径，以及真实 bucket 私有化、Worker route/R2 binding/Cache API/purge、5 秒传播、Linux 卷与权限、PG、SDK/公网时延、跨区时钟/断电、2C4G 资源及告警验收；不保证零费用。后端历史验证见[队列证据](./evidence/release-a-media-upload-queue-2026-09-11.md)，页面和网关替身证据不替代真实数据库/Cloudflare 验证。
