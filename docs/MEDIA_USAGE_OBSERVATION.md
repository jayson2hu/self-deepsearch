# R2 用量观察：只读查询与风险报告

更新：2026-09-11。Release A 的一次性 `media-usage` 子命令读取**账户级操作与存储分析数据**、给出分级建议；存储部分按同一时点汇总全部已观测 bucket，再按 UTC 日取峰值并生成整数定点的 GB-month 分析估算。该命令不定时运行、不修改图片模式、不连接数据库，也不是账单计量器。当前整体 Schema 为 v21：另有只保存操作量的[持久状态与可选定时器](./MEDIA_USAGE_STATE.md)，默认关闭；本轮没有把存储估算写入 Schema 或接入自动恢复。持久状态已有独立、默认 off 的[定时对账准入](./MEDIA_TASK_ADMISSION.md)和[逐操作上传准入/系统暂停](./MEDIA_UPLOAD_ADMISSION.md)消费者，不改变一次性命令语义，不等于动态页面停图或账单硬上限。

## 已核实的供应商信息

2026-09-11 通过正常 TLS 读取下列 Cloudflare 官方文档，无账号凭据、无供应商业务 API 调用：

| 官方来源 | 对本项目的影响 |
| --- | --- |
| [R2 Pricing](https://developers.cloudflare.com/r2/pricing/)（页面更新 2026-08-07） | Standard 免费额度为每月 10 GB-month、100 万 A 类、1000 万 B 类；Infrequent Access 不享受该免费额度。存储按日峰值在计费期平均计算，不能用当前两桶字节数替代 |
| [R2 Metrics and analytics](https://developers.cloudflare.com/r2/platform/metrics-analytics/)（2026-06-15） | `r2OperationsAdaptiveGroups` 可按 `actionType` 汇总 `sum.requests`；`r2StorageAdaptiveGroups` 提供 `bucketName`、`datetime` 及 `payloadSize/metadataSize/objectCount/uploadCount`；数据保留 31 天 |
| [GraphQL Analytics API](https://developers.cloudflare.com/analytics/graphql-api/)（2026-04-23） | 官方明确说明：分析数据**不应作为 Cloudflare 计费用量的衡量依据**。接口为固定 HTTPS GraphQL POST |
| [R2 Public buckets](https://developers.cloudflare.com/r2/buckets/public-buckets/)（2026-06-16） | 自定义域名与 `r2.dev` 是独立入口；禁用一个不等于禁用其余入口。入口控制仍须覆盖全部已启用通道 |

这些是公开文档事实，**不是用户账户已确认使用 R2、Standard、具体账期或具备相应 API 权限的证据**。仓库仍有 10 GiB 的当前对象保护默认值，本轮没有偷偷把它改成计费上限。真正用的是其他 S3 供应商时，不能套用本命令或 R2 的分类。

## 查询范围与失败处理

- 实现在 `services/platform-worker/internal/mediausage`，从现有 Worker 二进制进入，不新增常驻服务或依赖。
- 只在显式 `--live` 后访问固定 `https://api.cloudflare.com/client/v4/graphql`。分析 token 只从环境变量读取；不复用图片删除/缓存清理密钥，不支持命令行 token、任意 endpoint 或自动重定向。
- `--period-start` 必填，由操作者核对当前账户账期；程序不猜上海时区自然月、不自动跨月重置。`--until` 默认为当前 UTC 整秒，包含结束端点；窗口必须在最近 31 天内。
- 操作量使用官方公开的 `datetime_geq/datetime_leq`，按最长 24 小时拆分，最多 31 次串行请求。相邻窗口**共用一个端点**：精确落在端点上的事件可能重复计入，避免在两段之间跳过 1 秒时遗漏毫秒级事件。报告标记 `boundary_policy=inclusive_shared_endpoints`，因此数值是保守分析估计，不是精确账单。
- 存储量另发一次账户级查询，必须同时取得 `bucketName + datetime` 维度，不能把最大单桶的 `max(payloadSize)` 当账户总量。同一时点先对全部已观测 bucket 的 payload、metadata、对象数和未完成 multipart 数做 64 位安全求和；任一 bucket/时间重复、任一时点缺少本窗口内已观测 bucket、任一 UTC 日期无样本、字段/时间越界或达到 10000 行均整份失败。查询不会输出 bucket 名。
- 每个 UTC 日期从账户时点合计中选择 `payload + metadata` 最大的样本；相同总量选择较早样本，保证输出确定。`observed_byte_days` 用十进制整数字符串保存，`estimated_gb_month_microunits` 按 `byte-days ÷ (30 × 10^9)` 计算并向上取整到 0.000001 GB-month，全程不使用浮点数。该公式是分析近似，并不证明 metadata 的真实计费处理、供应商日界线、历史版本或账单舍入一致。
- 每次重查整个已指定账期，不能把多次月累计报告相加。一次性报告相互独立；持久观察器保留高水位并对计数倒退要求复核，不用较低新报告直接覆盖。单请求最多 8 秒，整次最多 30 秒，无自动重试；供应商限流或允许窗口比 24 小时更小时，明确失败，不缩小查询范围伪装完整。
- 每次只接收 HTTP 200、JSON、有效 UTF-8 和最多 2 MiB。GraphQL errors（即使同时有 data）、缺失/null 字段、多个账户、达到 10000 分组/行上限、重复字段/分组、负数/小数/溢出均拒绝。
- 操作名按已核实的定价列表分为 A/B/免费；**未知名字不会默认为免费或略过**，整次报告为 unknown，等待核对实际 schema/操作名。SDK 方法名不自动当作 Analytics 的 actionType 别名。
- 不筛选成功状态：相应已知操作的失败/重试也计入分析估计。DeleteObject 本身列为免费，但下架后的 HEAD 校验仍属 B 类；不能理解为整条下架链路完全免费。
- 操作量的单个安静日期可以为空；整期都无分组时按 `usage_no_data`，不以 0 假装已验证空账户。任一操作分段失败，放弃整个操作累计，输出 `observation: null`。存储查询不把缺失日期或缺失 bucket 行当 0，失败时输出 `storage_observation: null`、`storage_included: false`，命令退出 1。
- 日志/错误/报告不包含 token、account ID、请求 URL 或供应商原始错误正文。命令不读用户、作品、访问历史或图片内容。

## 风险建议，不是执行状态

默认 A/B 预算分别为 100 万/1000 万，分别预留 5 万/50 万（5%）。预算和预留可通过命令参数调整，不能据此认为供应商也更改了免费套餐。

| 任一操作类的分析估计 | 报告建议 |
| --- | --- |
| 低于 70% | `low_estimate / observe_only`，仅继续观察，不是账户可安全放行 |
| 达到 70%，低于 85% | `warning / review_usage` |
| 达到 85%，尚未侵入预留 | `high / pause_nonessential` |
| 当前估计 + 预留 ≥ 预算（默认 95%） | `stop_recommended / stop_media_review_required` |
| 不完整、失败、错账期、未来时间、查询截至时间或接收时间超过 30 分钟 | `unknown / hold_for_review` |

5% 仅是当前离线风险策略默认值，并非根据真实吞吐/供应商延迟测得的充足预留。它让停止建议先于名义 100% 出现，但不预留真实请求名额，不阻止其他应用继续消耗账户额度。

存储策略使用官方 Standard 免费额度 10 GB-month 和 0.5 GB-month（5%）预留，但**默认不进行免费额度分级**：只有操作者另外传入 `--standard-only-confirmed`，明确声明该账户所有 bucket 均使用 Standard 类别，才会按相同 70/85/95% 阶梯输出 `low_estimate/warning/high/stop_recommended`。未声明时固定为 `estimate_only / storage_class_unconfirmed / verify_standard_only_and_billing`。该开关只是操作者声明，报告仍固定 `billing_verified=false`，不能证明供应商配置、共享账户范围或真实账单。

报告固定带有：

```json
{
  "billing_verified": false,
  "provider_watermark_verified": false,
  "storage_included": true,
  "enforcement": "not_connected",
  "automatic_recovery": false
}
```

上述 `storage_included: true` 仅表示本次报告保留了一份通过本地结构校验的存储观测；查询失败时为 false。`received_at` 是本机收到结果的时间，`end_inclusive` 是请求的截止时间；两者**都不是供应商已完整计量到该时间的水位**。30 分钟检查只排除明显过旧的查询，不能证明上游没有延迟或采样误差。操作或存储的 `low_estimate` 都不能触发恢复正常图。持久状态仍只保存操作量，本轮存储结果只存在一次性 JSON 输出中。

退出码：0 表示报告生成且未给出停图/unknown 建议（仍可能 warning/high）；1 表示未知、建议停图或输出失败；2 表示参数/显式授权/凭据配置缺失。退出 0 不等于 Release A 验收通过。

## 后续有凭据时的只读检查

本轮没有执行 `--live`。帮助可完全离线运行：

```powershell
go run ./services/platform-worker/cmd/worker media-usage --help
```

未来仅在本地/日本运维环境安全注入 `R2_ANALYTICS_ACCOUNT_ID`、`R2_ANALYTICS_API_TOKEN`；选择只允许读取该账户 Analytics 的最小权限，具体控制台权限名称和套餐访问能力须现场核实。不要把 token 粘贴到文档、聊天、命令参数或报告中；只使用一次性命令时无需给常驻 Worker 配置 token。启用持久观察时仅日本 Worker 获得专用只读 token，前端、API 和北京均不获得。

确认真实账期后使用下列形式，尖括号占位符必须替换，不要原样运行：

```text
platform-worker media-usage --live --period-start <已确认账期起点的RFC3339时间>
```

只有现场确认该账户全部 bucket 都是 Standard 时，才追加 `--standard-only-confirmed`；使用 Infrequent Access、混合类别或无法确认时不要传。无论是否追加，输出都不是账单凭据。

`--until` 只适合短延迟的当前报告或诊断；查询旧历史超过 30 分钟会得到 unknown。对于新建空账户，`usage_no_data` 是预期保守结果，需要与控制台核对，不能为让测试通过而填入假用量。

Compose 已预留日本 Worker 的观察配置，默认 `MEDIA_USAGE_MONITOR_MODE=off`，不会自行查询。一次性命令本身仍不写数据库、S3、配置文件、审计状态或缓存，也不会更新持久观察器。操作者若自行保存报告，应放入受控运维目录而非公开静态目录。

## 完整目标尚需完成

1. 核实实际供应商、账户、Standard/其他存储类别、账期、API 权限、数据延迟/采样与操作名；与真实控制台核对。分析数据不足以证明账单零费用。
2. 两个专用桶的未完成 multipart 部件和单次扫描 256 请求预算已加入北京上传前容量核算；一次性报告也已增加全部已观测 bucket 的存储日峰值/GB-month 分析估算。仍需核实供应商版本历史、其他应用/未返回 bucket、Standard 类别、日界线、Analytics 延迟和真实账单水位，不能把本地估算接成硬限额或自动恢复。
3. 持久快照、失败/计数回退/过期的粘性复核和日本可选定时查询已实现，见[持久观察](./MEDIA_USAGE_STATE.md)；[授权复核与账期切换](./MEDIA_USAGE_REVIEWS.md)也已开发，仍须真实 SQL/供应商验证及实际恢复执行回执。不能从一份更低或新月份报告自动恢复。
4. 授权的动态策略下发、北京准入、定时对账用量准入、Next/API 动态降级及确认/审计/缓存失效。Release A 的全部对象数据面入口已登记到[任务策略](./MEDIA_STORAGE_TASK_POLICY.md)；以后新增非必要任务必须先扩展清单和准入。现有 `MEDIA_DELIVERY_MODE` 仍需配套重启，不可只改 env；观察定时器不是执行定时器。
5. 全部图片域名、`r2.dev`、直连/凭证入口及已有边缘缓存的停止与恢复；必须保留文字、账号和权利下架，不把新代理或收费产品未经评估加入部署。
6. 如果“绝不产生任何费用”是硬约束，必须另行核实供应商硬限额或等效的全入口请求准入；仅靠有延迟的监测和事后开关无法保证。

本轮是完整额度保护的数据观察基础，不替代上述未完成项。Release A/M5 及公网仍 **no-go**。原操作量基础见[用量观察验证](./evidence/release-a-media-usage-2026-09-11.md)，存储增量见[Storage Analytics 验证](./evidence/release-a-media-storage-analytics-2026-09-11.md)。
