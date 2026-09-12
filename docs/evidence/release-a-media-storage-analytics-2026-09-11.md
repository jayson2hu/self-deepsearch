# Release A：R2 Storage Analytics 只读估算验证

日期：2026-09-11（Asia/Shanghai）  
结论：一次性 `platform-worker media-usage` 报告已从 v1 操作量扩展为 v2 操作量 + 存储分析估算。本轮只完成离线代码、合成 HTTP 合同和文档，不连接真实 Cloudflare/R2，不改变 Schema v21、持久观察器、上传准入、图片模式或部署状态。Release A/M5 与公网仍 no-go。

## 实现范围

- 新增 `services/platform-worker/internal/mediausage/storage.go`：
  - 固定 Cloudflare GraphQL 目的地和只读环境凭据边界沿用原 Client；
  - 查询 `r2StorageAdaptiveGroups`，同时请求 `bucketName`、`datetime`、`payloadSize`、`metadataSize`、`objectCount`、`uploadCount`；
  - 不使用 bucket filter，不输出 bucket 名；
  - 同一时点先跨全部已观测 bucket 求和，再按 UTC 日期选择 `payload + metadata` 最大样本；
  - `observed_byte_days` 使用十进制整数字符串，`estimated_gb_month_microunits` 以大整数计算 `byte-days ÷ (30 × 10^9)`，向上取整至 0.000001 GB-month；
  - 只要出现 GraphQL partial error、多个/缺失账户、重复 bucket/时间、任一时点缺少已观测 bucket、任一 UTC 日期无样本、时间越界、字段缺失/null、负数/小数/指数/溢出、达到 10000 行或 HTTP/JSON 边界异常，就放弃整份存储观测。
- 抽取原操作量 HTTP GraphQL 传输到共用 `doGraphQL`，保留固定 HTTPS、8 秒 HTTP timeout、拒绝重定向、2 MiB 响应上限和无自动重试合同。
- 一次性命令报告升级为 v2，新增：
  - `storage_source`、`storage_policy`、`storage_observation`、`storage_assessment`；
  - 存储成功时 `storage_included=true`，失败时 observation 为 null、included 为 false、退出码 1；
  - 默认 `storage_class_unconfirmed`，不把 Standard 免费额度应用于未知/混合类别；
  - 仅显式 `--standard-only-confirmed` 后，使用 10 GB-month、0.5 GB-month 预留及 70/85/95% 分级；
  - 无论任何结果，`billing_verified=false`、`provider_watermark_verified=false`、`enforcement=not_connected`、`automatic_recovery=false`。

## 验证结果

在 `services/platform-worker` 执行：

```text
gofmt -w internal/mediausage/operations.go internal/mediausage/storage.go internal/mediausage/command.go internal/mediausage/storage_test.go internal/mediausage/command_test.go
go test ./internal/mediausage
go test -count=1 -json ./internal/mediausage
go test ./...
go vet ./...
go build ./cmd/worker
```

最终结果：

- `mediausage`：44 个顶层测试、219 个子测试，0 failed；
- platform-worker 全量 Go tests：全部通过；
- platform-worker `go vet ./...`：通过；
- Worker 二进制构建：通过，验证后已删除本地构建产物；
- 没有测试 skip 被本轮新增；
- 没有启动 Docker、PostgreSQL、SMTP、S3/R2 或 Cloudflare Worker；
- 没有执行 `media-usage --live`、真实 Analytics 查询、部署、远程 CI、暂存或提交。

新增用例覆盖：跨桶同时点求和、乱序行、UTC 日峰值、固定点向上取整、Standard 显式确认、存储预留停止建议、storage failure 命令退出、secret/account 不出报告，以及 malformed/partial/duplicate/null/越界/缺日/缺桶/行上限/所有整数异常和多种 64 位溢出。

## 仍未证明

1. Analytics 返回的全部 bucket、采样频率和时间点是否与真实账户一致；本地“完整 bucket 集合”只能拒绝观测内部缺行，不能证明供应商没有漏返回整个 bucket 或共享应用。
2. `payloadSize + metadataSize`、UTC 日界线、30 日分母和向上取整是否与供应商最终账单逐项一致；这是保守分析公式，不是计费合同。
3. Standard/Infrequent Access 或混合存储类别；`--standard-only-confirmed` 只是操作者声明，不是 API 验证。
4. 版本历史、计量延迟、供应商完整水位、免费额度向上取整、其他收费项目和控制台账单。
5. Storage Analytics 尚未进入 PostgreSQL、定时观察器、告警、动态停图或自动恢复；在这些风险未重新设计和验收前，不应新增 Schema v22 或消费该结果。
6. 真实 bucket 私有化、Cloudflare Worker route/R2 binding/Cache API/purge、5 秒策略传播和两地服务器联调。

因此，本轮关闭的是“只把当前对象字节数或最大单桶指标当账户存储量”的本地代码缺口，不关闭真实账单和硬限额风险。
