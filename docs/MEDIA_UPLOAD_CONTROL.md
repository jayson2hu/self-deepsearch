# 跨区上传暂停／恢复执行端

更新：2026-09-11。本页描述 Release A 的**手动授权执行链**，不是完整免费额度熔断器。手动执行端本身没有新增表；当前套件的 v21（62 表/5 公开视图）另已增加[后台授权队列与回执](./MEDIA_UPLOAD_QUEUE.md)，OpenAPI 为 87 操作/76 路径/106 Schema。没有增加服务。

## 当前完成了什么

```text
受控运维终端（日本 Go Worker 一次性命令）
  → 独立密钥签名、带快照条件的请求
  → 北京现有 Python 私有 HTTP 服务
  → 共享状态卷上的控制日志 + OS 锁
  → 所有已升级上传器在准入、每页 Listing、每次 PUT 前执行检查
  ← 绑定本次请求的签名回执：observed / applied / replayed
```

采用现有 Go 二进制和 Python 执行服务，避免为一个控制点增加常驻服务。单靠修改进程环境不能控制已运行的上传器；把北京直接接入 PostgreSQL 又会扩大权限和跨区依赖。因此本阶段选择 Go 显式下发、Python 持久执行，后续后台复用该合同。

日本 CLI 不连接 PostgreSQL、Analytics、SMTP 或 S3。北京不获取数据库/Analytics 凭据。命令不会修改公开资料、图片权利状态、缓存、账期或复核状态；**复核通过和账期切换不能自动调用恢复**。

后台 `/media/upload-control` 按钮、数据库队列、登录操作者关联、RBAC/近期认证 API 和回执落库已开发，见[队列与页面手册](./MEDIA_UPLOAD_QUEUE.md)。现另接入默认 off 的[逐操作用量准入](./MEDIA_UPLOAD_ADMISSION.md)：系统暂停写入同一日志/CAS 链，记录 v2 来源证据，不伪造人工请求。CLI 仍是持有专用密钥的受信任运维入口，不具备后台账号认证。受理请求不代表执行成功；准入启用后 CLI 恢复也须通过北京反向检查。

## 状态与并发约定

| 情况 | 上传行为与回执 |
| --- | --- |
| `off` 且从未建立控制状态 | 保持已有静态模式、共享上传锁和容量保护 |
| `enforce`，没有控制日志 | 视为暂停；只允许首次 `pause` 建立新 epoch，不能直接 resume |
| 已建立控制状态 | 重启后继续读取；仅把配置改回 off 不会清除保护 |
| paused | 拒绝新准入、下一页上传容量 Listing 和下一次 PUT |
| enabled | 只解除本控制点；静态停图、pending、容量校验仍可拒绝 |
| SDK 调用占有控制锁 | 立即返回签名 `503/control_busy`，不报告暂停生效 |
| 旧快照、旧恢复指令、重复 UUID 的不同内容 | `409/control_conflict`，不覆盖当前状态 |
| 最新同一 UUID、完全相同内容重试 | 不追加日志，重做持久化确认后返回 replayed |
| 日志损坏、不完整、超长、非普通文件或不可读写 | 拒绝，不擅自修复/重建或报告成功 |

每次控制操作在独立 OS 锁下比较 `expected_epoch` 和 `expected_generation`，成功后 generation 加一。第一条必须为 pause，epoch 取该命令的随机 UUID v4；后续指令必须引用它。旧 epoch 的命令不能用于新建状态。命令 UUID 不能被再次用于另一个指令；已被后续指令覆盖的旧重试返回冲突。

SDK PUT 从持久记录未确认对象到 SDK 调用返回，全程占有控制锁；因此不能在 SDK 调用尚未返回时抢先确认 pause。暂停可以在同一批次的两个 PUT 之间生效，下一次 PUT 不会发出。已成功上传的对象走现有核对回滚，未确认批次保留原恢复证据。

**这不是远端在途请求归零保证。** SDK 超时后供应商仍可能完成写入，另一个上传日志会保持 pending。`applied` 仅证明北京接受并持久化了这个控制状态，不证明所有图片、账单或远端写入都已停止。连续长 SDK 调用期间可能多次 busy，操作者需使用原命令稍后重试。

