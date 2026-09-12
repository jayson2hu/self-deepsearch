# Release A 联调准备清单

版本：v1.0  
适用范围：测试网站首次真实联调，不包含自动采集（Release B）和广告联盟（Release C）。

准备项只覆盖真实环境输入，不代表代码缺口。逐项需求状态见[Release A 需求与验收矩阵](./RELEASE_A_REQUIREMENTS_MATRIX.md)。

## 先准备核心项

2026-09-11 更新：[手动跨区上传控制](./MEDIA_UPLOAD_CONTROL.md)及[后台授权队列/回执](./MEDIA_UPLOAD_QUEUE.md)已开发，默认关闭，Schema v21。后续启用需全部北京上传器支持限时命令、两地独立匹配控制密钥、可信网络/时钟、北京共享卷和新 SQL 合同验收。日本 dispatch 要求北京 enforce；无 observer 仅能暂停、不能恢复。密钥写服务器私有环境，不在聊天发送。上传控制 UI 和逐操作上传自动暂停已完成离线实现；启用前须另配[北京到日本私有准入通道](./MEDIA_UPLOAD_ADMISSION.md)、两端模式/独立密钥并升级 v2 日志 reader。日本 API/Next 的[动态默认图](./MEDIA_DELIVERY_MODE.md)与[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)也已开发；后者要求关闭匿名 bucket 入口、部署 Worker route/binding 并验证 purge。不是补凭据即可上线。

准备 PostgreSQL、邮件服务和首个 owner 后即可启动第一轮账号联调；随后由 owner 邀请第二个运营账号，再执行异人审核发布。真实内容可在业务链路跑通后再补：

| 优先级 | 项目 | 最小准备内容 | 验收入口 |
| --- | --- | --- | --- |
| P0 | 日本 PostgreSQL 16 | 测试库地址、端口、数据库名、迁移账号；允许日本 API/Worker 访问 | `healthz`、fresh migration、权限测试 |
| P0 | 邮件服务 | Mailpit（测试）或 SMTP（正式）；发件邮箱 | 注册、重置密码、关闭账号、邀请 E2E |
| P0 | 首个 owner | 邮箱和一次性强密码 | 后台登录、创建普通用户/管理员邀请 |
| P0 | 第二个运营账号 | 由 owner 邀请一个 editor 或 admin；不得与审核账号是同一用户 | 自动执行“录入者提交 → admin/owner 审核发布”且验证禁止自审 |
| P1 | 首批资料 | 可先使用仓库合成 fixture；真实验收再准备 5～10 部作品、2～3 个人物、1 个厂牌 | 真实资料的人工录入、审核、发布、番号搜索 |

仓库已有合成 fixture（30 个人物、100 部作品），因此首轮不必准备真实资料。日本测试库可在 migration/runtime logins 后、创建真实 owner 前，通过双确认的 `database-fixtures` 一次性工具显式加载；工具拒绝非空真实数据和非 fixture 账号，不会由生产迁移自动执行。没有图片的记录会使用默认图；要验证图片链路时再提供一张已确认可展示的本地测试图片即可。

如果先做隔离的自动核心验收，可使用 CI `core-e2e` 或运行手册 7.1 的固定名称专用空库流程：运行器会随机生成 owner，并通过 Mailpit 邀请 editor，不必提前提供这两个账号。此模式不加载上述演示 fixture、不复用共享测试库；通过后仍需在日本/北京真实部署环境复验。

## 第二阶段准备

第一轮业务联调通过后，再配置以下项目：

| 项目 | 需要准备 | 说明 |
| --- | --- | --- |
| S3/R2 | endpoint、私有母版 bucket、公开派生 bucket、最小权限密钥、图片公开域名 | 两桶分开；母版不公开。容量检查需完整两桶 ListObjects、ListMultipartUploads、ListParts，PUT/DELETE 仍限管理前缀；核对 GB/GiB、月计费和账户共享额度 |
| 北京媒体服务 | 北京副本和私有 media-state 卷、UID 10001 可写、固定 `MEDIA_UPLOAD_LOCK_FILE`、日本到北京的防火墙放行、`MEDIA_HMAC_SECRET` | 无 PostgreSQL 凭据；所有上传同机同卷同锁，旧卷权限不由新镜像自动修正 |
| 域名与边缘 | `display`、`ops`、`api`、`media` 域名，Cloudflare Zone、Origin Certificate | 账号/搜索 HTML、API 和后台必须绕过缓存；公开目录 HTML 可按 5 分钟策略缓存 |
| 后台访问控制 | 固定 IP、VPN 或 Cloudflare Access | 测试期可先使用普通后台登录，正式开放前必须加网络层限制 |
| 备份 | 北京备份目录和恢复用临时 PostgreSQL | 每 7 天一次，正常保留最近 4 份；未验证文件和最后恢复点受保护，超额需补验；正式生产再提高频率 |
| 告警通知 | 值守邮箱、Alertmanager 私有配置、独立 SMTP 密码文件 | critical 与 warning/info 分流；上线前验证 firing 和 resolved 邮件均可送达 |

## 资料字段

