# 自动采集与批次导入计划

版本：v1.0  
状态：Release B 设计基线，暂不执行真实采集

本文定义北京、日本两套 Python 采集脚本如何由 Go Worker 调度，如何把候选资料安全写入 PostgreSQL 的 `collector` Schema，以及如何经过人工审核后进入公开资料。自动采集不能绕过审核，也不能因为某个来源失败而影响公开站。

## 1. 目标与边界

### 目标

- 支持普通网页来源和已配置的来源连接器；
- 支持按人物、厂牌、来源或增量时间窗口创建采集任务；
- 支持北京、日本两个区域互不抢任务，并将结果写入同一个日本 PostgreSQL；
- 支持批次幂等、候选去重、来源追踪、冲突审核和图片候选审核；
- 支持用户后续提供采集脚本，但脚本必须经过代码审查、测试和镜像发布；
- 采集失败、来源结构变化或跨区网络中断时可以重试、暂停和恢复。

### 不做

- 不自动发布采集结果；
- 不通过验证码、登录 Cookie、代理轮换、指纹伪装或其他方式绕过封锁；
- 不把百度或 Google 搜索结果页当作默认自动抓取源。搜索引擎只作为人工候选入口，或在取得合规 API 后由专用连接器访问；
- 不采集私人页面、付费页面、需要登录的页面或无法确认权利边界的内容；
- 不允许后台通过任务参数上传并直接执行任意 Python 文件；
- 不采集头像做人脸识别或人物自动合并；
- 不抓取或保存视频、音频、磁力、种子、网盘和下载资源。

## 2. 组件职责

```text
管理员/定时器
  -> Go API 创建或暂停 collector job
  -> 日本/北京 Go Worker 按 region 租约领取任务
  -> Worker 启动固定版本的 Python CLI 子进程
  -> Python 输出候选 JSONL、图片候选和结构化错误
  -> Worker 校验、压缩、签名并提交日本 Go API
  -> collector.source_records 幂等落库
  -> 归一化、去重、冲突候选和人工审核
  -> Go 发布服务创建 revision/publication
  -> 搜索文档、缓存和 sitemap 更新
```

| 组件 | 职责 | 不允许做的事 |
| --- | --- | --- |
| `collector-python` | 访问允许的公开来源、解析 HTML/JSON、输出候选 | 直接连接 PostgreSQL、直接发布、修改正式表 |
| `platform-worker` | 租约、超时、子进程、批次、重试、spool、指标 | 执行未审核脚本、绕过来源限制 |
| `platform-api` | 任务 API、批次接收、幂等、候选和审核 API | 把外部网页内容直接返回给公开用户 |
| `ops-web` | 创建任务、查看日志/冲突、人工确认、取消和重试 | 从浏览器上传并执行任意代码 |
| PostgreSQL | `collector` 事实候选、`platform` 已审核发布、`audit` 证据 | 由 Python 采集器直接写入 |

Python 采集器不持有数据库、S3 或管理员凭证。Go Worker 通过受控子进程接口调用固定镜像内的 CLI，不向容器暴露 Docker socket。

## 3. 任务类型

### 3.1 任务枚举

| `job_type` | 用途 | 默认触发方式 | 是否允许自动运行 |
| --- | --- | --- | --- |
| `source_probe` | 检查来源可达性、编码、结构版本和 robots/条款提示 | 管理员或每周 | 是，结果不发布 |
| `work_incremental` | 按来源更新时间或外部 ID 获取作品候选 | 管理员、每日低峰 | 是，进入审核 |
| `performer_incremental` | 获取人物公开艺名、别名和可选公开参数候选 | 管理员、每周 | 是，进入审核 |
| `work_detail` | 对指定候选补齐标题、厂牌、发行日期和关系 | 人工点选或批次 | 是，进入审核 |
| `image_candidates` | 只获取允许展示的宣传图候选和元信息 | 人工点选或批次 | 是，进入图片审核 |
| `reconcile_source` | 对已收录来源做 hash、字段和下架状态对账 | 每周或手动 | 是，不自动覆盖人工值 |
| `retry_dead_job` | 管理员重新执行 dead 任务 | 管理员 | 是，需记录原因 |

### 3.2 任务状态