HEAD 校验、必要删除、权利下架、缓存清理、图片检查和对账接口不受此上传开关阻断；它们可能继续消耗供应商操作量。定时全量对账有自己的[用量准入开关](./MEDIA_TASK_ADMISSION.md)。公开网页、已知图片 URL、旧标签页和 CDN/源站请求不受这里控制。

## 私有协议与传输

两个新增接口位于北京已有 8090 私有服务，不加入面向浏览器的 OpenAPI：

- `POST /v1/upload-control/status`：请求体必须为 `{}`。
- `POST /v1/upload-control/apply`：请求体必须精确包含 `command_id`、`expected_epoch`、`expected_generation`、`mode`、`reason`、`resume_confirmed`。mode 为 paused/enabled；恢复必须显式确认，原因 2–1000 字，不含控制字符/密钥。

成功回执固定为 `status/epoch/generation/mode/command_id/applied_at`，时间为 UTC、精确到秒。HTTP 200 只有完整持久化之后才返回；本合同没有 202 或“排队即成功”。查询为 observed，应用为 applied，最近请求重试为 replayed。

专用 `MEDIA_UPLOAD_CONTROL_SECRET` 至少 32 字节，必须与既有 `MEDIA_HMAC_SECRET` 不同，只注入日本 Go Worker 和北京媒体服务。密钥仅来自环境，不支持 Go 命令行参数，不写入控制日志。删除/对账的旧 v1 签名不能用于控制。

控制请求 `u1` 签名包含协议域、POST、精确路径、时间戳、随机 32 字节 nonce 和原始请求体。服务端允许 ±300 秒，拒绝重复必需头、重复 JSON 字段、压缩/分块正文及大于 16 KiB 的正文。读取正文有 5 秒超时。

响应另以不同协议域签名，绑定精确路径、原请求时间戳/nonce/正文 SHA-256、HTTP 状态和完整回执正文。Go 强制 4 KiB/UTF-8/JSON/固定字段、命令 UUID、epoch、generation、模式与时间核对。重放上一次签名响应、伪造 200、篡改内容和重定向都不能确认成功。客户端总时间上限 8 秒，无自动重试。

HMAC 提供完整性与来源验证，**不加密公网 HTTP**；攻击者仍可能观察操作原因、阻断流量或重放有效窗口内的幂等请求。推荐固定 IP 防火墙配合 VPN/TLS；可使用 IP 证书或受控隧道，并不强制改为域名。Go 默认拒绝 HTTP，现有内网/隧道 HTTP 必须显式加 `--allow-http`。8090 不得暴露给公众或前端。

## 部署与操作

模板默认 `MEDIA_UPLOAD_CONTROL_MODE=off`，没有实际开启本功能或访问供应商。以后部署时：

1. 停止全部旧上传器，核对旧 PUT、pending、真实 bucket 和北京副本。禁止新旧上传器混跑，或从日本/控制台绕过受控适配器写入。
2. 北京所有 `serve`、`prepare`、手工/将来采集进程使用同一宿主机、Compose 项目、私有 `media-state` 卷和 `MEDIA_UPLOAD_LOCK_FILE=/var/lib/self-deepsearch/media-state/upload.lock`。
3. 全部北京实例升级并设置 enforce，注入独立控制密钥；缺日志时会暂停。日本升级 Worker 二进制，配置相同密钥及 `MEDIA_UPLOAD_CONTROL_URL`（只含 scheme、host、port，不能附 `/v1/...`）。先验证防火墙和可信传输。
4. 用 Go 查询 → 首次 pause 初始化 → 复核后显式 resume。不要删除状态文件来绕过暂停，也不要回退旧镜像恢复上传。

环境值通过私有部署文件注入。以下是部署后可运行的示例，本轮未运行真实网络请求：

