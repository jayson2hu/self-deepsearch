# 每日图片检查：规则与运维

更新：2026-09-11。代码已接入，真实 PostgreSQL、对象存储、Cloudflare 和目标部署尚未验收。Release A 仍为测试阶段，本功能不启用自动采集、广告联盟或头像识别。

## 检查范围

这是日本 Go Worker 的独立任务，与 Go→Python 文件对账互补：

| 项目 | 检查内容 | 正常流程不误报 |
| --- | --- | --- |
| 注册对象 | scope、key 前缀/路径、派生类型、公开 URL 与 key 对应、当前版本、权利及资产/对象发布状态 | 已批准图片可提前用于草稿或普通隐藏资料；不会仅因父资料未发布而告警 |
| 公开关联 | 所关联资产类型，以及当前公开的 w320/w640/w960 三档派生 | 没有关联时允许页面用默认图；共享资产不因一个父资料隐藏而被撤下 |
| 退出对象 | 隐藏公开图 URL 已清空，并存在身份匹配的 pending/retry/running 删除任务 | 等待正常删除不算发布故障；保留的私有母版不要求提前删除 |
| 默认图 | GET `/default-work.svg` 和 `/default-performer.svg`，必须 200、SVG MIME、完整有界内容且匹配发布资产哈希 | LF/CRLF 换行兼容；不因 Windows/Linux 构建差异误报 |

每个异常对象/关联计一次；同一资产可能影响多个对象与关联，所以总数不是唯一资产数。样本按两类分别最多 50 个 UUID，完整计数不随样本截断。

注册对象的 URL 检查验证 HTTPS 结构与 key 路径对应，不逐张请求图片，也不探测任意域名；登记 API 另按配置的 S3_PUBLIC_BASE_URL 限制公开域名。每日默认图检查只覆盖两张固定兜底文件，不能解释为所有公开 URL 都已验证可读。

检查只读业务数据，不自动修复状态、覆盖图片、删除文件或重启死信。来源、反馈、作品记录中的 URL **不会被请求**。已物理删除却仍在存储中的对象、未登记对象、S3/北京副本缺失和哈希不一致，继续由 `media_reconciliation_runs` 文件对账覆盖。两者均不能证明 bucket IAM/匿名访问策略正确。

## 配置与资源边界

日本 Worker：

```dotenv
MEDIA_INSPECT_ENABLED=true
MEDIA_INSPECT_DISPLAY_ORIGIN=https://display.example.com
```

origin 由运维控制，只接受没有凭据、路径、查询和 fragment 的 HTTP(S) origin。两条默认图路径固定，不允许配置任意 URL 列表。本地模板默认关闭；日本部署模板默认开启，环境检查器要求开启。core 模式可用内网 `http://display-web:3000`；full 模式必须与 `SITE_URL` 同 origin，不能拿内部源站通过冒充公网 Cloudflare 通过。

- 固定每 24 小时一次；启动即查询是否到期，之后每 5 分钟查询。重启或增加 Worker 不应在同周期重复执行。
- PostgreSQL advisory lock 后使用 Read Committed 新语句快照决定到期状态；库存另用只读 Repeatable Read，网络探测前释放数据库连接。
- 对象与公开关联分别最多 10000 条；超限记 `inventory_too_large`，不以部分样本冒充完整巡检。超过该规模前须开发有界分页的一致性方案，不能只提高上限。
- 整次运行最多 45 秒；每张默认图最多 5 秒、读取 64 KiB。不带 Cookie，不跟随重定向；403、挑战页、HTML 错误页或伪装 SVG 不算正常。
- 运行中断超过 5 分钟，下一次调度检查把遗留 running 标为 failed/interrupted；不会补写成零异常的 completed。
- 每日两次默认图 GET，不消耗 S3 对象读额度；快照仍有数据库查询开销，真实 2C4G 的耗时/资源必须实测。

