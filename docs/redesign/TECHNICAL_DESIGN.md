# 增量架构、接口与数据设计

版本 RD v1 / 2026-09-16。依据：[产品规格](./PRODUCT_DESIGN.md)、[决策边界](./DECISIONS.md)。下文“新增/拟定”为待开发合同，不表示当前 API 已支持；工程验证后如需改变设计，先记录变更。

## 1. 架构决定

保留日本单 PostgreSQL、Go API/Worker、独立 Next.js 公开站和运营后台、北京媒体处理及既有对象存储边界。不增加服务、Redis、搜索集群或新广告平台。默认沿用现有缓存失效 outbox，不让页面改版绕开发布和下架事务。

```text
公开 HTML：标题/更新时间/资料/链接 ← display-web ← Go 公开目录 ← published 视图
个人状态：收藏/关注/推荐偏好      ← 浏览器同域 API ← 会话与账号仓储（no-store）
内部过程：来源/审核/发布/下架      ← ops-web → 原权限/事务 → outbox → 缓存失效
```

前台简化与后台保留分层进行。公开 DTO 不新增 reviewer、author、raw_source、内部核验说明字段。搜索引擎读取的公开主体不能依赖用户 Cookie；个人数据不得进入共享 ISR/CDN 内容。

## 2. 已核对的现状与修改入口

| 模块 | 现状依据 | 本轮增量 |
| --- | --- | --- |
| 公开数据结构 | [catalog.go](../../services/platform-api/internal/catalog/catalog.go)、[handlers.go](../../services/platform-api/internal/httpapi/handlers.go) | 三类详情尚无公开更新时间；作品只有 studio_name，需加可点击厂牌对象 |
| 发布时刻 | [发布仓储](../../services/platform-api/internal/database/operations_store.go)、[发布表/视图迁移](../../db/migrations/00003_identity_publication.sql) | publication.published_at 在正式发布时写入，现有视图可读取 |
| 作品搜索 | [store.go](../../services/platform-api/internal/database/store.go)、[OpenAPI](../../packages/api-contracts/openapi.yaml) | 当前 q 为 2–100 字符；结果只有作品；保留兼容端点 |
| 关联作品 | [store.go](../../services/platform-api/internal/database/store.go) | 人物 100 条、厂牌 200 条内嵌截断，新增独立分页 |
| 登录 | [auth-client](../../apps/display-web/lib/auth-client.ts)、[登录表单](../../apps/display-web/components/login-form.tsx) | 当前默认回首页；新增受限目标恢复 |
| 广告 | [AdSlot](../../apps/display-web/components/ad-slot.tsx) | 当前无条件 placeholder；改为显式状态、空态不占位 |
| 偏好 | [PersonalizedWorkGrid](../../apps/display-web/components/personalized-work-grid.tsx) | 组件按 hidden ID 过滤，需由页面显式指定“推荐”场景 |
| 历史写入 | [account_handlers](../../services/platform-api/internal/httpapi/account_handlers.go)、[account_store](../../services/platform-api/internal/database/account_store.go) | 浏览聚合请求可附带账号历史写入；未来开关必须在服务端拦截 |
| 类型与测试 | [合约生成器](../../scripts/generate_openapi_types.py)、[CI](../../.github/workflows/ci.yml) | 合约源→生成类型→前后端与夹具同时更新；不手改生成类型充当实现 |

当前 Schema 为 v21。文档本身不增加迁移、不改 schema readiness 版本；确需变更时使用当时下一个空闲版本号，不能覆盖已应用迁移。

## 3. 公开更新时间与详情合同（DEV-03）

### 3.1 字段

| 合同 | 新增字段 | 类型/语义 | 兼容 |
| --- | --- | --- | --- |
| WorkDetailResponse | public_updated_at | RFC3339 UTC 字符串或 null；当前公开文字资料最近一次正式发布时刻 | 新增可选字段；新前端允许字段缺失 |
| PerformerDetailResponse | public_updated_at | 同上 | 同上 |
| StudioDetailResponse | public_updated_at | 同上 | 同上 |
| WorkDetailResponse | studio | StudioSummary 或 null，含 id/name/href | 保留 studio_name；无可公开厂牌则 null |