```text
pending -> running -> completed
                   -> retry -> running
                   -> dead
                   -> cancelled
```

- `pending`：等待区域 Worker；
- `running`：持有有效租约；
- `retry`：可重试的网络、限流或临时依赖错误；
- `completed`：脚本完成且批次已持久化，代表“候选已接收”，不代表内容已发布；
- `dead`：达到重试上限，等待管理员处理；
- `cancelled`：管理员取消，不能再次自动领取，重跑必须创建新任务或明确恢复原因。

### 3.3 任务参数

任务 payload 只保存结构化参数，不保存 Python 代码：

```json
{
  "source_id": "source_a",
  "connector_version": "source_a@2026.07.31",
  "scope": {
    "performer_ids": ["uuid"],
    "external_ids": ["remote_123"],
    "since": "2026-07-01T00:00:00Z"
  },
  "limits": {
    "max_pages": 20,
    "max_records": 100,
    "max_images": 200
  }
}
```

任务创建接口必须校验 `source_id`、区域、连接器版本、页数、记录数、图片数和时间窗口。禁止接受 shell 命令、Python 代码、浏览器 Cookie、代理地址或 S3 凭证。

## 4. Python 脚本接入合同

### 4.1 目录和版本

用户提供的脚本进入代码仓库并经过审查，不直接上传后台执行：

```text
workers/collector-python/
  common/
  connectors/
    source_a/
      connector.py
      parser.py
      fixtures/
      contract.yaml
    source_b/
      connector.py
      parser.py
      fixtures/
      contract.yaml
  cli.py
```

每个连接器必须有不可变版本号、测试 fixture、超时配置、允许的域名列表、字段映射和变更说明。镜像使用 Git commit 标签发布；任务参数只能引用已发布的 `connector_version`。

### 4.2 CLI 入口

```text
python -m collector.cli probe \
  --source-id source_a \
  --connector-version source_a@2026.07.31 \
  --output-dir /work/output

python -m collector.cli collect \
  --source-id source_a \
  --connector-version source_a@2026.07.31 \
  --scope-file /work/task.json \
  --output-dir /work/output

python -m collector.cli validate \
  --input /work/output/candidates.jsonl
```

CLI 约定：

- 正常候选写入 `candidates.jsonl`，图片候选写入 `media_candidates.jsonl`；
- 结构化日志写标准错误，不把密码、Cookie、验证码、完整邮箱或签名 URL 写入日志；
- 退出码 `0` 表示输出已完成，`2` 表示输入或合同错误，`10` 表示可重试依赖失败，`20` 表示来源结构变化需人工处理；
- 单页、单响应、单图片、总字节和总运行时间都有上限；
- 输出目录由 Worker 创建，任务完成后压缩并写入 spool，Web 服务器不可访问该目录。

### 4.3 候选记录格式

```json
{
  "external_id": "remote_123",
  "entity_type": "work",
  "operation": "upsert",
  "source_updated_at": "2026-07-31T10:00:00Z",
  "idempotency_key": "source_a:work:remote_123:v8",
  "content_hash": "sha256:...",
  "payload": {
    "canonical_code": "MK-1042",
    "title": "公开标题候选",
    "release_date": "2026-07-18",
    "studio_name": "厂牌候选",
    "performer_aliases": ["别名候选"],
    "summary": "公开资料摘要候选"
  },
  "provenance": {
    "source_type": "public_web",
    "source_url": null,
    "source_title": null,
    "checked_at": "2026-07-31T10:02:00Z",
    "confidence": 0.75,
    "rights_status": "needs_review"
  }
}
```

字段来源不明确时保留 `null` 并降低置信度，不允许脚本猜测。`source_url` 可空，但 `source_type`、`checked_at`、`confidence` 和 `rights_status` 必须存在。

## 5. 执行和写入流程

