# 账户用量观察：持久状态与定时查询

更新：2026-09-11。当前整体 Schema **v21**；观察层在迁移 19 新增两张表，迁移 20 增复核回执，迁移 21 另增独立上传控制队列/状态，总计 62 表/5 个公开视图。观察模块使用日本现有 Go Worker 与 PostgreSQL，本身只保存保护建议；现有消费者包括定时对账准入、授权上传恢复检查、默认 off 的[逐操作上传准入](./MEDIA_UPLOAD_ADMISSION.md)、日本 API/Next [动态展示策略](./MEDIA_DELIVERY_MODE.md)，以及通过鉴权 edge policy 读取同一有效策略的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)。北京在操作前查询并持久化系统暂停；日本 API 去除公开/用户目录图片地址，Next 对旧 ISR HTML 用逐文档 CSP 阻断远程图片，Worker 在读取对象缓存/R2 前检查策略。以上都是下游按需执行，不是观察器主动推送；离线代码已覆盖 Release A 计划内的已知公开 URL和旧边缘对象，真实 bucket 私有化、route/binding/purge 和传播仍未验证。真实 SQL 和供应商验证未完成；原观察层证据见[验证记录](./evidence/release-a-media-usage-state-2026-09-11.md)。

一次性命令和 Analytics 的计费限制见[只读查询](./MEDIA_USAGE_OBSERVATION.md)。该命令现可同时输出 Storage Analytics 日峰值/GB-month 估算，但存储观测仍不连接数据库、不进入本页的高水位、定时器、准入或恢复判断；此处持久状态继续只保存操作量。

新增独立的[定时全量对账用量准入](./MEDIA_TASK_ADMISSION.md)，显式开启后消费这里的状态，拒绝高风险/失效情况下的新扫描；它不修改观察状态，不恢复图片，默认 off。API/Next 动态展示与 Cloudflare 私有 R2 网关消费者均已开发；全面控制是否成立仍取决于真实派生桶关闭匿名入口、Worker route/R2 binding/Cache API/purge 与传播验收。

## 架构选择

比较了日本本地状态文件、额外缓存服务、现有 PostgreSQL。文件难以与多 Worker 调度、权限和数据库备份保持一致；新增缓存服务会增加 2C4G 的常驻资源和恢复路径。因此使用现有日本 PostgreSQL/Worker 连接池，不新增服务，不让北京直连数据库。

```text
日本 Go Worker
  → PostgreSQL 领取观察租约（短事务）
  → R2 Analytics 只读查询（不持有数据库连接/锁）
  → 同事务保存证据 + 高水位 + 复核/停用建议
  → 私有指标和告警

已接：管理员复核/账期切换 → API 请求 → Worker 校验 → 不可变回执
已接：定时对账、北京逐操作准入/授权恢复、日本 API/Next 动态展示按需读取状态
已接代码：Cloudflare Worker 在对象缓存/R2 前读取 edge policy，私有 R2 binding 不暴露 provider URL
待验收：真实 bucket 私有化、Worker route/R2 binding/Cache API/purge、5 秒传播与恢复确认
```

## 默认关闭

根级和日本模板均为 `MEDIA_USAGE_MONITOR_MODE=off`。北京、API、前端和 Python 媒体服务不注入 Analytics 凭据。关闭时账户/账期可以留空；只有在运维环境确认真实供应商、账户和只读权限后，才考虑：

```text
MEDIA_USAGE_MONITOR_MODE=observe
MEDIA_USAGE_PERIOD_START=<确认的RFC3339账期起点，包含>
MEDIA_USAGE_PERIOD_END=<确认的RFC3339账期终点，不包含>
MEDIA_USAGE_INTERVAL=15m
R2_ANALYTICS_ACCOUNT_ID=<账户标识>
R2_ANALYTICS_API_TOKEN=<账户级只读Analytics令牌>
```