示例增量（不是完整响应）：

```json
{
  "public_updated_at": "2026-09-16T07:00:00Z",
  "studio": {"id": "00000000-0000-4000-8000-000000000001", "name": "示例厂牌", "href": "/studios/example"}
}
```

数据源采用当前 `site=main` 且 published 的 publication.published_at，经公开视图读取。它不等于实体表任意 updated_at、不等于核验/浏览/抓取/构建时间；新草稿或审核操作不更新前台日期。重新发布、回滚为新的公开版本时显示该次发布时刻，不倒退为历史版本最初日期。

本版“资料更新”限定为文字资料发布；独立媒体替换不自动刷新该时间。如果产品要求“图片也计入整个页面更新时间”，另加持久字段和所有媒体/发布事件的写入合同，不能取系统当前时间伪造。sitemap 的 lastmod 另按页面重要内容更新定义，不直接强制等同本字段。

公开发布记录按现有约束应有发布时间；读取异常返回可观测错误或兼容 null，不暴露内部详情。首轮无需新增表：扩展 Go 目录结构/查询/响应/OpenAPI，再接前端。若现有视图或权限不足，先做真实 SQL 合同验证后再确定最小迁移。

发布时正文和日期从同一公开快照读取；outbox 失效详情和依赖页。更新缓存后要一起看到新标题和新时间，不能只改时间。隐藏/下架仍优先于缓存与旧数据；不得为了显示日期重新开放草稿。

## 4. 搜索合同与分页（DEV-08/09/10）

### 4.1 端点规划

| 方法/路径 | 状态 | 合同 |
| --- | --- | --- |
| GET `/api/v1/search/works?q=&cursor=` | 已有，保留 | WorkSearchResponse；阶段 A 不破坏字段与归一化行为 |
| GET `/api/v1/search/performers?q=&cursor=` | 新增，阶段 B | `{query, items: PerformerSummary[], next_cursor}` |
| GET `/api/v1/search/studios?q=&cursor=` | 新增，阶段 B | `{query, items: StudioSummary[], next_cursor}` |
| GET `/api/v1/performers/{slug}/works?cursor=` | 新增 | `{items: HomeItem[], next_cursor}` |
| GET `/api/v1/studios/{slug}/works?cursor=` | 新增 | 同上 |

前端 `/search` 新增 type=work/performer/studio；省略时 work；非法类型给出明确提示或规范化到带说明的默认类型，不发任意端点请求。所有列表均只返回公开 published 实体；草稿/下架永不作为搜索候选。

q 继续为归一化后 2–100 个 Unicode 字符，空词由 UI 展示输入页。人物/厂牌姓名查询不能直接复用“至少两个字母数字番号”的 compactCode 守卫；分别验证 Unicode 名称，涵盖中文/日文样本，避免沿用作品编号限制而误拒绝名字。若单字名称需要支持，另改合约并补用例，不静默与 OpenAPI 分叉。

每页沿用 24 条、仓储多取 1 条决定 next_cursor；不承诺总数。人物排序：规范名精确→别名精确→前缀→其他文本匹配；厂牌：规范名精确→前缀→其他文本匹配。同档次用稳定归一化名称和 UUID 打破并列，不用随机排序。基于 PostgreSQL 现有能力，先用真实数据 EXPLAIN 验证，不为假定流量引入独立搜索服务。

新端点游标编码版本、场景、归一化查询指纹/父实体 ID、排序键和末项 UUID；校验长度、解码、类型、query/parent 绑定及版本。base64 不是权限措施，所有 SQL 仍参数化且从公开视图读。跨词、跨实体、非法游标返回 400 `INVALID_CURSOR`；不能吞错重复首页。空尾页返回 items=[] / next_cursor=null。

