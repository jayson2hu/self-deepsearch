# Release A 发布就绪审计

更新时间：2026-09-12（Asia/Shanghai）
范围：测试网站 Release A；本页负责放行结论，功能进度见 [项目进度](./PROGRESS.md)。

## 放行结论

Ubuntu 的真实 PostgreSQL / Mailpit 核心联调已通过，旧的“缺少数据库与邮件环境”阻塞已解除。
本次续验又关闭了两站真实浏览器/Worker 缓存和历史归档恢复缺口；目标两地环境和公网发布仍有独立门槛。

| 范围 | 当前判定 | 放行条件 |
| --- | --- | --- |
| Ubuntu 隔离核心验收 | passed | 受限 SQL 仓储、核心账号/运营链和本地恢复已有实测 |
| 本地代码 | go（本地隔离范围） | 最新真实浏览器/归档及代码回归通过；正式制品须按新提交重建，旧五镜像证据不覆盖最新修改 |
| 目标两机完整 Release A / M5 | no-go | 真实媒体、边缘、账单、跨区备份和资源验收未完成 |
| 公网开放 | no-go | 最终域名、安全入口、邮件、真实资料与性能未验收 |

最新详细结果见 [未完成项续验](./evidence/release-a-followup-2026-09-12.md)，前次完整视觉与五镜像记录见 [Ubuntu 验收证据](./evidence/release-a-ubuntu-acceptance-2026-09-12.md)。
逐项产品要求以 [需求与验收矩阵](./RELEASE_A_REQUIREMENTS_MATRIX.md)为索引。

## 当前证据矩阵

| 验收项 | 结果 | 尚未覆盖 |
| --- | --- | --- |
| Schema v21 | PostgreSQL 16.15 的 21 迁移 up/down、权限和增强 seed 负向最终通过 | 目标已有库升级 |
| API 数据库合同 | 59 顶层 + 109 子测试，0 skip / 0 fail | 最终公网入口端到端 |
| Worker 数据库合同 | 7 顶层 + 16 子场景，0 skip / 0 fail | 真实跨区网络及对象供应商 |
| 核心真实服务 | 8 项检查 passed | Turnstile、真实 SMTP |
| 两站真实浏览器 + Worker | 15 项浏览器检查全通过，发布/隐藏可见性 1480/1224ms；0 手动失效 | 真实内容、最终域名、Turnstile、对象存储和目标负载 |
| 历史归档恢复 | 14 顶层 + 34 子测试及 race，通过 9 个真实 PG 场景 | 目标文件系统、复制和真实断电演练；恢复不得复活已清除历史 |
| 本地备份恢复 | dump/校验/本地副本/空库恢复通过，非空目标拒绝 | 跨区 SFTP、目标调度、告警和长期保留演练 |
| Python | 最终 CI 原命令 315 tests + 339 subtests，0 skip；Ruff/mypy 19 文件/OpenAPI 通过 | 远端 Actions 以具体提交结果为准 |
| Go | 最终 test/vet/race 通过 | SQL 由单独的真实数据库合同验收 |
| 前端基本门禁 | 最新 40 单元/76 工具、类型/lint 通过；运营站重建及桌面/移动录入 2/2 通过 | 本次未重复全部 82 项；完整套件继续由 CI 执行，前次 82/82 属于历史基线 |
| Go → Next 缓存 | 9/9 通过 | 真实发布 API 与 Cloudflare 的完整链路 |
| Playwright / 视觉 / 性能 | 最终 82/82，0 失败/跳过/重试；12 图复核及哈希校验、LCP/CLS、HTTP P95 均通过 | 真实资料、真实后端、最终域名和目标负载 |
| Compose / edge | 三套静态解析、真实 nginx -t 通过 | 目标部署的健康检查、路由和运行资源 |
| Docker 五镜像 | 构建、非 root HEALTHCHECK 全通过；两站同源 home/search → Go API → PG 均 200，缓存可写且日志无 EACCES | 目标运行态、真实负载、提交后正式版本制品 |
| 依赖/文档检查 | npm 生产及全量依赖均 0 漏洞；本地 Markdown 链接与 diff 检查通过 | 不代表全部语言或目标镜像的漏洞扫描已完成 |

前端最终结果见 [前端汇总](./evidence/release-a-frontend-ubuntu-2026-09-12.json)，运行镜像见 [Docker 汇总](./evidence/release-a-docker-ubuntu-2026-09-12.json)。
本地核心 passed 也不代表目标服务器已部署，或者任何真实图片上传/删除已经执行。

## 产品与实现边界

- Release A 提供资料站、发现/番号搜索、账号与用户功能、人工运营审核及受控媒体链路。
- 自动采集 Release B、广告联盟 Release C 和头像识别不在本次验收范围。
- 广告保持有标识的静态占位，不接入联盟、悬浮、倒计时、popunder 或资源播放下载。
- 本轮修复 Ubuntu 中文纵排导致的侧栏广告文字重叠，并加入浏览器文本几何断言；E2E 截图改写本轮输出目录，不覆盖历史证据。
- Schema 保持 v21；本轮修复不新增业务迁移。
- 目录图片只读合法主图/精选图槽位；缺图和保护状态使用默认图。
- 主图替换/下架事务成功只证明状态与队列已提交，物理删除必须以实际存储回执证明。
- 用量观察、管理员复核、上传控制、动态默认图和私有 R2 网关代码不等于真实账单保护已验收。

## 公网与 M5 尚未关闭的门槛