占位符不能原样运行。不要把真实 token 放进文档、聊天、命令参数或公开文件；须在供应商控制台核实最小权限，不复用删除/缓存清理令牌。代码只能检查格式，不能证明真实权限范围。

- 仅 `off`、`observe`，没有可用的 `enforce` 模式。无效模式、北京启用、缺账号/令牌、无效时间/间隔会启动失败。
- 账期带时区、整秒、最长 31 × 24 小时。不会猜自然月、按上海日期重置或自动换账期。
- 每 5～15 分钟观察一次，默认 15 分钟；每分钟轻量检查持久调度。单请求 8 秒/整次查询 30 秒，无自动重试。
- 结构正确但已经结束/尚未开始的账期不让整个 Worker 启动失败：观察器写复核状态、不查询 Analytics；其他 outbox、权利下架和归档可继续。
- 风险预算沿用 A/B 100 万/1000 万、5% 预留、30 分钟本地新鲜度。命令行预算覆盖参数不会自动改变持久观察器。
- 本轮模板保持 off，未运行真实观察器或切换现网模式。

## 数据和事务

`platform.media_usage_state` 保存账户 SHA-256 摘要、明确账期、预算/间隔快照与稳定配置摘要；A/B/免费高水位、最后有效接收/查询截至时间；首次复核原因/时间、停用建议；下次观察时间、活动 run ID 和 2 分钟租约。令牌不入库、不参与配置摘要，正常轮换令牌不等于换账户。

`audit.media_usage_runs` 保存运行窗口、开始/完成时间、结构化有效观察/判定或有限错误代码。最多一条 running；完成/失败后不能覆盖。失败 observation 为 null，不能写成 0 伪装成功。

领取使用 READ COMMITTED、事务 advisory lock、拿锁后的新读，提交后才发 HTTP。完成再次锁定状态，核对配置、run ID、原窗口和本机/数据库 `clock_timestamp()` 的租约有效性；证据、状态与租约清除同事务提交，任一步失败不返回已提交状态。

中断留下 running；后续确认租约过期，写 `usage_interrupted`，保留高水位/复核标记，按原 next_poll_at 决定是否重试。找不到对应 running 证据时拒绝清除，不把不一致当恢复。

## 不自动恢复

| 情况 | 结果 |
| --- | --- |
| 尚无有效观察 | 读取时建议 hold，不解释为 0 用量 |
| 有效且不低于上次 | 更新有效观察、高水位、判定 |
| 任一计数、查询窗口或接收时间倒退 | 保留旧有效值，标记 `usage_counter_regressed`，要求复核 |
| 超时、未知操作、部分/缺失/截断响应 | failed，保留旧有效值，要求复核 |
| 默认到 95% | 锁存停用建议和复核标记，不证明已执行 |
| 后来重新得到有效或较低风险结果 | 可以记录新结果，不清除旧复核/停用标记 |
| 账户/账期/预算/间隔变更 | 配置摘要不符，要求复核，不覆盖原身份和高水位 |
| 账期结束 | 不再查询该账期，不自动跨月清零 |
| 定时器停止，只剩旧“低风险”记录 | 读取者动态检查 30 分钟有效期和账期，过期仍建议 hold |

数据库触发器禁止普通写入降低高水位、清除复核/停用建议或改写身份。迁移 20 仅允许绑定不可变请求及精确前后快照的授权转换；Worker 无 DELETE，API 对状态只读、对复核请求仅 SELECT/INSERT，public_reader 无权限。API 通过固定投影的专用锁函数取得行锁，不获得状态 UPDATE 权限。

**授权复核、账期切换、上传控制、API/Next 动态展示和 Cloudflare 私有 R2 网关已开发，但复核 applied 不等于全部入口已经恢复。** 见[复核流程与升级](./MEDIA_USAGE_REVIEWS.md)：owner/admin 近期密码、显式确认与幂等请求由 Worker 校验处理；上传恢复须走授权队列，页面恢复须重新得到新鲜安全状态并完成目录缓存失效；边缘恢复还要等待策略 TTL，并在真实 route/binding/Cache API 环境确认。不要删除状态、降低高水位、停用触发器或重新开放匿名 R2 绕过。新空账户需要受控初始化，不能为了制造非空 Analytics 填入假计数。

