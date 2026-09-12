# 上传前用量准入与自动暂停

更新：2026-09-11。Release A 增量，默认关闭。复用日本 Go Worker、现有 PostgreSQL 用量状态与北京 Python 媒体服务；没有新增服务、数据库迁移或公开 API。当前仍是 Schema v21、62 表/5 公开视图。离线验证见[本轮证据](./evidence/release-a-media-upload-admission-2026-09-11.md)。

## 解决什么问题

人工上传控制原来只能依据管理员请求暂停。现在开启准入后，北京每次开始上传检查、容量扫描的每页 Listing、每次 SDK PUT 之前，都会向日本查询一次当前用量状态。只有完整、及时且通过签名验证的 allow 才能继续；拒绝、超时、配置错误或状态失效均阻止本次操作。启用/恢复指令也要经过同一检查。

这里的“实时”是每次操作读取**当前已保存的观察状态**，不是每次实时查询供应商。Analytics 仍每 5～15 分钟采样，可能延迟；观察器的 30 分钟有效期、账期与高水位规则不变。不得把这条链路描述成账户零费用保证。

```text
北京上传器（共享 control 锁）
    → 签名 POST → 日本现有 Worker:8081 /v1/upload-admission
                    → TaskGuard → 日本 PG 当前用量状态
    ← 请求绑定的签名 allow / deny
    ├─ allow：本次 SDK 操作
    └─ deny / unavailable：本地持久追加系统暂停 → generation + 1 → 拒绝操作
```

## 为什么用这条链路

比较过三个方向：

- 把自动暂停放进人工命令队列：需要单独的系统身份语义；一条结果未知的人工请求还可能挡住保护动作，不采用。
- 新建北京观察服务或轮询缓存：多一套常驻组件与过期窗口，也增加小服务器运维成本，不采用。
- 在既有上传边界同步查询：复用状态与连接池，不缓存许可，结果直接作用于现有持久控制链。当前人工录入、小批上传阶段采用此方案。

代价是每个检查约增加一个跨区 RTT（此前两机测得约 171～172 ms，非本轮实测），且检查/SDK 期间占北京控制锁。日本最多同时处理两个已认证检查，数据库读取截止 2 秒，不排队重试；第三个请求收到签名拒绝。复用原 3 连接池，不跨 HTTP 持有数据库事务。未来吞吐优化需独立设计有上限的许可预算，不能直接缓存 allow。

## 状态规则

日本复用 `mediausage.TaskGuard`：A/B 高水位达到 85%、到达预留停止线、复核/停用锁存、样本缺失/过期/未来时间、账期或策略不匹配、数据库失败均拒绝；具体阈值和语义见[作业准入](./MEDIA_TASK_ADMISSION.md)。检查本身不修改观察/复核状态，不调用 Analytics。

北京收到非 allow 时：

1. 当前 enabled：在同一 `.control.jsonl` 追加 `version: 2`、`source: usage_admission` 的系统暂停，包含请求 nonce、结果与已认证回复摘要；fsync 成功后才形成持久证据。未收到有效签名时摘要为空、结果为 unavailable。
2. 当前 paused：保持暂停；不会反复追加日志，也不会因新样本变好自动恢复。
3. 状态日志损坏、满、写入失败：本次操作仍被阻断，但不得宣称已成功持久化暂停；需保留日志并人工修复。

系统事件不带 actor_id，不插入冒充管理员的授权请求。它与人工命令使用同一 epoch/generation 链，自动暂停后旧快照的恢复请求失效。日本队列下次成功观察会保存新的远端状态；后台“最近确认状态”可能显示暂停，而人工请求历史不会伪造一条管理员操作。详细来源证据保留在北京私有日志。

`upload.pending.json` 与上述控制日志互相独立：暂停、复核、恢复都不会清除未确认写入。已派发的人工请求仍可能待确认；自动保护不会把它强行标记成功或释放它的未知结果。

## 配置与私有通道

| 位置 | 配置 | 启用要求 |
| --- | --- | --- |
| 日本 Go Worker | `MEDIA_UPLOAD_ADMISSION_MODE=enforce`、`MEDIA_UPLOAD_ADMISSION_SECRET` | 同时 `MEDIA_USAGE_MONITOR_MODE=observe`、`MEDIA_UPLOAD_QUEUE_MODE=dispatch` |
| 北京 Python | 同名 mode/secret，`MEDIA_UPLOAD_ADMISSION_URL`、`MEDIA_UPLOAD_ADMISSION_ALLOW_HTTP` | `MEDIA_UPLOAD_CONTROL_MODE=enforce`，所有上传器共用持久卷/锁 |
| API/两前端/北京 Go | 无准入密钥 | 不直接调用此接口，不接入 PG |

两端模式和密钥必须一致，密钥为独立随机值（32～4096 字节），不得复用删除、控制、缓存、认证或监控密钥；建议 32 个以上随机字节的安全文本编码。部署检查器验证模式依赖、秘密分离与 IP origin，不打印值；它不会证明网络可达或 TLS 配置正确。

URL 只允许固定 IPv4/IPv6 的 origin，不允许域名、用户信息、路径、查询、fragment、IPv6 zone、零端口或空白。这避免 DNS 解析突破总截止时间，符合当前两机 IP 互通的方案。HTTPS 必须校验覆盖该 IP 的可信证书；没有跳过证书校验选项。HTTP 仅在显式 `...ALLOW_HTTP=true` 且由 VPN/SSH 等加密可信通道保护时使用；HMAC 提供完整性，不提供内容机密性。