1. 管理员或定时器通过 Go API 创建任务；
2. API 写入 `collector.jobs`，分配 `region=beijing` 或 `region=japan`；
3. 对应 Worker 以短租约领取任务，发送心跳；
4. Worker 校验连接器版本和 payload，创建隔离临时目录；
5. Worker 启动 Python CLI，限制 CPU、内存、运行时间、文件大小和网络出口；
6. CLI 输出 JSONL 和图片候选，Worker 先本地校验再生成 batch；
7. 北京 Worker 先写本地 spool，再通过 HTTPS/HMAC 提交日本 Go API；
8. Go API 校验 schema、batch、签名、区域和幂等键，写入 `collector.source_records`；
9. 归一化、判重、冲突检测生成候选和人工审核任务；
10. 管理员确认字段、人物关系、图片和权利状态；
11. Go 发布服务在事务中生成 revision/publication，之后刷新搜索、缓存和 sitemap；
12. 只有已发布资料可以被公开网站读取。

Python 只负责“发现和整理候选”，Go 服务负责“接收、约束、审核和发布”。

## 6. 去重与冲突规则

### 6.1 记录级幂等

- 同一 `source_id + external_id + connector_version` 不能重复创建候选；
- 相同 `idempotency_key` 和相同 body 重复提交返回原响应；
- 相同 key 但 body 不同返回 `409 IDEMPOTENCY_MISMATCH`；
- 相同 `content_hash` 可以复用候选，但必须保留每个来源的 provenance；
- 批次重复提交 3 次只能产生一份有效接收记录。

### 6.2 作品判重

候选顺序：来源映射完全一致、规范番号 + 厂牌、规范番号 + 发行日期 + 人物集合、标题/日期/厂牌/人物相似度。番号不是全局唯一键，疑似重发版或特殊版本通过 `edition_of` 关联。

### 6.3 人物和字段冲突

- 图片不能作为人物自动合并依据；
- 艺名相同只生成候选，不自动合并；
- 已人工确认的发行日期、人物关系、身体参数、社交账号和图片不能被后采集值静默覆盖；
- 多来源冲突进入 `conflict_reviews`，管理员必须选择来源、保留旧值或标记未知；
- `adult_status` 未确认的记录禁止进入公开发布流程。

## 7. 两个区域的运行策略

| 区域 | 默认任务 | 并发上限 | 失败时行为 |
| --- | --- | ---: | --- |
| 日本 | 日本可访问来源、低频增量、发布后对账 | Python CLI 1 个 | 只暂停对应来源，公开站继续 |
| 北京 | 普通网页来源、批量候选、图片处理前置 | 采集 CLI 1-2 个 | 结果保留本地 spool，跨区恢复后批量提交 |

- 两区只领取自己的 `region` 任务，不共享租约；
- 日本可以处理北京无法访问的来源，但不能用来规避封锁、地域限制或服务条款；
- 北京到日本的跨区请求只使用批量 HTTPS/HMAC，不执行逐条同步 SQL；
- 跨区 172ms 延迟或中断时，spool 保留批次，指数退避；
- 同一来源同一时间只运行一个任务，避免重复抓取和触发来源限流。

## 8. 调度、重试和资源限制

### 8.1 推荐调度

- Release B 初期：全部手动创建任务，验证每个来源连接器；
- 稳定后：每日低峰执行 `work_incremental`；
- 每周执行 `source_probe` 和 `reconcile_source`；
- 图片候选默认按人工选择的作品执行，不做全站无界限图片抓取；
- 来源结构变化、连续限流或权利状态异常时自动暂停连接器。

### 8.2 重试策略

- DNS、连接超时、5xx、跨区暂不可达：指数退避，最多 5 次；
- 429 或明确限流：读取 `Retry-After`，到时间后最多重试 3 次；
- 解析结构变化：不重试抓取，进入 `schema_changed` 结果并暂停来源；
- 4xx 权限、登录、验证码或条款拒绝：立即停止该来源，不做绕过；
- 超过上限进入 `dead`，管理员查看错误摘要后选择取消、修复连接器或新建任务。

### 8.3 资源护栏

- 单任务默认最多 20 页、100 条候选、200 个图片候选；
- 单 HTTP 响应、单图片、单批次和总下载字节设置硬上限；
- 日本 2C4G 同时只运行一个采集 CLI，避免与备份和批量发布并发；
- 北京 2C8G 采集与图片处理分别限并发，禁止占满 Go API 和 PostgreSQL 连接池；
- 磁盘 70/80/90% 分级告警，spool 达到硬上限后暂停北京采集。

## 9. 安全和合规