1. R2 双桶：私有母版、公开派生图、完整列表/multipart 权限、真实 SDK 超时、中断恢复、删除和容量回收。
2. Cloudflare：受控媒体域、Worker route、R2 binding、旧匿名入口关闭、缓存策略、purge 和保护传播。
3. 计费：核对真实账单、Standard 类别、历史版本、全部 bucket/共享应用及供应商水位完整性。
4. 两机：日本 2C4G / 北京 2C8G 的 CPU/RSS/磁盘/PID、卷 UID/持久性、跨区故障与资源峰值。
5. 备份：日本定时生成、北京真实 SFTP 副本、独立目标空库恢复、保留策略和告警送达。
6. 邮件：最终 SMTP、真实收件送达、权利邮箱，以及额度/熔断和通知恢复。
7. 入口：最终域名、TLS、Turnstile hostname/action、后台固定 IP/VPN/Cloudflare Access。
8. 全站：最终入口的真实 Go API + 两站浏览器流程、公开/私有缓存、退出/重试、真实数据视觉和负载性能；本地真实浏览器闭环已通过。
9. 运维：目标归档与恢复、Prometheus/Alertmanager、版本标签、健康探针及 [上线执行记录](./RELEASE_A_GO_LIVE_RECORD.md)；本地归档恢复合同已通过。

Analytics、双桶字节检查、页面 CSP 或管理员确认都不是账户账单硬上限。
权利下架与必要删除不应被非必要上传/扫描保护阻断。
本地单进程或无网络限速的性能结果不能放行目标 CPU/内存限制。

## 证据解释与安全边界

- [核心 JSON](./evidence/release-a-core-e2e-ubuntu-2026-09-12.json)来自真实 Go API、PostgreSQL 和 Mailpit；其 not_covered 列表继续有效。
- [后端汇总](./evidence/release-a-backend-ubuntu-2026-09-12.json)区分通用 race 回归与专项真实 SQL：通用运行的 47 个 opt-in 跳过项由数据库及缓存专项另测，不宣称 SQL 已在 race 模式执行。
- [缓存 JSON](./evidence/release-a-cache-contract-ubuntu-2026-09-12.json)包含真实 Go/Next 进程，但目录 API 是合成替身。
- 新增[真实浏览器 JSON](./evidence/release-a-real-stack-ubuntu-2026-09-12.json)不使用目录替身，实际串联 PostgreSQL、Mailpit、Go API/Worker 和两个生产前端；首页/详情/sitemap 验证缓存失效，`no-store` 搜索只验证可见性。它不把合成账号和资料冒充真实运营数据。
- 新增[归档恢复 JSON](./evidence/release-a-history-archive-ubuntu-2026-09-12.json)使用真实 PG 和临时表逐字段恢复核对；目录同步故障注入不等于真实掉电测试。
- [前端汇总](./evidence/release-a-frontend-ubuntu-2026-09-12.json)记录完整一轮 82/82 浏览器通过，桌面/移动各 41 项；js-yaml 更新至 4.3.2 后生产与全量依赖审计均为 0 漏洞。
- [浏览器性能 JSON](./evidence/release-a-performance-2026-09-12.json)和 [HTTP 延迟 JSON](./evidence/release-a-http-latency-2026-09-12.json)属于 local-synthetic，最终布局版本已通过：首页/详情桌面 LCP 中位数 332/196ms、移动 184/148ms，移动 CLS 均为 0；首页 API/搜索 P95 为 29.79/9.10ms。
- [视觉 manifest](./evidence/release-a-visual-2026-09-12/manifest.json)记录最终 12 张合成截图、视口与已核对的 SHA-256，布局和坏图检查通过，不能作为真实内容验收。
- 早期 preview 图片是原型设计基线；历史 evidence 保留原日期，新截图不得覆盖旧日期结果。
- 运营及账号页采用请求级 CSP nonce；公开目录仍保留框架脚本所需 unsafe-inline，风险边界未因本轮消失。
- 数据库 URL、密码、私有环境、会话、原始服务日志、备份文件与缓存均不提交。
- 语法检查 DNS override 只供 nginx -t 使用，不能加入实际 edge 部署。

## 后续执行顺序

1. 使用本轮提交后的新 SHA 构建正式制品；本地验收镜像明确来自 modified_worktree，不将其基线 SHA 标签当作正式发布版本。
2. 在目标两机执行生产配置检查、Schema/运行角色校验、核心业务和真实邮件验收。
3. 接真实 R2/Cloudflare，验证完整图片生命周期、保护切换及必要删除。
4. 完成跨区备份、资源/告警、最终域名安全与真实数据性能验收。
5. 记录每项结果后重新作出完整 Release A / M5 与公网 go/no-go 决定。

执行准备见 [联调清单](./RELEASE_A_PREP_CHECKLIST.md)、[运行手册](./RELEASE_A_RUNBOOK.md)及 [媒体任务策略](./MEDIA_STORAGE_TASK_POLICY.md)。

## 历史审计入口

- [9 月 11 日本地回归](./evidence/release-a-local-regression-2026-09-11.md)、[运行时目录](./evidence/release-a-runtime-catalog-2026-09-11.md)、[CSV 安全导出](./evidence/release-a-csv-error-export-2026-09-11.md)。
- [账号页 CSP](./evidence/release-a-ops-csp-nonce-2026-09-11.md)、[Turnstile 绑定](./evidence/release-a-turnstile-binding-2026-09-11.md)、[Worker 租约](./evidence/release-a-worker-lease-2026-09-10.md)。
- [主图读取](./evidence/release-a-media-primary-read-guards-2026-09-12.md)、[媒体退出](./evidence/release-a-media-retirement-2026-09-10.md)、[私有边缘网关](./evidence/release-a-media-edge-gateway-2026-09-11.md)。

重复旧状态已从本页移除；历史记录中的“SQL skip / 未启动 Docker”描述只适用于原执行日期。
