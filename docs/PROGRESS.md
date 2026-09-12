# 项目进度

更新时间：2026-09-12（Asia/Shanghai）
当前目标：Release A 测试网站；本页只记录当前快照，历史执行细节保留在 evidence。

## 当前结论

Ubuntu 隔离核心验收已通过。此前缺少 PostgreSQL / Mailpit、无法执行真实 SQL 的阻塞已解除。
已在真实 PostgreSQL 16.15 和 Mailpit 上运行 API、Worker 仓储合同及核心业务闭环；本次续验又完成两站真实浏览器与 Worker 缓存闭环、历史归档恢复，并修复实际发现的问题。

| 放行范围 | 当前结论 |
| --- | --- |
| Ubuntu 核心数据库与邮件链路 | passed，具体覆盖见下表 |
| 本地代码 | go（本地隔离范围），真实浏览器及归档续验通过；制品须按新提交重建 |
| 目标两地完整 Release A / M5 | no-go，真实对象存储、边缘及目标环境未验收 |
| 公网开放 | no-go，最终域名、安全入口、邮件及性能门槛未完成 |

最新结果见 [未完成项续验记录](./evidence/release-a-followup-2026-09-12.md)；前次五镜像及完整视觉结果见 [Ubuntu 验收记录](./evidence/release-a-ubuntu-acceptance-2026-09-12.md)，不将旧镜像冒充本次修改后的制品。
放行定义见 [发布就绪审计](./RELEASE_A_READINESS.md)，逐项需求见 [验收矩阵](./RELEASE_A_REQUIREMENTS_MATRIX.md)。

## 已实现能力

- 公开资料展示、作品发现与人物混排、番号搜索、详情、站点地图和默认图。
- 作品 CSV 预检/提交与安全错误导出；人物、厂牌人工维护；修订、异人审核、发布、隐藏和合并。
- 注册、登录、密码重置、账号关闭、邀请；owner/admin/editor/user 分权、近期密码与会话撤销。
- 收藏、关注、不感兴趣、浏览历史和反馈；私有账号状态不进入公开目录缓存。
- 图片处理、母版/派生图 manifest、主图与精选图、主图替换、母版保留、下架删除调度。
- 双桶对象及 multipart 容量检查、上传互斥、逐操作准入、授权暂停/恢复及不可变回执。
- 每日公开状态巡检、对象对账、用量观察与复核、API/页面动态默认图、私有 R2 网关代码。
- PostgreSQL Schema v21、21 个迁移；两地 Compose、五镜像构建、备份、监控和告警配置。

Release A 只使用合成或授权人工资料和静态广告占位。
自动采集属于 Release B，广告联盟属于 Release C；头像识别不在当前范围。
对象存储观察、自动任务和保护执行器按各自配置默认关闭，不能因为代码存在就视为已启用或已验证。

## 本轮已确认验收