默认 SVG 哈希在 Worker 中是发布合同，测试与 `apps/display-web/public` 文件核对。主动修改默认图时必须同批更新 Worker 哈希和公开站制品，先跑合同；不允许静默接受任意 200 响应。HTTP 缓存可能仍返回旧制品，这会如实告警。

## 结果与告警

迁移 18 新增 `audit.media_inspection_runs`。只有 audit_writer（API/Worker 继承）可读取/登记，不对 public_reader 开放。保存固定 origin、运行时间、对象/关联与问题计数、UUID 样本、两个默认图 HTTP 状态和错误码；不保存响应正文、用户行为或账号信息。

管理员后台“每日图片检查”显示上次完成时间、发布状态问题、默认图失败数（0～2）和 24 小时任务失败数。无记录明确提示“尚未完成检查，不代表图片正常”；系统健康接口无记录或超过 2 天亦为 degraded。API 不向普通用户/editor 返回这些状态。

私有 Prometheus 指标：

- `self_deepsearch_media_inspection_age_seconds`：无完成记录时不输出，不能填 0；
- `self_deepsearch_media_publication_issues`、`self_deepsearch_default_image_failures`：最近完成检查的问题数；
- `self_deepsearch_media_inspection_failures_24h`：最近 24 小时落成 failed 的次数。

规则分别覆盖从未完成、超过 2 天、公开状态/默认图异常（critical）和检查任务失败（warning）。目标 Prometheus/Alertmanager 的规则加载和通知送达仍待演练。

受控运维连接可查询最近记录，不要把 origin、对象 UUID 样本贴到公开工单：

```sql
SELECT run_id, run_status, display_origin, started_at, completed_at,
       object_count, link_count, publication_issues, default_failures,
       error_code, issue_samples, default_results
FROM audit.media_inspection_runs
ORDER BY started_at DESC
LIMIT 10;
```

先确认任务是否完成、origin 是否正确，再按样本核对资产/对象/关联和删除 outbox。默认图失败则核对路由、静态资源发布、MIME、访问策略与哈希。修复后手工确认，并等待下一次正常周期刷新指标；重启不会绕过 24 小时门槛，不删除旧审计来强行制造绿色状态。死信和权利撤下继续走已有人工流程。

## 升级与验收

1. 保留恢复点，在隔离测试库执行当前 v1～v21 fresh/upgrade/down、运行账号与 SQL 合同；缺库 SKIP 不算通过。
2. 每日检查由迁移 18 引入，当前套件还包括观察状态迁移 19、复核回执迁移 20、上传回执迁移 21：维护窗口内先补齐至 v21，再部署配套 API、Worker、运营站和监控规则。当前 API readiness 只接受 v21，不能只更新数据库或前端后保持旧实例运行。公开站默认图未改变时可沿用原制品；改变默认图必须同批交付。
3. 首次检查验证审计落库、后台状态及指标，再模拟默认图 404/恢复、身份不匹配的删除事件、并发调度和中断。公开 bucket 的匿名/private 隔离、S3/北京/Cloudflare 物理删除与资源用量单独验收。

CI 在专用 `self_deepsearch_worker_test` 中，串行执行 outbox → 文件对账调度 → 每日检查 → 用量观察状态。手动运行需 `MEDIA_INSPECT_CONTRACT_DATABASE_URL`、`MEDIA_INSPECT_CONTRACT_ADMIN_DATABASE_URL` 和 `CONFIRM_MEDIA_INSPECT_CONTRACT=disposable-database`；只接受同一回环主机/显式端口的固定空测试库，不创建或清空业务库。

```powershell
go test -count=1 -timeout=90s -v ./services/platform-worker/internal/mediainspect -run '^TestPostgresInspection.*Contract$'
```

并发合同使用真实受限 LOGIN，观测两个锁等待者后仅允许一个运行，并校验完成不可覆盖；状态矩阵在事务内切换到受限 Worker 角色查询，所有合成变更回滚。当前本机二者明确 SKIP。迁移 18 Down 会丢弃新增审计表，只允许一次性测试库，不作线上故障处理。
