# 人物候选批次接收 v1

2026-09-22：主平台新增版本化人物元数据接收接口、来源绑定凭据、事务暂存与幂等回执。已使用独立 PostgreSQL 16 和固定归档验证，并在专用 loopback 环境完成双项目网络与后台验收；主部署默认关闭，未替换现有 review 或生产环境。正式人物映射、独立审核发布、图片隔离存储及定时调度仍待后续开发。

后续联调已补齐采集端客户端及后台只读验收入口，实际网络/浏览器结果见[双项目验收](COLLECTOR_E2E_ACCEPTANCE.md)。

## 两个仓库的分工

`self-deepsearch` 管理正式平台、接口、数据库及审核发布；同级 `self-deepsearch-collector` 独立维护来源连接器、SQLite 数据基座、归档和媒体暂存。它们通过版本化数据合同交接，不自动同步代码。2026-09-19 的[固定归档联调](COLLECTOR_INTEGRATION_ACCEPTANCE.md)保留为旧工具验收；本次新增通用的元数据接收接口，旧预览仍只展示它原来的固定测试来源。

## 接收合同

`POST /internal/v1/collector/performer-batches`，完整字段见 [OpenAPI](../packages/api-contracts/openapi.yaml)。当前只接收人物文字元数据。

- 通过 `COLLECTOR_INGEST_SOURCE_ID` 与 `COLLECTOR_INGEST_TOKEN` 配置一个来源和独立服务凭据；两项都空时接口 404。部分配置、短于 32 字符、包含空白或复用登录/监控/媒体凭据会阻止启动。必须已配置数据库。
- `Authorization: Bearer <token>`，不接受后台 cookie；拒绝带 `Origin` 的浏览器请求。部署到远端时必须经 HTTPS/受控服务网络传输凭据。
- 来源须预先登记为 `active / public_web`，权利状态为 `needs_review` 或 `allowed`。这里 active 表示允许接收已取得的资料，不开启自动采集。资料 URL 必须与登记的来源 host（含端口）一致。
- `schema_version=collector.performers.v1`；一个批次一个来源；1～500 条记录，UTF-8 原始请求体不超过 2 MiB。不接受压缩、未知字段、重复 JSON 键、重复 external_id。
- `payload` 包含 `name`、`aliases`、`profile_url`、`source_content_hash`；记录包含 `external_id` 和带时区的 `checked_at`。URL 只允许 HTTPS，不能含用户名、密码、查询或 fragment。时间不得早于 2000 年或晚于接收时间五分钟。
- `X-Content-SHA256` 是 `sha256:` 加**完整请求体原始字节**的 SHA-256。重试必须保存并重发同一文件，不能重新格式化 JSON。源 `source_content_hash` 原样保留作证据；平台根据 Go 编码的类型化 payload 另算观测 hash，采集端无需重现这个内部 hash。

批次幂等键在数据库中加合同版本和来源前缀，与人工 CSV 命名空间隔离。同来源同键同字节返回 HTTP 200 和原回执；首次暂存返回 201；同键不同字节返回 409。未知或不允许的来源返回 403，字段/hash 错误 400，超限 413，压缩或类型错误 415，数据库错误 503。错误通过既有 error envelope 返回稳定 code 与 request_id，不返回原数据或 SQL。

同来源通过事务 advisory lock 串行接收。成功回执含 batch_id、request_hash、status=staged、input_count、inserted_count、duplicate_count、received_at。整个批次原子提交；拒收不保留部分行。批次表内部 accepted 仅表示接收成功，API 对外明确返回 staged。

同来源、external_id、payload hash 的观测只插入一次。仅 checked_at 改变不创建新观测；元数据改变新增观测，旧候选的人工状态不变。所有新候选强制 pending，记录强制 needs_review / staging / adult_status=unknown；采集请求没有设置公开或成年确认状态的字段。接口不创建正式人物、审核任务、媒体对象、publication 或搜索文档。

## 固定归档准备

在主项目根目录执行，将 UUID 替换为平台实际登记的来源 ID，输出目录必须不存在：

```bash
python3 scripts/prepare_collector_batches.py \
  --source-id 12345678-1234-1234-1234-123456789abc \
  --output .cache/collector-prepared-batches
```

工具先校验原归档 804 个文件和 SQLite，再生成 9 个 JSON 批次及 manifest，共 4,196 条人物。manifest 保存每个文件的 request_hash、字节数和数量。此命令只生成文件，不联网、不导入数据库，不复制或提交 130 张头像。原归档和采集仓库只读。

接口接收符合合同的新批次；此准备工具仍限定已验收的固定归档。采集项目现已提供 SQLite 导出与可恢复 HTTP 投递客户端；完整操作见[双项目验收](COLLECTOR_E2E_ACCEPTANCE.md)。不能直接把旧 performers.jsonl 当作新接口请求体。

## 可重复验收

普通回归：

```bash
go test ./services/platform-api/...
python3 -m unittest scripts/tests/test_prepare_collector_batches.py scripts/tests/test_collector_integration.py
python3 scripts/generate_openapi_types.py --check
npm run check:markdown-links
```

完整数据库验收需要 Go、Docker、已缓存的 `postgres:16-alpine` 和同级固定采集归档。工具创建随机名称、随机 loopback 端口的临时容器，应用当前全部迁移，使用受限 platform_api_login 测试，再自动删除容器，不使用现有数据库配置：

```bash
python3 scripts/run_collector_ingest_contracts.py --report /tmp/collector-ingest-new-report.json
```

报告路径须不存在。包含 8 请求并发幂等、改内容拒收、不同批次观测去重、人工 reviewing 保留、新观测保留旧版本、SQL 中途失败整批回滚，以及真实归档 4,196 条经鉴权 HTTP handler → PostgreSQL 写入、9 批原样重放、正式人物/发布/媒体数量为零。[本轮证据](evidence/collector-ingest-2026-09-22/contracts.json)。HTTP 测试通过 Go httptest 在进程内调用完整路由，不覆盖部署网络、TLS、代理限额或负载压测。

## 后续缺口

候选到正式人物/来源实体映射、版本与字段证据、独立审核任务、头像隔离存储及发布仍未完成。未知成年状态和未审核权利继续阻止公开链路；这些要求需要审核结果，不能由采集程序推断。定时调度、多凭据轮换、生产部署和代理网络验收另行实现。本接口完成了人物元数据接收这一阶段，不能代表全部正式采集发布闭环已上线。