| 项目 | 实际结果 | 证据边界 |
| --- | --- | --- |
| PostgreSQL 16.15 迁移 | 21 个迁移 up/down、运行权限及增强 seed 负向最终复跑通过 | 独立 PostgreSQL 16 容器 |
| API 真实仓储 | 59 个顶层测试、109 个子测试通过，0 skip / 0 fail | 受限 API 登录；含账号、权限、媒体和运营 SQL |
| Worker 真实仓储 | 7 个顶层测试、16 个子场景通过，0 skip / 0 fail | 租约、调度、巡检、用量和上传队列 |
| Go API + PostgreSQL + Mailpit | 8 项核心检查通过 | 注册/重置/关闭/邀请/异人审核/发布/搜索/隐藏 |
| 两站真实浏览器 + Worker | 15 项浏览器检查及 API/Worker readiness 全通过；发布 1480ms、隐藏 1224ms | 真实受限 SQL、Mailpit、Go API/Worker、生产 Next；无目录替身、无手动失效；搜索为 no-store |
| 历史归档与恢复 | 14 顶层 + 34 子测试通过，含 9 个真实 PG 场景与 race | 临时表逐字段核对、保持软删除；文件不可覆盖，目录同步失败不得提交成功 |
| 本地备份恢复 | dump、SHA-256、副本校验、空库恢复通过；拒绝非空目标 | 本地复制，未验证日本到北京 SFTP |
| Python | 最终 CI 原命令 315 tests + 339 subtests 通过，0 skip | 含重定向认证头保护、隔离导入、真实进程树清理及缓存证据边界 |
| Python 静态/API 合同 | Ruff、mypy 19 文件、OpenAPI 漂移通过 | 包含新增真实浏览器父运行器 |
| Go test/vet/race | 最终复跑通过 | 含 Go→Python HTTP；SQL 另由上方专用库验收 |
| 前端基础验收 | 最新 40 单元、76 工具、类型和 lint 通过；运营站重新生产构建、作品录入桌面/移动 2/2 通过 | 新修复表单成功后的错误提示；此前两站构建/HTTP 冒烟证据保留 |
| Go → Next 缓存 | 9/9 通过 | 真实 Go/Next 进程、合成目录 API |
| 浏览器、视觉与性能 | 82/82，0 失败/跳过/重试；12 图复核及 SHA-256 校验通过；LCP/CLS、HTTP P95 达标 | 最终布局版本，桌面/移动各 41 项；合成 API、无网络限速 |
| npm 依赖审计 | 生产及全量依赖均 0 漏洞 | js-yaml 锁定版本已由 4.3.1 更新至 4.3.2 |
| Compose / Nginx | 根级、日本、北京静态解析和 nginx -t 通过 | 不代表目标服务器运行态已通过 |
| 五个 Docker 镜像 | 全部构建并以非 root 用户通过实际 HEALTHCHECK；两站缓存写入及同源首页/搜索 200 | 真实 Go API / PG 空测试库；无目标两地部署 |

后端 JSON：[数据库、Go/Python 与备份汇总](./evidence/release-a-backend-ubuntu-2026-09-12.json)、[真实核心闭环](./evidence/release-a-core-e2e-ubuntu-2026-09-12.json)。
前端完整结果见 [前端验收汇总](./evidence/release-a-frontend-ubuntu-2026-09-12.json)：首页/详情桌面 LCP 中位数 332/196ms，移动 184/148ms，移动 CLS 均为 0；首页 API/搜索 P95 为 29.79/9.10ms。
浏览器、视觉和性能 JSON 的合成数据边界应保留，不能替代真实数据库、公网或两地网络验收。

## 收尾与交付

- 最新 CI 修复 console `pytest` 的脚本导入失败；加入真实浏览器和归档门禁。远端结果以对应提交的 Actions 运行记录为准，本地通过不自动等同远端通过。
- 本次续验的两个专用容器、合成数据库/邮件、临时归档、隔离前端副本、临时二进制和原始日志已清理；开发依赖和可复用缓存保留，测试数据可重新生成。
- 五个运行镜像构建与实测已完成，见 [Docker 验收汇总](./evidence/release-a-docker-ubuntu-2026-09-12.json)。镜像只保留在本机，没有推送 registry 或部署目标服务器。
- 脱敏后端/前端/Docker JSON、12 张新视觉图和本轮验收记录作为交付证据；临时配置、凭据、原始日志和数据库不提交。
- 本轮临时容器、网络、合成数据库/邮件/备份、配置和原始日志已清理；开发依赖、构建缓存及五个通过验收的本地镜像保留，合成资料可按手册重新生成。

镜像来自基于 7a932be 的修复工作树，汇总明确记录 modified_worktree，不把旧基线 SHA 标签冒充未修改源码；正式部署须由提交后的新 SHA 重新构建并固定版本。

## 本轮修复