新游标保存排序元组而不是仅依赖“再查末项”：静态数据集完整遍历无重漏；游标末项删除后仍能继续。数据并发更新时不承诺快照一致，UI 支持刷新重查；不得将发生变更的结果集当作固定全集报告。排序规则变化升级游标版本，旧新版本处理需在发布说明中列出。

关联分页排序使用 `(release_date DESC NULLS LAST, published_at DESC, work_id ASC)`，所有元组比较方向必须与 ORDER BY 一致；未知日期进入独立尾部区间。下一页带真实 URL，父实体被下架时返回 404。已有 detail.works 暂保留兼容，客户端改用分页端点后再单独决定移除；本轮不无预警截短旧响应。

作品搜索已有行为与排序保留；新增排名或 match_reason 字段必须来自服务端真实依据，当前没有该字段则只描述实体类型。新实体搜索先不混入旧作品零结果指标；若扩展统计，按实体类型单独定义，继续只统计首屏、排除错误和分页、无原始查询长期日志。

## 5. 登录续接（DEV-04）

增加共享 return-target 校验工具（拟放在 display-web/lib 下），供登录、注册、重置页及按钮使用。只允许站内已知路由和白名单查询参数；拒绝外域、`//`、反斜杠、控制字符、非 http(s) scheme、编码绕过及嵌套 returnTo。统一解析后核对 origin/path，再输出站内相对地址；测试负例必须覆盖一次/多次编码。

URL 只放安全 `returnTo`，动作意图存当前标签页 sessionStorage：`{version, kind: favorite|follow, entityId, targetPath, createdAt}`；仅对本站明确点击写入，技术有效期 15 分钟。它不是认证凭证，服务端仍验证当前用户/实体状态/权限。URL、存储中不放邮箱、口令、验证码或反馈正文。

认证成功先回目标页；前端检查意图匹配和有效期，展示“继续收藏/关注”。用户确认后使用现有 POST 设置为收藏/关注，不使用切换型 toggle；重复请求应幂等，重试和已完成均有明确状态。完成、取消、过期清除意图；存储禁用时仍能返回页面，用户手动再点按钮。密码重置若未建立会话则回登录，不冒充已登录。

不能以 GET 请求自动变更关系；已有 Cookie、安全头、同域代理、CSRF/Origin 防护保持。页面测试覆盖 session 切换、目标下架、取消、重复提交与公共缓存无个人数据。

## 6. 推荐偏好（DEV-06）

保留 hidden_preferences 表和 `/api/v1/me/hidden-works` API 作为兼容存储，不把数据库命名当作全站屏蔽产品语义。将偏好读取抽成页面级共享能力；工作网格必须显式传入 `context=recommendation|catalog`（拟定组件合同）。只有 recommendation 过滤，默认 catalog 避免误伤主动查找。

推荐加载、失败、撤销状态统一；不能在搜索 UI 先过滤掉命中项再报告“零结果”。现有关注作品仓储也可能过滤 hidden_preferences，必须按产品矩阵逐个检查前后端，不能只改搜索组件。管理员 publication_status 过滤继续保留在所有仓储层。

私有响应 `no-store`；匿名目录无需新增身份跟踪。收藏/关注/偏好更新后只刷新个人状态；不要使整站公开详情缓存因用户偏好而分裂。

## 7. 广告状态层（DEV-05）

将 AdSlot 从“总是占位”改为受控状态输入：empty/ready/error/preview。ready 需要有效素材、明确位置、目标地址、已确认可展示时间；没有素材的数据源一律 empty。现阶段只接本地受控配置或测试 fixture，不新增广告库/后台管理接口。

empty/error 返回空，不留固定空白；ready 才输出明确广告标识、可访问名称、确定尺寸；不按登录状态隐藏。素材地址和目标地址按允许范围验证，不渲染任意 HTML/脚本。预览由受保护测试环境或组件夹具启用，生产构建/配置禁用；第三方 SDK 仍禁止，未来接入须单独审查 CSP、媒体和隐私。