- 来源 URL 来自管理员配置或已批准来源清单，不直接执行用户反馈中的 URL；
- 每次重定向重新解析 DNS，拒绝环回、内网、链路本地、云元数据 IP 和非 HTTP(S)；
- 不携带登录 Cookie、Authorization、验证码或个人账户信息；
- 下载内容限制 MIME、尺寸、压缩炸弹、EXIF 和脚本注入，图片先进入私有 staging；
- 用户提供的脚本必须进入代码评审、fixture 测试、依赖扫描和镜像发布流程；
- 任务 API 使用区域白名单、HTTPS、HMAC、时间戳、nonce 和 request ID；
- 日志只记录 `job_id`、`batch_id`、`source_id`、版本、错误码和耗时，不记录原始 HTML、完整邮箱或签名 URL；
- 来源、图片权利、成年确认和下架请求必须可审计；
- 自动采集不能根据图片推断人物身份或年龄。

## 10. 监控和告警

每个任务至少记录：

```text
job_id, batch_id, source_id, region, connector_version,
records_seen, records_accepted, records_rejected,
images_seen, conflicts_created, duration_ms,
attempt_count, status, error_code
```

核心指标：

- 任务成功率、重试率、dead 数量和最老任务年龄；
- 来源连接耗时、429/403/5xx、解析结构变化次数；
- 候选接收量、重复率、冲突率、人工审核积压；
- 每个区域 CPU、内存、磁盘、spool 大小、网络错误和 PostgreSQL 连接；
- 批次提交延迟、幂等冲突、跨区失败和恢复时间。

告警必须对应动作：来源异常只暂停来源；spool 或磁盘高水位暂停北京采集；公开 API、数据库、权利下架和备份故障优先级最高。

## 11. Release B 分阶段计划

### B0：合同和模拟器（2-3 天）

- 固定 JSON Schema、退出码、错误码、任务状态和 CLI 参数；
- 用本地 fixture 生成候选 JSONL，不访问外网；
- 完成重复提交、非法 payload、超限和失败重试测试。

### B1：单来源北京采集（3-5 天）

- 接入一个普通公开网页来源；
- 完成 source probe、增量任务、spool、批量提交和人工审核；
- 验证北京断网、任务重领和重复批次。

### B2：单来源日本采集（3-5 天）

- 接入一个需要日本网络可达的普通来源；
- 验证日本 Worker 与北京 Worker 任务隔离；
- 验证日本采集不会阻塞公开 API、备份和发布。

### B3：去重、冲突和图片候选（3-5 天）

- 作品/人物候选判重；
- 发行日期、人物关系、图片和权利冲突审核；
- 图片 staging、去 EXIF、派生和默认图回退。

### B4：稳定运行（至少 30 天）

- 连续运行并观察成功率、积压、资源和来源政策；
- 无未解决高严重度故障后，才评估广告联盟 Release C；
- 任何自动发布需求必须另立评审，不作为当前 Release B 默认行为。

## 12. Release B 验收标准

- 同一批次提交 3 次只生成一份有效记录；
- 北京和日本同时运行不会领取同一任务；
- Worker 崩溃后租约过期可重领，spool 批次不丢失；
- 来源结构变化只暂停对应连接器，不影响其他来源和公开站；
- 已人工确认字段不会被采集值静默覆盖；
- 采集任务不持有 PostgreSQL、S3 或管理员凭证；
- 图片、身体参数、社交账号和人物关系都经过人工审核才可发布；
- 下架后公开页面、搜索、sitemap、缓存和公开图片按流程移除；
- 自动采集资源占用不使公开 API 超过既定 SLO；
- 可从北京最近一次验证备份恢复任务和发布所需数据。

## 13. 来源接入登记（后续，不纳入当前设计）

本节只作为未来增加来源时的登记清单，当前 Release B 不设计具体来源、连接器规则、采集频率或真实定时任务。未确认来源前，只开发通用任务合同、fixture 和模拟器，不启用真实来源采集。

后续每增加一个来源，再补充以下信息：

- 来源域名和来源类型；
- 是否有官方 API、RSS 或站点地图；
- 采集脚本或字段解析规则；
- 增量字段，例如更新时间或外部 ID；
- 访问频率、每日页数、记录数和图片候选数量限制；
- robots、来源条款、图片展示权利和下架联系人；
- 允许使用北京、日本哪个区域访问。
