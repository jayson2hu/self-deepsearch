# 用量人工复核与账期切换

更新：2026-09-11。当前 Schema v21、62 表/5 个公开视图。后台入口 `/media/usage`；仅 owner/admin 可访问。本功能管理**观察状态**，不更改 `MEDIA_DELIVERY_MODE`，也不确认图片、上传或源站已停止/恢复。Release A/M5 未完成，公网仍 no-go。

## 采用的流程

比较了后台直接改状态、另设控制服务、现有 API 提交/Worker 处理三种方式。直接更新容易绕过高水位和审计；另设服务增加日本 2C4G 的部署与恢复成本。因此采用现有 Go API/Worker 与 PostgreSQL，不新增服务、连接池或北京数据库访问。

```text
管理员读取状态 → 近期密码确认 + 原快照 + 幂等键 + 显式确认
  → API 保存五分钟有效请求及请求审计（202：仅排队）
  → 日本 Worker 分钟 tick：角色/期限/快照/配置/风险复验
  → 同一事务保存前后证据、改变观察状态、记录 applied/rejected
  → 后台手动刷新查看结果

已接：日本 API/Next 按请求消费状态，异常 fail-closed 到默认图
已接代码：Cloudflare 私有 R2 网关在对象缓存/R2 前读取 edge policy
待验收：真实 bucket 私有化、Worker route/R2 binding/Cache API/purge、5 秒传播及恢复回执
```

观察器仍默认 `MEDIA_USAGE_MONITOR_MODE=off`。只有 observe 模式运行此 Worker 流程，后台不能启用观察器；关闭时提交的请求会等待并过期，后续启用才记录拒绝。不会为了处理复核请求访问额外供应商接口。

## 两个操作

独立的[手动跨区上传执行端](./MEDIA_UPLOAD_CONTROL.md)现已实现，默认 off。它由日本 Go 运维命令调用，不属于本文的复核队列，也没有从 acknowledge/rotate 自动调用恢复。北京 applied 回执与“观察状态 applied”含义不同；[后台授权队列/页面](./MEDIA_UPLOAD_QUEUE.md)现已分别记录请求和可信执行结果。新增[逐操作准入](./MEDIA_UPLOAD_ADMISSION.md)可持久化系统暂停，但不会因为本文复核 applied 自动恢复。

### 1. 异常复核（acknowledge）

只适用于异常已排查、后续收到有效统计的场景：

- 必须仍有复核标记，当前没有观察租约/运行任务。
- 当前 Worker 配置摘要必须与状态一致，统计及请求截至时间不能来自未来或超过 30 分钟，且仍处于该账期。
- 最后判定必须为 low_estimate/warning；同时根据高水位重新计算风险，不能只相信状态标签。达到 85% 的高风险或锁存停用建议不能通过这个操作解除。
- 只清除当前复核标记/原因/首次时间，保留全部计数、有效观察、账期、策略和先前审计。新的失败仍会重新进入复核。
- applied 仅表示上述观察状态事务已提交，不是恢复图片或确认免费额度的许可。

同一账期已耗尽预算不能靠较低新统计、改预算或反复确认清零；统计修正、账户迁移和预算更改不在此接口范围。

### 2. 账期切换（rotate_period）

1. 在供应商控制台确认新账期。必须显式填写含时区的 RFC3339 整秒时间；开始包含、结束不包含，最多 31 × 24 小时，不猜自然月。
2. 新账期开始不得早于旧账期结束，且必须包含当前时间；允许明确确认后的非重叠间隔，不伪造错过账期的数据。
3. 先更新**所有日本 Worker 实例**到同一账户、同一预算/预留/时间间隔策略、同一新账期，再刷新后台提交。多实例混跑旧配置会拒绝请求或重新标记配置异常，不能只改一台。
4. 新 Worker 在尚未批准时仍保留旧状态并报告配置不一致；不会自动清零。后台提交绑定旧状态的完整 config_digest/updated_at，Worker 必须核对新配置的起止与申请相同。
5. 切换保存旧账期完整快照，建立新账期零高水位，但 last_success_at/last_until 为空，强制保留 `usage_period_rotated / hold_for_review`。空计数不是已验证零用量。
6. 等待新账期有效统计后，另行提交一次异常复核；真实上传恢复另走已有授权队列和新鲜状态准入。日本 API/Next 会按请求重新读取有效状态，但还必须失效停图期间生成的目录缓存，才能避免默认图保留到原 TTL；Cloudflare 私有 R2 网关还需等待策略 TTL，并在真实 route/binding/Cache API 环境确认 normal 已恢复，不能仅凭复核 applied 宣称全部入口恢复。

正常只读 Analytics token 轮换不改变配置摘要，无需切换账期；本接口不接收或保存 token，也不改账户和策略。

## API 契约

| 接口 | 含义 |
| --- | --- |
| `GET /admin/v1/media/usage` | 同一只读快照内返回 state（未初始化为 null）及最近 10 条复核记录；计数为十进制字符串；不返回原始账户 ID、token 或完整前后快照 |
| `POST /admin/v1/media/usage/reviews` | 近期密码认证 + 管理员权限 + 确认；202 只表示已排队；完全相同的同用户/同幂等键重试返回 200 和原结果，不再创建请求 |

POST 字段：`idempotency_key`（UUID）、`action`、`expected_digest`、`expected_updated_at`、`reason`（2～1000 字）、`confirmed: true`；rotate_period 还需 `next_period_start/end`。快照时间原样回传，保留数据库微秒；显示用北京时间到秒，不能用页面格式化字符串重建并发令牌。