服务端能确定 ready 时预留确定尺寸；不能确定时不异步突插首屏广告挤动主操作。正文请求不依赖广告成功。默认无广告回退无需数据库迁移。

## 8. 历史策略后续设计（DEV-12，阻塞）

Q-01 定稿前只做说明，不改变默认记录或删除已有数据。后续拟增加 GET/PATCH `/api/v1/me/preferences`，字段至少有 history_sync_enabled、policy_version、updated_at；确定旧用户迁移和保留规则后才冻结 OpenAPI、错误码和迁移 SQL。

若按主动开启落地，新用户写入默认关闭，旧用户如何迁移须明确；服务端 RecordHistory 路径统一检查，不能只停用前端 reporter。匿名小时浏览聚合与账号逐条历史分离，关闭历史不能暗改前者口径。设置更新返回成功后发起的新历史请求不得写入；同一用户的并发写入与设置应采用事务性顺序控制并测试竞争。

清除可见历史、停止未来记录、到期删除在线行、归档删除、备份恢复是不同动作。未来有限保留必须覆盖在线表、归档 manifest/对象和恢复规则；不能仅删除可见页面。新偏好生效后，回退到忽略开关的旧 API 会恢复写入，属于不可接受回退：应关闭历史写入或前向修复，不能简单回滚二进制。具体期限未知，暂不提供可执行清理任务。

## 9. SEO 技术基础与发布隔离（DEV-13/14）

采用[正式 SEO 基线](../PRODUCT_SEO_PLAN.md)和 CHG-20260916-01：D-08/D-09 已按用户授权确定，本节是待开发合同，不再等待 SEO 产品选项。本轮仍只更新文档，不连接外部工具。

- 共用正式 origin 生成 canonical/OG/JSON-LD/sitemap URL；运行环境缺配置在部署预检中报错，不默默输出 localhost。测试 origin 不进入正式地图。
- 独立的正式环境识别与索引放行配置必须同时满足，不能仅凭 production build 推断可以索引；缺配置默认不放行。中文 lang=zh-CN，无真实替代语言页不输出 hreflang；不新增地区重定向。
- 不同内容分页自引用，不归并第一页；真实链接可抓取；追踪参数与内容参数分开处理。
- 将 HTTP 状态通过可控制响应的层实现并用最终生产构建入口验证；流式页面已发送 200 后再显示错误不是 503。首次响应与缓存回源故障分别测，不能仅断言错误组件文本。
- 先诊断此前预览 robots/子 sitemap 的 500，保存制品、配置与日志证据；不将 SITE_URL 猜测写成已确认根因。
- sitemap 增加确定性分片/继续读取、索引资格输入和数量/体积校验；测试 50,001 条以及接近 50 MB 的文件。策略按已定矩阵执行，正式放行前只在受保护环境验证，不提交预览地图。
- 保留 CreativeWork/Person/Organization；面包屑标记与真实可见导航一致；不为 SEO 新增来源面板或虚构评分/视频。
- private/test/demo 访问控制独立于 robots/noindex；D-08/D-09 的正式规则确定不等于部署已通过。账号页面、演示夹具、审查网页不随正式站开放进入索引。

### 9.1 共用 SEO 策略合同

新增服务端纯策略层（具体文件名开发时决定），输入环境/origin、页型、公开状态、内容质量资格、实际内容分类、媒体外部预览许可；输出 canonical、indexable、sitemapEligible、adultRating 和 imagePreviewMode。HTML metadata、XML、JSON-LD image 与 OG 共同消费，避免前端允许而地图/图片走另一套规则。`sitemapEligible` 必须包含 indexable 且排除普通分页；不能把 published 直接等同 indexable。