## 私有指标

沿用原 Bearer `/metrics`，数据库读取最长 2 秒、只读无行锁；公开 health/ready 不读取/暴露此状态。观察异常不停止独立 outbox 和权利下架。

- `self_deepsearch_media_usage_monitor_enabled`：观察器启用状态。
- `..._state_available`：是否读到匹配配置的持久状态；失败不输出假零计数。
- `..._review_required`、`..._stop_recommended`：持久复核和停用建议。
- `..._hold_recommended`：叠加读取时过期/缺失后的建议。
- `..._observation_age_seconds`：请求截止时间距今，不是供应商完整水位。
- `..._class_a_highwater`、`..._class_b_highwater`：分析估计高水位；首次未成功时不输出。

四条相关告警：状态不可读 5 分钟 warning；复核/陈旧/缺失建议 5 分钟 warning；停用建议 1 分钟 critical；动态展示启用但策略输入不可用 1 分钟 warning。前三条文案明确只是观察建议，不代表已经执行；第四条对应 API/Next fail-closed 到默认图。指标不包含账户/token/bucket/对象/用户/URL 标签，并以 `media_default_only` 区分有效执行态。

## 升级和验收

1. 保持 off，按原流程备份；在一次性 PostgreSQL 16 验证 v1-v21 fresh/down 和受限权限。此步骤本轮未执行。
2. 补齐迁移 19、20、21，再部署配套 API/Worker/运营站/公开站和 edge。API readiness 当前要求 21；迁移和新制品默认不启用观察、上传准入或动态停图，也不更改业务图片。
3. 核对真实账户/账期/权限，先用一次性命令与控制台比对，包括空数据、未知操作、延迟和查询范围。确认后才考虑 observe。
4. 实测单一调度者、中断留痕、低计数不清标记、完成不可覆盖、API 状态只读/请求可插入、公开拒绝和指标/告警送达。
5. 回退应用前关闭观察，保留证据；不能只回退 API 镜像忽略 Schema 门槛。Down 会删除新证据，仅用于确认可丢弃测试库，正式环境优先 roll-forward。

`TestPostgresMediaUsageStateContract` 已排在 CI 其他 Worker 合同之后串行执行。要求独立 `self_deepsearch_worker_test`、受限运行账号、同回环端口 owner guard、`CONFIRM_MEDIA_USAGE_CONTRACT=disposable-database`。本机没有配置时明确 SKIP，未触发远程 CI；替身不能证明真实 SQL。

Schema v21 还需执行真实 API 仓储锁/CAS/幂等合同及 Worker 复核/账期切换合同，专用库、确认开关及权限要求见[复核验收](./MEDIA_USAGE_REVIEWS.md)。观察合同通过不能替代复核事务验证。

只复用日本 Worker/PG 的现有 3 连接池，无新增监听/常驻服务；网络查询不占池连接。默认每日约 96 份正常观察，审计保留不自动删除；目标内存/CPU和长期磁盘增长尚未测定，仍需按 2C4G 限额验证。

## 未完成

仍需存储 GB-month/全部账户消耗的可信口径、供应商权限/延迟验证、复核/切换真实 SQL 验收，以及 API/Next/北京执行端与 Cloudflare 网关在真实多实例、bucket 私有化、Worker route/R2 binding/Cache API/purge、Nginx/Cloudflare 和两地环境中的联调。Release A 当前唯一非必要自动对象任务是全量对账，已接双阶段准入；扩展约束见[对象存储任务策略](./MEDIA_STORAGE_TASK_POLICY.md)。**Release A/M5 未完成，公网 no-go。**