- 修复新建作品异步提交后读取已失效的 `event.currentTarget`，避免保存成功却显示连接错误；新增成功后清空字段且无错误的浏览器断言。
- 历史归档改为无覆盖安装、校验复用及文件/目录同步；新增不连接数据库的只读校验工具，恢复演练不恢复用户已清除的历史可见性。
- Cloudflare purge 拒绝所有重定向，避免认证头被转发；失败保留备份并返回可重试错误。
- 修复 CI Python 收集路径，补充真实服务浏览器、缓存结果边界及自有进程异常清理合同。
- 修复 psql 16 忽略 quit 参数、拒绝加载 seed 却返回成功的缺陷。
- 允许迁移内建的空手动来源；已有真实批次、来源记录或非夹具资料仍拒绝 seed。
- 修复 API 时间运算、邀请 session、JSON 构造和后台概览的 PostgreSQL 参数推断错误。
- 修复 Worker 对账/巡检调度的时间参数类型，并使用真实库覆盖。
- 修复 Linux 下 Python Windows 常量类型检查与前端路径测试的跨平台假设。
- 修复 Ubuntu Chromium 中文纵排字形高度为零造成的桌面广告文字重叠；侧栏改为横排文字纵向堆叠，浏览器断言文本框非零且不重叠。
- E2E 媒体截图改用 Playwright outputPath，防止覆盖旧日期证据；js-yaml 4.3.2 补丁更新后生产及全量 npm 审计均为 0 漏洞。
- 修复 Nginx 主机变量替换过滤器；语法检查使用独立测试 DNS override。
- 修复两站 Docker 监听地址及 Next 缓存写权限；只授权缓存目录，保留非 root 运行与代码只读权限。
- 同步测试夹具、依赖锁和忽略规则；具体代码与验证见本轮证据。

## 仍需目标环境验收

1. 真实 R2 双桶权限、私有母版/派生图、multipart、上传 HEAD、主图替换、物理删除与容量回收。
2. Cloudflare route、私有 R2 binding、Cache API、purge、默认图传播以及旧公开入口关闭。
3. 可信账单、存储类别、版本历史、共享应用和供应商水位；Analytics 估算不是账户费用上限。
4. 日本 2C4G / 北京 2C8G 的资源限制、共享媒体卷、跨区调用、故障恢复和真实 SFTP 备份。
5. 最终域名/TLS/Turnstile、真实 SMTP 送达、权利邮箱、后台固定 IP/VPN/Access 和告警送达。
6. 在最终入口复验两站真实 API 浏览器业务与缓存；本地真实闭环已通过，真实资料视觉、LCP/CLS、跨区延迟和并发负载仍待验收。
7. 目标归档存储与恢复演练、版本标签一致性和完整上线执行记录；本地归档与只读恢复合同已通过。

目标环境准备与执行见 [联调清单](./RELEASE_A_PREP_CHECKLIST.md)、[运行手册](./RELEASE_A_RUNBOOK.md)和 [上线记录](./RELEASE_A_GO_LIVE_RECORD.md)。

## 历史证据入口

- [9 月 11 日本地全量回归](./evidence/release-a-local-regression-2026-09-11.md)与 [主图读取防御](./evidence/release-a-media-primary-read-guards-2026-09-12.md)。
- [账号生命周期](./evidence/release-a-identity-lifecycle-2026-09-10.md)、[角色会话](./evidence/release-a-role-sessions-2026-09-10.md)、[密码护栏](./evidence/release-a-password-work-2026-09-10.md)。
- [媒体发布](./evidence/release-a-media-publication-2026-09-10.md)、[主图替换](./evidence/release-a-media-primary-2026-09-10.md)、[逐对象退出](./evidence/release-a-media-retirement-2026-09-10.md)。
- [上传容量](./evidence/release-a-media-multipart-capacity-2026-09-11.md)、[私有边缘网关](./evidence/release-a-media-edge-gateway-2026-09-11.md)、[存储 Analytics](./evidence/release-a-media-storage-analytics-2026-09-11.md)。
- [备份历史验证](./evidence/release-a-backup-2026-09-10.md)、[部署静态验证](./evidence/release-a-deployment-static-2026-09-11.md)。

本轮删除了重复、过期的进度段落；历史证据、设计文档和原型保留其原有用途与执行日期。