日本主 Compose 仍只 `expose: 8081`，无新增公网监听。可选叠加 `infra/compose/japan/upload-admission.tunnel.yaml`，只提供宿主机 `127.0.0.1:18081 → Worker:8081`，供已审核的私有隧道或 TLS 代理作为上游。该文件**不会自己建立两机网络通道**：后续部署需让北京 media 容器访问固定的隧道/VPN 对端 IP，将流量转到日本回环端口；北京容器内的 `127.0.0.1` 不是日本，也不是北京宿主机。代理只允许授权北京来源访问 `POST /v1/upload-admission`，保留签名头和原始正文，不加缓存、不重定向，不将整个 Worker/metrics 暴露到公网。不要直接放开 `0.0.0.0:8081`。本轮没有启动 Compose、隧道或修改防火墙。

本地 Compose 也不填默认准入 URL。将来需要启动联调时先确认容器可达的固定私有地址；不能填 `platform-worker-japan` 这类 DNS 服务名，也不能直接填浏览器 API 地址。

## 私有协议与资源边界

- 请求固定 POST、`/v1/upload-admission`、JSON `{}` 两字节；独立时间戳、64 位十六进制随机 nonce 和 `a1=` HMAC-SHA256，服务端允许时间差 ±30 秒。
- 请求签名域 `sd-upload-admission-request-v1` 绑定方法、路径、时间戳、nonce 和精确正文；回复域 `sd-upload-admission-response-v1` 还绑定请求摘要、HTTP 状态和精确回复正文。各字段以换行分隔，详见 Go/Python 实现与交叉语言测试。
- 成功解析只接受三个字段：`decision`、UTC 整秒 `checked_at`、`request_nonce`。200 allow 才能放行；签名 deny/429 均拒绝。未知字段、重复字段/头、错误类型、签名、状态、时刻和旧回复都不算许可。
- Python 总 socket 截止 3 秒，含连接/TLS/发送/慢响应头/正文；wire 上限 8 KiB、正文 1 KiB；无代理环境、重定向、自动重试或许可缓存。时钟偏差超过约 5 秒会失败关闭，需 NTP。
- nonce 用来绑定新回复，服务端不保存 nonce 去重表。重放已认证请求仍会重新读取**当前**状态，不能据此回放旧许可；私有通道、密钥和并发上限共同控制请求滥用。
- 恢复指令在反向检查结束后再次核对原五分钟有效期，不借检查延长许可；已过期恢复不会追加 enabled。
- 使用既有单机文件锁；运行中的 SDK 调用及其内部有限重试**不会被本次检查中途撤销**，后续操作会重新检查。检查与调用之间仍有正常状态变化窗口，这不是强一致的供应商计费预扣。

## 升级、恢复与回滚

1. 先保持 off，暂停所有旧上传器，按[上传控制手册](./MEDIA_UPLOAD_CONTROL.md)核对日志、pending、卷权限和备份；升级全部北京服务/命令。旧 reader 遇到 v2 系统记录会拒绝读取，不能混跑。
2. 验证日本现有 Schema v21、观察状态、队列合同与私有通道/时间同步，准备两端独立匹配密钥。日本 observer/dispatch/admission 与北京 control/admission 配套启用；变更 env 后需实际重启对应进程，模板修改不生效。
3. 无控制链时先通过原人工暂停流程初始化；检查观察/复核、pending 和最新远端快照后，明确提交恢复。恢复要通过日本原 ResumeGuard，以及北京新增的反向准入。
4. 发生自动暂停：先查用量/复核锁、时钟、网络、密钥与日志；修复原因后重新取快照、独立确认恢复。旧请求只按原有幂等/待确认流程处理，不强行套新 generation。
5. 降级到旧 Python 前先停所有上传器与队列并保留完整私有日志；不要删除/改写 v2 系统记录来“兼容”。优先前向修复；关 admission、关 control 或重启均不应清除已持久化暂停。状态损坏按离线维护流程处理。

## 监控与尚未完成

日本私有 `/metrics` 新增 `self_deepsearch_media_upload_admission_enabled` 和按 allowed/denied/busy 分组的计数器。告警提示“日本拒绝，检查北京执行证据”，不声称已经停掉整个站点。无法到达日本的请求不会计数；结合既有上传队列快照新鲜度和北京私有日志排查。指标没有图片路径、用户、账户或密钥标签。

本功能是**下一次操作触发**：空闲上传器不会被定时主动置 paused，已在途 SDK 调用可能继续；必要 DELETE/HEAD、下架和缓存清理仍独立运行并可能耗用额度。自动采集与广告联盟继续关闭。

后续的 API/Next 动态默认图与默认 off 的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)已完成离线代码。[对象存储任务清单](./MEDIA_STORAGE_TASK_POLICY.md)已确认 Release A 唯一非必要自动数据面任务是全量对账，且已有双阶段准入；未来新增任务必须先登记并接保护。仍需完成存储 GB-month/供应商账单口径，并验收真实 bucket 私有化、Worker route/R2 binding/Cache API/purge、5 秒传播、PostgreSQL/Analytics/Linux 共享卷/SDK/跨区 TLS 与部署。观察额度不等于账单；公网与完整 Release A 仍 no-go，不能以提供凭据替代剩余工作。