内容分类使用明确的 `unknown/non_explicit/explicit` 枚举；它与“人物已成年”不同，不能从 adult_status 或 18+ 标签直接推导。外部图片预览许可使用 `unknown/allow/deny`；allow 还必须同时满足公开派生图、可正常访问、用途获准和非露骨。unknown/deny 不进入预览允许清单；网页内容分类 unknown 暂不开放索引，但不把内部分类状态暴露为前台审核面板。

这些是新的内部合同要求，不能声称现有 Schema 已有这些列。DEV-14a 先检查既有出版/媒体元数据能否可靠提供；不足时新增最小持久分类和许可字段/迁移，默认 unknown，编辑权限沿用原内部权限，输出公开 DTO 时不泄露原因/操作人。保留变更审计。存量资料须在正式切换前抽样和回填，记录数量及 noindex 影响；不得不检查就全量改 allow 或上线造成全站意外去索引。

技术实现可分为内容侧资格（Go）与环境/页型限制（Next），最终可索引结果为各条件的交集。禁止对外客户端参数直接设置资格。环境禁索引必须覆盖所有页型；生产启用后仍由页型与内容策略排除搜索/工具/私有/测试内容。

### 9.2 图片预览与内容分类输出

- 合格普通页面使用 `max-image-preview:standard`；含受限图片时更保守使用 none/noimageindex，并按需要在实际可抓取资源响应设置 X-Robots-Tag: noindex。Google 特定语义按官方文档验收，其他引擎另验，不能承诺所有机器人遵守。
- 含露骨内容页面输出 rating=adult；正文有索引资格不等于图片获准。无合格实体预览图则 OG 使用站点品牌图，JSON-LD 不带受限图片；图片 sitemap 首期关闭。
- 图片响应策略覆盖公开派生图服务/CDN 副本，不触碰私有母版授权。页面 noimageindex 不代替资源级限制、实际删除或缓存失效；OG 不受 Google robots 指令统一约束，要独立检查。
- 页面资格、分类或媒体许可变化时，同步失效 HTML、地图、OG/JSON-LD 和媒体响应缓存。权利下架保留原最高优先级撤回链路，不只是改 meta。身份与属性必须来源于真实内容判断，不做爬虫专供的不同正文。

### 9.3 发布准备与搜索工具

DEV-14b 在最终 HTTPS origin 核对完整索引矩阵、去索引指令、HTTP、图片头及缓存。DEV-14c 由授权负责人验证 Search Console 所有权并提交正式 sitemap 索引；本地模拟账号或测试域不能替代。不能因为暂时没有工具权限而撤销已确定的产品方案，应记录部署准备缺项。

每周观察收录异常、展示/点击与页面类型，真实数据不足明确标记；不承诺通过工具测试就必然收录、排名或富结果。目标地区依真实数据迭代，首期不伪造国外或国内流量占比。

## 10. 部署兼容与回退

| 改动 | 推荐顺序 | 回退条件 |
| --- | --- | --- |
| 新时间/厂牌字段 | OpenAPI/后端附加字段 → 新前端；旧字段保留 | 可回旧前端；新前端须容忍旧 API 缺字段 |
| 新搜索/关联端点 | 后端与索引先上 → 验证 → 前端开放入口 | 前端关闭新类型/分页入口，保留旧 API；不能让旧内嵌截断冒充完整列表 |
| UI/广告/偏好 | 测试矩阵通过 → 同批替换相关页面 | 广告可关闭；偏好回退不得重新让搜索“找不到”而无说明 |
| 历史生命周期 | 政策→迁移→服务端强制→前端→说明→恢复演练 | 不撤销用户选择或复活已清除记录；删除操作通常不可通过 down 恢复 |
| SEO | 先技术验证，后经确认的正式 origin/索引配置 | 保留旧址 301，不回退成无保护测试站；工具提交不是代码回退 |

发布记录必须固定提交 SHA/制品版本，说明迁移和缓存动作。DB 索引新增先在代表性数据上验证查询计划、写入影响与索引构建方式，再进入目标环境；不对生产库执行试验性 SQL。既有图片、权利下架和两地部署验收继续独立执行。