容量并发/中断恢复、人工主动默认图和可选动态执行端已开发，见[容量手册](./MEDIA_UPLOAD_CAPACITY.md)与[停图切换/恢复](./MEDIA_DELIVERY_MODE.md)。准备两地一致的 `MEDIA_DELIVERY_MODE`；若启用动态模式，仅在日本 API/公开站设置 `MEDIA_DELIVERY_DYNAMIC_MODE=enforce`。若启用图片网关，日本 API另设 `MEDIA_EDGE_POLICY_MODE=enforce` 和独立 token，Cloudflare Worker 配置相同 secret、media origin、policy URL和私有派生桶 binding；核对匿名 R2 URL关闭、Next/Cloudflare HTML 失效、5 秒策略传播和删除 purge。Go [一次性观察命令](./MEDIA_USAGE_OBSERVATION.md)现含操作与存储 Analytics，需要确认供应商、全账户 Standard 类别、明确账期、账户级只读 Analytics 权限与真实响应；无法确认全账户 Standard 时不得传 `--standard-only-confirmed`。Analytics token 仅在日本 Worker 注入，edge token 仅在 API 与 Worker secret；均不发送到聊天或前端。[授权复核/账期切换](./MEDIA_USAGE_REVIEWS.md)已开发，真实 SQL 尚待验收；可信账单/版本历史/共享应用口径和网关真实验收仍未完成，提供凭据不能替代这些工作。

作品最小字段：

- 作品编号（番号）
- 作品标题
- 实际发行日期（未知可留空，不进入“最新发行”）
- 人物公开艺名
- 人物别名（可选）
- 厂牌/制作方

来源和权利字段：

- 来源类型（普通网页、官方页面、人工核验等）
- 来源 URL（可选）
- 核验时间（精确到秒，带时区）
- 操作人
- 权利状态
- 下架联系人或工单标识

图片字段不是人工手填 S3 元数据。管理员提供本地图片和实体 ID，北京 `media-python` 生成 manifest，后台只导入并核对一一映射。

## 安全提交方式

不要通过聊天、工单或 Git 提交密码、SMTP 密钥、S3/R2 密钥、Cloudflare Token、HMAC secret。将它们写入服务器私有 `.env`，并限制文件权限。可以只反馈以下非敏感信息：

```text
环境：测试 / 生产
PostgreSQL：日本服务器，地址和端口（不含密码）
邮件：Mailpit / SMTP（不含密码）
owner：邮箱已准备 / 待准备
图片：无图片（使用默认图）/ 已准备本地测试图
公网：暂不开放 / 已有域名
```

## 部署前环境检查

将日本、北京生产变量分别写入服务器私有 env 文件后执行：

```powershell
npm run check:release-a-env -- --japan-env C:\secure\japan.env --mode core
npm run check:release-a-env -- --japan-env C:\secure\japan.env --beijing-env C:\secure\beijing.env --mode full
python3 scripts/release_a_env_check.py --japan-env /srv/self-deepsearch/.env --mode core --check-files
```

检查范围包括：未替换占位符、生产安全开关、镜像固定版本、数据库登录分离、密钥长度与互不复用、站点 URL/允许来源关系、共享媒体密钥、双 bucket、公开图片基地址、10 GiB 上限，以及证书/私钥、metrics token、Alertmanager 配置和 SMTP 密码文件。输出只包含变量名和错误原因，不包含密钥值。full 应在受控管理机上读取两份私有配置，不要跨服务器复制整份 env；`--check-files` 只需在日本服务器上使用。仓库 `.env.example` 本身应检查失败，因为它只用于列出变量并保留明显占位符。

## 联调顺序

1. 对服务器私有 env 文件执行 core 环境检查；
2. 在日本执行 v21 fresh migration 和运行账号配置；需要 30 人/100 作品合成目录时，在空测试库显式执行双确认 `database-fixtures`；
3. 使用 API 镜像内的一次性 `owner-bootstrap` 工具创建首个 owner，再启动 Go API、Go Worker、公开站和运营后台；服务器不要求安装 Go；
4. 运行 `scripts/release_a_deployment_probe.py`，确认两站入口、API/Worker 版本、数据库 ready、安全头、缓存和广告边界；
5. 用 Mailpit 执行注册、重置密码、关闭账号和邀请 E2E；
6. 准备两个不同运营账号，用 `scripts/release_a_catalog_e2e.py` 完成“编辑提交 → 管理员审核 → 发布 → 番号搜索/详情/sitemap → 隐藏”；
7. 接入 S3/R2 和图片 manifest，执行 full 环境检查并验证默认图、派生图、下架和对账；
8. 配置 Alertmanager 值守邮箱，验证 warning/critical 触发及 resolved 邮件送达；
9. 最后接入域名、Cloudflare、北京副本和备份恢复验收。

每一步未通过时，不进入下一步；未配置真实来源前不启用自动采集，未完成政策审核前不启用广告联盟。

定时对账可另行启用[用量准入](./MEDIA_TASK_ADMISSION.md)：默认 `MEDIA_RECONCILE_USAGE_GUARD=off`，开启要求已验证 observe 模式与对账、全部日本实例配置一致及新告警送达。它只控制新全量扫描；上传、页面和边缘图片入口有各自默认 off 的执行端，必须逐项真实启用和验收，提供凭据不会自动完成这些项。