```bash
# WORKER_REGION=japan，控制 origin/secret 已在私有环境设置。
platform-worker media-upload-control --live --action status

# 首次状态为 generation=0、epoch=""。这里及每个新操作都用新的 UUID v4。
platform-worker media-upload-control --live --action pause \
  --command-id <new-uuid-v4> --expected-generation 0 \
  --reason "Initialize paused control after uploader upgrade"

# 引用刚查询到的 epoch/generation，先确认额度、pending 和在途写入。
platform-worker media-upload-control --live --action resume \
  --command-id <another-new-uuid-v4> \
  --expected-epoch <observed-epoch> --expected-generation <observed-generation> \
  --reason "Reviewed usage, storage and all uploaders" --confirm-resume

# 普通暂停同样使用刚查询到的快照。
platform-worker media-upload-control --live --action pause \
  --command-id <another-new-uuid-v4> \
  --expected-epoch <observed-epoch> --expected-generation <observed-generation> \
  --reason "Pause uploads for capacity review"
```

明确采用受控 HTTP 时给每条命令加 `--allow-http`。没有 `--live`、区域不是 japan、缺快照或缺恢复确认都不会发请求。退出码 0 表示取得符合合同的回执；1 表示未确认远端结果；2 表示参数/配置错误，未发送。网络错误或回执丢失时不要直接换新 UUID 重做；先查询，或用**相同 UUID 和完整原请求**重试。发生 CAS 冲突后重新检查状态和操作意图，不自动套用新快照。

`resume_confirmed` 是运维确认，不是自动账户计量或 S3 检查。若原 `upload-status` 仍为 review_required，仍需按[未确认批次恢复流程](./MEDIA_UPLOAD_CAPACITY.md)核对；清 pending 也不能解除本上传暂停。

## 持久状态、容量与恢复

同一私有目录内新增 `<upload.lock>.control.lock` 和 `<upload.lock>.control.jsonl`。永久锁文件不得删除或替换；日志按控制操作追加，记录版本、epoch、generation、完整命令及 applied_at，fsync 文件和目录后才确认。日志拒绝符号链接/多硬链接/非普通文件，每次读取校验全部有序记录、时间和 UUID。

日志上限 1 MiB；enabled 指令另预留 16 KiB，避免最后一次启用耗尽暂停记录空间。此为少量人工指令设计，不适合高频策略循环。没有自动轮转：接近上限应先 pause，在所有上传器/控制服务停止的维护窗口保存并校验私有归档，重建时使用新 epoch，先保持暂停并重新复核；保留旧日志与时间，不能丢弃审计。日志部分写入/损坏时保留原件、停止服务并线下恢复，不提供在线强制 reset。

CLI 日志仍不证明后台用户身份。v21 队列通过同一人工 command ID 关联后台 actor、reason、request ID、期限和可信回执；北京日志不保存登录凭据。自动准入的系统事件没有 actor 或人工队列行，详情只在北京 v2 私有日志，日本后续可信观察可显示新暂停状态。旧 reader 不兼容 v2，升级/回滚须按[准入手册](./MEDIA_UPLOAD_ADMISSION.md)停止旧进程，不能删掉系统证据。用量复核 applied 回执不能替代上传执行证据。新队列还禁止远端 epoch/generation 回退：维护重建日志时须停止全部队列/上传器并独立审核 PG 与日志恢复点，不可仅换日志后继续运行。

## 未完成项与验证边界

- 后台按钮、数据库执行队列和操作者审计已开发；逐操作用量拒绝/自动暂停已接入，默认 off。空闲上传器不定时主动暂停，在途调用不撤销；真实 SQL/完整跨区业务联调尚未验证。
- API/Next 动态默认图和默认 off 的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)已开发；后者在对象缓存前执行策略，但真实 bucket 私有化、Worker route/R2 binding/purge 尚未验收，账户存储/GB-month 仍未完成。展示保护不能替代本执行端的上传回执。
- 本地已验证 Windows 锁/文件、真实 Go→Python HTTP 与 SDK 准入；S3 和 Cloudflare 为替身。真实北京 Linux 卷/权限、断电、公网传输、真实 SDK 重试和供应商在途写入尚待验收。
- 本轮不启动 Docker/PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics，不部署或开启此开关；Release A/M5 仍未完成，公网 no-go。

可复跑命令与结果见[验证证据](./evidence/release-a-media-upload-control-2026-09-11.md)。