400 表示输入无效；401 要求登录/近期密码；403 权限失效；404 未初始化；409 表示旧快照、观察运行中、已有待处理请求或同键不同内容。网络错误/503/不完整响应不能解释为失败后可换新键，须保留原请求并查询/幂等重试。

页面保存不确定请求于当前页面内存，刷新状态不会丢弃它；重试使用完全相同的 JSON 与 key。页面重载/关闭后内存不保留，应先查最近记录和审计，不宣称浏览器已替用户取消服务端请求。后台不自动轮询或盲目重提。

## 数据库与权限

- `audit.media_usage_reviews` 保存操作者、幂等键、原快照身份、原因、Request ID、五分钟期限和处理结果。全局最多一个 pending，同 actor/key 唯一；完成/拒绝后不可覆盖。API SELECT/INSERT，Worker SELECT/UPDATE，均无 DELETE。
- `platform.media_usage_state.last_review_id` 关联该回执。普通 Worker 更新仍不能降低高水位、清除复核或更改身份；授权变更必须与不可变请求的原身份及 before/after 完整快照一致。
- 状态与 applied 回执在同一事务提交。延迟约束触发器拒绝“只写 applied 回执、没有对应状态”的提交；任一步失败均回滚，不输出已处理。
- `platform.lock_media_usage_reviewer(uuid)` 是固定 search_path 的只返回布尔值的 SECURITY DEFINER 辅助函数。它对当前用户行持有 SHARE 锁，复核 active owner/admin；Worker 不因此获得邮箱/密码/session 的读取或 users UPDATE 权限。PUBLIC 无执行权限。
- API 对状态表仍只有 SELECT。PostgreSQL 的 `SELECT FOR UPDATE` 还要求 UPDATE 权限，因此 API 通过固定 search_path 的 `platform.lock_media_usage_review_state()` 取得单行锁及四个 CAS 字段；函数仅授予 API 执行，无参数、动态 SQL 或写入能力，不扩大 API 的状态修改权限。
- 领取与 API 排队沿用现有 advisory lock/READ COMMITTED，等待锁后重新读取；Analytics 网络查询不在事务内。角色变更后尚未处理的请求会被拒绝。首次请求的近期密码校验由 API 完成，排队授权只在之后五分钟内有效。
- 下架、账号、邮件等独立流程不依赖复核成功。没有引入能让公开用户清零额度的接口。

拒绝记录使用有限代码：`usage_review_expired`、`usage_review_stale`、`usage_review_busy`、`usage_reviewer_inactive`、`usage_schedule_changed`、`usage_rotation_config_mismatch`、`usage_review_not_recoverable`。数据库/供应商原始错误及凭据不写客户端或普通日志。

## 升级与验收

1. 保持观察器 off，备份后在一次性 PostgreSQL 16 验证 v1-v21 fresh/upgrade/down。迁移 20 保留旧状态，不自动创建/应用请求，不启用观察。
2. 部署 Schema v21 配套 API、Worker、运营站、公开站、新版 edge 与监控；API readiness 要求 21。动态展示依赖 API 去图、公开站逐文档策略读取和 edge 私有路径隔离，不能沿用旧公开站或旧 edge 制品声称已支持停图。不存在“只更新后台就能处理”的保证。
3. 真实库执行 `TestPostgresMediaUsageStateContract` 和 `TestPostgresMediaUsageReviewContract`：独立回环空库、受限 Worker、同一服务器 owner guard 和显式 `CONFIRM_MEDIA_USAGE_CONTRACT=disposable-database`。后者覆盖 API 角色插入、真实 Worker 切换/复核、旧计数证据、过期/旧快照/权限撤销、直接修改权限及虚假 applied 提交拒绝；不替代真实 HTTP/邮箱认证联调。
   另在独立 `self_deepsearch_migrate_test` 库执行 `TestPostgresMediaUsageAPIRepositoryContract`。它实际调用 API 仓储，不用手写 INSERT 替代：验证受限账号行锁、NOWAIT 竞争拒绝、无 UPDATE/公开执行权限、未初始化、CAS、同键重放、不同载荷及已有 pending 冲突、唯一请求审计。需同回环服务器的 `PLATFORM_API_TEST_DATABASE_URL` / `CONTRACT_DATABASE_URL` 及显式 `CONFIRM_MEDIA_USAGE_API_CONTRACT=disposable-database`，只清理本测试用量夹具，保留合成账号及追加式审计；CI 已加入门槛，但本轮没有执行真实 SQL 或远程 CI。
4. 验证拒绝后的修复流程、响应丢失后的同键重试、任务期间角色变更、全部实例配置一致性和 2C4G 资源占用。没有真实 SQL 依赖时测试 SKIP，不算通过。
5. 回退优先 roll-forward；Down 删除本轮复核证据，只适用于已确认可丢弃测试库，不能在业务库使用 DELETE/停用触发器“恢复”。

审计不自动清理，处理次数与长期增长应纳入磁盘容量监控。实际存储/GB-month、供应商权限/延迟、动态展示在目标 Nginx/Cloudflare/多实例下的缓存失效与恢复，以及真实 bucket 私有化、Worker route/R2 binding/Cache API/purge 和传播仍待验证。**不能据此承诺零费用或 Release A 已上线。**

本轮本地测试、修复记录和未执行范围见[验证证据](./evidence/release-a-media-usage-reviews-2026-09-11.md)。

后续已加入[定时对账准入](./MEDIA_TASK_ADMISSION.md)：启用该独立开关时，异常复核/账期切换后的状态会在下一次扫描准入中重新评估。这只可能允许新的定时对账，不是上传或图片恢复许可；无有效统计或保留 review/stop 标记时仍拒绝。
