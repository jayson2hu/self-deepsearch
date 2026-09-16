# 幕鉴 SEO 优化方案

日期：2026-09-16
状态：正式产品 SEO 开发基线（用户授权采用推荐方案）；尚未实施到产品代码，尚未提交搜索引擎。
关联：[产品评审](./PRODUCT_REVIEW_2026-09-16.md)、[设计任务书](./PRODUCT_REDESIGN_BRIEF.md)、[实施清单](./PRODUCT_REDESIGN_BACKLOG.md)。

审核后修订（2026-09-16）：用户补充“seo的意见，按照正式的推荐来即可，符合正式的产品”，据此通过 [CHG-20260916-01](./redesign/DECISIONS.md)将 D-08/D-09 落为下列正式推荐基线。本文件定义 SEO 政策，工程合同见[技术设计](./redesign/TECHNICAL_DESIGN.md)，实施见[DEV-13/DEV-14](./redesign/DEVELOPMENT_PLAN.md)。D-07 继续有效：正文展示公开资料标题/名称和更新时间，不新增来源/核验面板。开发可继续，不再等待 SEO 产品选项；实际域名、账号权限、媒体条件与发布验收仍要具备，本次不执行对外开放或提交。

## 1. 目标与优先顺序

SEO 的目标是让真正需要作品、人物和厂牌公开资料的人，通过自然搜索进入准确、可追溯的页面，完成查询并愿意继续使用。优化不承诺收录量、排名或流量，也不以播放、下载等本站无法满足的搜索意图吸引访问。

先解决抓取和地址信号，再改善落地页与内部链接，最后用实际搜索数据迭代内容。原规划的“有效收录率 ≥70%”保留为待验证的观察目标；没有确认目标页面集合和获得 Search Console 数据前，不称为已达成指标。

首期按中文资料产品开发，面向中文阅读需求，不预设已经验证的高流量地区。以 Google Search Central 官方规范和 Google 搜索工具作为首批 SEO 技术验收与效果观察基准，保持标准 HTML/HTTP/XML 的通用兼容；其他引擎不默认享有同样的解析或展示效果。实际获客地区由后续数据验证，不能将“规范基准”写成已证明 Google 是最大流量来源。

### 1.1 正式产品配置基线

| 项目 | 本版规则 | 性质 |
| --- | --- | --- |
| 语言 | 中文界面与正文，HTML lang=zh-CN；可保留实体原文名与别名 | 当前产品范围，不创建机翻语言分站 |
| 地区 | 不新增国家定向或自动地区跳转，原访问限制不变 | 暂无市场数据，不虚构目标国家 |
| 搜索引擎 | Google 官方机制首批必验；其他引擎按真实需求补验 | 技术验收选择，不是排名/流量承诺 |
| 多语言 | 无独立翻译页面不添加 hreflang，不复制同内容语言 URL | 后续真有多语言产品再单独设计 |
| 正式域名 | 唯一 HTTPS origin，现有 SITE_URL 及部署入口一致 | 真实域名由部署提供，不虚构示例域名为正式地址 |
| 公开索引 | 采用第 4 节矩阵与统一资格判断 | 已确定产品规则；环境放行后才能生效 |
| 图片/分享 | 首期标准尺寸预览；仅有授权、非露骨且适合外部展示的公开派生图 | 第 5.3 节给出保守默认，不放开私有母版 |
| 成人内容 | 按实际内容分类，适用页面使用 adult 标记，不规避 SafeSearch | 第 6 节；未知分类不能默认安全 |
| 外部工具 | 站点负责人验证 Search Console，提交正式 sitemap；按周观察 | 工具权限与上线准备，不是额外 SEO 产品决策 |

以上包含产品取舍，并非 Google 对每个站点的统一强制配置。若未来明确中国大陆为核心市场，应新增相应目标引擎和实际访问条件的专项验证；不据此自动迁移域名或部署。没有真实翻译/市场证据时保持本版范围。

## 2. 已有基础与本轮发现

已经有服务端输出的详情正文、作品/人物/厂牌 JSON-LD、Open Graph、站点地图索引、分实体 sitemap、旧地址 301 查询、搜索及账号页 `noindex`、图片派生尺寸与默认图。下一步是在这些能力上补齐一致性，不能将本方案写成“从零增加 SEO”。

| 发现 | 证据 | 需要处理的影响 |
| --- | --- | --- |
| 搜索页设 `noindex`，同时被 robots 禁抓 | [robots](../apps/display-web/app/robots.ts)、[搜索页](../apps/display-web/app/search/page.tsx) | 禁抓会使爬虫无法读取页面上的去索引指令；不能把 robots 当作删除索引机制 |
| 公开 robots 默认允许抓取，没有在该文件中按测试/生产环境分流 | [robots](../apps/display-web/app/robots.ts) | 测试环境需独立访问控制与索引保护；实际部署边缘是否另有保护需验证 |
| 相对 canonical 与绝对 sitemap/JSON-LD 地址采用不同生成路径 | [根布局](../apps/display-web/app/layout.tsx)、[作品详情](../apps/display-web/app/works/[slug]/page.tsx)、[域名工具](../apps/display-web/lib/site.ts) | 统一正式 HTTPS origin 与 URL 规范；不能假设相对地址已经指向错误域名 |
| 厂牌列表所有游标页都声明 `/studios` canonical | [厂牌列表](../apps/display-web/app/studios/page.tsx) | 内容不同的分页不能一律归并第一页；参数处理要区分分页与重复追踪参数 |
| 详情服务失败返回普通提示页面，未在组件中设置 503 | [作品详情](../apps/display-web/app/works/[slug]/page.tsx)、[人物详情](../apps/display-web/app/performers/[slug]/page.tsx) | 需通过故障注入核对最终 HTTP；避免故障内容以 200 代替正常页面 |
| 每类 sitemap 请求最多取 50,000 条，未有分片继续读取 | [站点地图处理](../services/platform-api/internal/httpapi/handlers.go)、[查询](../services/platform-api/internal/database/store.go) | 超过上限时会遗漏；URL 数与未压缩文件大小都要控制 |
| 作品 description 基本只包含番号与固定文案 | [作品元信息](../apps/display-web/app/works/[slug]/page.tsx) | 搜索摘要没有充分表达人物、厂牌、日期和资料依据 |
| 作品厂牌为文字，人物/厂牌关联作品有数量上限 | [作品页](../apps/display-web/app/works/[slug]/page.tsx)、[目录查询](../services/platform-api/internal/database/store.go) | 部分关联页发现路径不完整，长尾详情难以持续被找到 |
| 已有匿名站内统计，未获得搜索引擎效果数据 | [原指标定义](../PRODUCT_PLAN.md) | 站内搜索零结果率不能代替自然搜索表现 |

本轮另对现有 `127.0.0.1:13000` 预览容器做了只读 HTTP 抽查：`/robots.txt` 和 `/sitemaps/works` 返回 **500**；首页、最新、最近收录和人物列表未返回 canonical；厂牌页及 `?cursor=invalid` 均为 200，并返回相对 canonical `/studios`；搜索页返回 `noindex, follow`。

这组观察只代表该预览容器当时的响应；未核对其制品与当前源码完全一致，也不是最终域名验收。500 的具体根因尚未确认，需要部署检查及运行时诊断；本轮没有擅自修改环境配置。正式上线前应在最终入口重新验证。

## 3. 关键词与落地页策略

围绕实体和任务建立有限的查询集，不先追求大量页面。以下例子使用虚构编号与人物，不是搜索量数据。

| 搜索意图 | 查询例子 | 主要落地页 | 页面必须解决的问题 |
| --- | --- | --- | --- |
| 确认作品 | `TEST-001`、编号变体、编号加人物 | 稳定作品详情 | 正确身份、版本差异、日期、人物、厂牌和资料依据 |
| 确认人物 | 公开艺名、原文名、别名 | 同一人物的 canonical 详情 | 别名对应关系、公开字段、关联作品与资料更新时间 |
| 查相关发行 | 厂牌名称、厂牌加作品编号 | 厂牌页或作品页 | 厂牌身份、实际关联、可继续翻页的作品列表 |
| 持续浏览 | 已核验的发行分类或编辑主题 | 人工维护的固定专题 | 独立介绍、明确选入理由、持续维护与足量内容 |
| 寻找播放或下载 | 播放、磁力、资源下载等 | 不专门建设此类获客页 | 站点边界保持准确，不能用误导标题引流 |

首轮结合 R-12 建立约 20–30 条人工标注查询，覆盖号码变体、别名和多版本。之后只用实际工具可获得的数据补充需求；不编造搜索量、关键词难度或流量预测。

一个真实实体尽量维护一个主页面；别名和编号变体在同页解释，不批量生成只换词的“SEO 页面”。有独立信息的不同版本保留不同页面，并显示关系，不能为合并权重错误地合并实体。

专题先做少量可维护主题。没有独立介绍、有效筛选逻辑和持续更新责任人的标签组合，默认不作为新的索引落地页。由机器协助写作时仍须校验事实和来源，不批量制造无法满足用户任务的模板内容。这是结合本站维护能力提出的内容策略，参考 [Google 的实用内容指导](https://developers.google.cn/search/docs/fundamentals/creating-helpful-content?hl=zh-cn)。

## 4. 索引、地址与状态规则

### 4.1 正式页面索引矩阵（D-09 已按推荐方案确定）

| 页面类型 | 抓取/访问 | 索引与 canonical | sitemap |
| --- | --- | --- | --- |
| 正式首页、latest/recent/performers/studios 的目录首页 | 允许访问与抓取 | 有真实内容时 index；绝对自引用 canonical；空目录暂 noindex | 纳入符合条件的主地址 |
| 已发布且达到质量门槛的作品/人物/厂牌 | 允许抓取 | index；绝对自引用 canonical | 纳入 |
| 已公开但信息不足、关系未核实的资料 | 如允许公开则可抓取 | 暂 `noindex`；补齐后再明确切换 | 暂不纳入 |
| 站内搜索结果 | 允许读取 `noindex`；限制无限参数扩张 | `noindex`；不把不同搜索词统一 canonical 到首页 | 不纳入 |
| 排序/筛选组合、热门临时榜单 | 允许读取 noindex，同时减少无限组合链接；不依赖禁抓来去索引 | noindex；重复参数页才使用真实主地址 | 不纳入 |
| 内容不同且有结果的合法列表分页 | 真正的下一页 href，稳定且有界的地址 | index；各页绝对自引用，不全部指向第一页；无效/空尾页不入索引 | 不纳入，保留站内发现路径 |
| 固定专题（后续有真实维护内容时） | 沿用已有发布边界，不批量生成 | 独立内容充足才 index；本轮不新建专题系统 | 符合条件时纳入 |
| 关于资料页 `/about` | 匿名可读 | 实际站点介绍完整时 index、自引用 | 纳入正式有效页 |
| 隐私说明、登录/注册/重置等工具页 | 说明匿名可读，账号操作按原安全边界 | noindex；不是获客落地页 | 不纳入 |
| 收藏、历史、账号、运营页面 | 服务端认证/授权 | noindex 是补充，不能代替访问控制 | 不纳入 |
| 测试站、合成资料、审查 HTML | 认证或受控访问 | noindex；不提交搜索引擎 | 不纳入正式 sitemap |

“可公开”“可索引”“应提交 sitemap”分别判断：可公开不自动等于可以索引；可索引分页也不自动纳入地图。质量门槛不按字数凑量，也不把有图片作为必要条件；正常默认图与未知发行日期不单独剥夺有效资料的索引资格。

详情可索引必须同时满足：正式环境已允许开放索引；实际 HTTP 200；当前公开 published 状态；稳定有效 canonical；有可区分的真实标题/名称和作品编号或足以确认实体的公开字段/关系；内部记录能支持主要事实；无未处理的发布/权利限制；内容分类已确定。仅有孤立名字、占位标题、合成测试或错误页不合格。`public_updated_at` 缺失不能用当前时间补，也不单独代表内容虚假；记录质量问题并按实际身份/字段判断。

此资格由统一服务端策略供 HTML、地图和发布验收共用，不额外公开审核状态。资料补齐后可重新评估；已不符合条件的页从地图移除并输出正确响应/noindex。未发布、私有、权利下架先做访问/状态处置，不能只加 noindex 继续展示。

robots 控制抓取，`noindex` 控制索引；二者不能随意叠加后期待爬虫看到指令。对已经收录的页面，应设计可抓取的去索引响应、必要的工具流程及验证；密码保护和权限判断继续独立生效。搜索引擎临时移除工具不替代真实删除或正确响应。机制依据：[Google noindex 指南](https://developers.google.cn/search/docs/crawling-indexing/block-indexing?hl=zh-cn)。

### 4.2 主域名、canonical 与分页

- 确认唯一正式 HTTPS origin；主机变体、HTTP→HTTPS 和 URL 格式按统一规则跳转，避免链式跳转。
- canonical、Open Graph URL、JSON-LD `@id`、sitemap loc 和内部链接由同一套正式地址规则生成。建议输出绝对 canonical；部署门禁拒绝 localhost、测试域和不匹配主机。
- 保留现有稳定 slug 与旧址 301 能力。改标题和艺名不应无必要地改 URL；确需调整时旧址直接跳到新的相关实体，不把所有旧页导向首页。
- 合法追踪参数可以归一到无追踪参数主地址；不同分页和不同版本不能因此被归并。`canonical` 是给搜索引擎的信号，不是强制指令，也不是访问权限。
- 不要求仅为 SEO 将所有游标分页重写成页码，但地址必须可重复访问、没有无限组合，页面有真正可抓取的链接。非法游标不得静默返回相同内容并伪装成功；应约定明确的错误或规范化策略。
- 如果前端以后采用“加载更多”，仍需提供实际 URL 的顺序翻页入口。不能只依赖点击按钮、滚动事件或客户端个性化让重要详情被发现。

地址与分页规则参考 [canonical 指南](https://developers.google.cn/search/docs/crawling-indexing/consolidate-duplicate-urls?hl=zh-cn)及[分页与增量加载指南](https://developers.google.cn/search/docs/specialty/ecommerce/pagination-and-incremental-page-loading?hl=zh-cn)。

### 4.3 HTTP 与生命周期验收

| 情况 | 正式默认响应及行为 |
| --- | --- |
| 正常且有资料 | 200，主体、元信息和公开状态一致 |
| 地址已变且有真实对应实体 | 直接 301 到有效 canonical，避免循环与多跳 |
| 不存在或已永久移除且无替代资料 | 真实 404；确定永久撤销时可选 410，页面提供合理导航 |
| 暂时上游故障 | 能准确返回 503 的处理层，必要时 Retry-After；不缓存故障正文为正常资料 |
| 资料下架 | 按权限与权利流程及时撤下，并同步搜索、sitemap、图片及缓存；不用改标题“已删除”继续返回正常页 |

详情故障、非法游标及隐藏/合并场景要从最终 HTTP 入口验证；React 页面显示什么不等于 HTTP 状态正确。已有 Next、Worker 和边缘缓存的失效链路应纳入同一验证。搜索侧处理机制参考 [HTTP 状态与网络错误说明](https://developers.google.cn/search/docs/crawling-indexing/http-network-errors?hl=zh-cn)。

### 4.4 站点地图完整性

保留现有 sitemap 索引与分实体结构，增补稳定排序、继续读取与分片，避免每类只截取前 50,000 条。单文件不得超过 50,000 个 URL 或 50 MB 未压缩大小；选择更小的运维分片阈值并在边界样本上验证无重无漏。只提交允许索引、返回正常资料的正式 canonical；`lastmod` 反映内容的实际重要更新，不在每次请求时刷新所有日期。新增、合并、下架与缓存失效要同步生效。依据：[Google sitemap 指南](https://developers.google.cn/search/docs/crawling-indexing/sitemaps/build-sitemap?hl=zh-cn)。

## 5. 页面内容、内链和媒体

### 5.1 标题与摘要模板

作品标题模板：`番号｜准确作品标题｜幕鉴`；人物/厂牌在有区分价值且长度可控时加入，不把所有字段都塞进 title。人物模板：`公开艺名｜别名与关联作品｜幕鉴`；无别名或关联作品时省略未具备的描述。厂牌模板：`厂牌公开名称｜作品资料｜幕鉴`；同名实体使用已经核实的区分信息，不编造所属地域或经历。

作品摘要示例：“TEST-001 的公开发行资料：发行日期 YYYY-MM-DD，关联人物某某，厂牌某某。查看公开资料与相关作品。”字段未知时省略，不写虚构日期、不堆关键词、不诱导不存在的资源下载。

title、H1 和正文表达一致，但不要求机械完全相同；摘要应针对实体而不是只替换番号。优先以搜索预览和真实标题展示检查可读性，不把固定字符数当成排名规则；搜索引擎可能改写标题或摘要。

正文应可在初始服务端 HTML 中读取到目标身份、关键公开字段、关联链接与真实资料更新时间（未知则省略）。公开静态主体与用户私人收藏/偏好分开，避免抓取内容依赖登录或加载个人状态。D-07 不要求公开来源/核验面板。

### 5.2 内部链接与内容维护

补齐“作品→人物/厂牌→关联作品”、明确的面包屑、相关资料理由和列表翻页。导航锚文本描述目的，如“查看某人物的作品”，避免所有入口都叫“点击这里”。核心资料不能只有 sitemap 入口而没有站内发现路径。

新增资料进入既有发布目录或真实关联页；来源与核验日期内部保留，前台资料更新时间按公开发布合同显示；别名变更、合并、下架有明确链接处置。每期抽样检查孤立页、坏链、错误关联和过深路径。点击深度是产品检查指标，不承诺某个深度必然提高排名。

本轮不新增来源链接清单；若未来需要公开来源标题/链接，先完成 Q-06 的确认。不批量购买外链、不制造站群或交换低质量链接。品牌介绍、资料方法和联系渠道要准确，不能用“专家审核”等没有依据的声明增加信任。

### 5.3 图片和结构化数据

保留当前 WebP 派生、明确宽高、`srcset` 和默认图。主图的 alt 描述真实对象，装饰性图片可以留空；不把关键词列表放进 alt。需要被图片搜索发现的有效图片必须在已授权展示范围内可正常抓取；不因 SEO 放开私有母版或绕过媒体保护。

正式默认：只有已经允许公开、具有外部搜索/分享展示条件、分类明确且非露骨的派生图可进入搜索与 OG 预览；素材来源/条件不清或涉及露骨图片时，OG 使用站点品牌图而非实体图片，结构化数据不另行指定受限图片。无合格图片仍可索引合格文字资料，不拿图片数量凑质量。

首期合格页面采用 `max-image-preview:standard`，不默认申请大图预览；含受限图片的页面采用更保守的 `max-image-preview:none` 与 `noimageindex`。限制图片搜索时还需在可抓取的图片资源响应使用 `X-Robots-Tag: noindex` 等适用规则并覆盖实际公开副本；页面级 noimageindex 不能替代对其他引用路径、资源响应和缓存的处理。这些是搜索展示指令，不是保密或版权访问控制，也不约束所有社交平台。

OG 预览、图片 sitemap、JSON-LD image 与媒体响应采用同一允许清单，避免另一通道重新暴露禁止预览的图片。首期不单独开启 image sitemap，页面可抓取 img 与普通 sitemap 足够作为起点；后续确有需求再补，不提交默认占位或私有母版。已下架对象按既有物理删除和缓存机制处理，不只移除 meta。

图片发现参考 [Google 图片指导](https://developers.google.cn/search/docs/appearance/google-images?hl=zh-cn)；预览/去索引指令的含义与限制参考 [robots meta 和 X-Robots-Tag](https://developers.google.cn/search/docs/crawling-indexing/robots-meta-tag?hl=zh-cn)。上述保守的图片选择是本产品首期策略，不是官方要求所有成人资料站禁止图片搜索。

保留准确的 `CreativeWork`、`Person`、`Organization` 语义；有可见面包屑后再添加匹配的 `BreadcrumbList`。字段必须和页面可见内容一致；不为争取搜索样式伪造评分、评论、问答或发行日期，资料索引站不添加不存在的 `VideoObject`。

“JSON-LD 可解析”“Schema 语义正确”“Google 支持某种富结果”“该页面符合展示政策”是四件不同的事。普通 `CreativeWork`/`Person`/`Organization` 标记不保证获得专属富结果，成人或露骨内容还需单独核对政策。准确性、可见性和展示资格参考 [Google 结构化数据通用政策](https://developers.google.cn/search/docs/appearance/structured-data/sd-policies?hl=zh-cn)。

## 6. 成人资料的搜索展示边界

SEO 不改变“仅提供成年人的公开资料、经审核图片、无播放下载与资源跳转”的产品边界，也不承诺绕过 SafeSearch。

按实际正文、图片和页面上下文判断是否涉及露骨内容；页面写有“18+”和 JSON-LD 的 `isFamilyFriendly: false`，都不能代替搜索引擎的分类机制。确定包含露骨内容的页面输出 `<meta name="rating" content="adult">`；不确定的资料暂不开放索引并回到内部处置，不自动标为普通内容，也不为流量隐瞒内容性质。仅凭 18+ 提示不武断认定全站所有页面属于同一分类；搜索引擎仍可能按自身规则作更广的分类。依据：[Google 露骨内容网站指南](https://developers.google.cn/search/docs/specialty/explicit/guidelines?hl=zh-cn)。

如未来出现独立的大规模露骨内容专区，按官方指导评估内容分区/子域；本轮不因假定流量迁移主域或批量改 slug，也不以换域规避分类。官方指南对年龄门槛下的 Googlebot 抓取有专门建议；本产品本轮选择保持既有真实访问限制，不直接豁免爬虫。确需调整抓取访问方式时单独核对访问责任与发布方案，不将无法访问的页面承诺为可索引页面，也不提供实质不同的爬虫专用正文。

测试应分别观察正文、缩略图、社交分享和搜索样式；不要把一般关键词的曝光预期照搬到可能受 SafeSearch 限制的资料。广告和结构化数据也有各自政策，获得收录不等于获得富结果或广告资格。

## 7. 性能与效果验证

### 7.1 性能

保留已有 LCP 与 CLS 门槛，补充交互响应 INP。常用的良好体验目标为 LCP ≤2.5 秒、INP ≤200 毫秒、CLS ≤0.1；方案要求以真实用户数据的第 75 百分位分别观察手机和桌面。这些是待验收指标，不代表当前线上已达成。阈值参考 [Google Core Web Vitals 说明](https://developers.google.cn/search/docs/appearance/core-web-vitals?hl=zh-cn)。

原本无网络限速的合成预览与本地 HTTP 延迟测试不能替代实际用户指标。试运行时检查长标题、真实图片、弱网、慢设备、搜索提交和展开交互；未取得足量真实用户数据时显示“数据不足”，使用实验室诊断辅助，不伪造 CrUX 结果。

### 7.2 搜索效果

上线后再由有权限的站点负责人验证 Search Console 所有权并提交正式 sitemap；本轮不连接账号、验证域名或对外提交任何地址。初期每周核对抓取/索引异常，内容方向用可比时间窗观察，避免一天的波动引发大规模改版。

| 指标 | 使用方式 | 注意事项 |
| --- | --- | --- |
| 可索引页面集合 | 已发布、达到质量门槛、允许索引的 canonical 页数 | 与全部已发布资料数分开，先明确分母 |
| 有效收录 | 工具报告与代表性 URL 检查结合，按实体类型观察 | 报告存在延迟和覆盖边界，不把已提交数量当已收录 |
| 自然搜索展示/点击/CTR | 在同一引擎、地区、设备、页面类型与相近时间窗下比较 | 区分品牌/番号精确意图与其他意图；平均排名不能单独作为成功结论 |
| 标题或正文改动效果 | 固定一批页面、记录修改日期、观察可比窗口 | 标记同时发生的内容更新、季节性与抓取延迟；小样本不承诺因果 |
| 抓取和规范化健康 | sitemap/robots 状态、选定 canonical、404/5xx、重定向链 | 应与真实错误及目录变动对照，不把所有未收录都当故障 |
| 页面体验 | 手机/桌面的实际 LCP、INP、CLS 与任务完成情况 | 不用单次 Lighthouse 分数代替真实指标 |
| 用户价值 | 受控任务正确完成、站内继续浏览、明确允许的账号使用 | 原有匿名无标识边界不变；不能声称已具备完整 SEO→注册→留存归因 |

原规划只做不含查询词的匿名聚合。未来引入 Search Console 数据同样需要确定访问人、聚合粒度与保留方式；敏感查询可能带有个人兴趣，不把原始查询批量放进公开报表或长期行为库。

## 8. 分期与验收

| 阶段 | 主要工作 | 对应候选项 | 通过条件 |
| --- | --- | --- | --- |
| SEO-A：发布前基础 | 按已定中文/Google 技术基准和索引矩阵配置正式域名、robots/noindex、canonical、HTTP、成人展示边界 | D-08、D-09 已确定；DEV-13/DEV-14 | 最终入口返回正确响应；测试站受保护；不泄露私人页或错误主域 |
| SEO-B：内容与发现 | 元信息模板、公开资料及更新时间、实体内链、分页、sitemap 分片、图片和结构化数据 | R-16、R-17；执行时映射 DEV-03/10/13 | 代表性实体可被正确发现；地图完整且主地址一致；标记与正文一致 |
| SEO-C：试运行与持续维护 | 实际性能、搜索工具验证、质量抽样、发布与下架检查 | R-19、R-20 | 有真实基线和负责人；指标口径清楚；出现异常可以定位并回退 |

每阶段保留 URL 样本与 HTTP/渲染证据，至少包含：正常详情、缺图、字段未知、同名/多版本、旧地址、搜索结果、合法第二页、非法参数、404、隐藏/下架、上游故障、测试域、图片保护状态。

SEO 的发布检查是已有发布门槛的补充，不替代真实资料审核、隐私、媒体与部署验收。无最终域名或明确索引授权时，可以完成方案和离线验证，但不能以预览地址提交站点地图。

### 8.1 上线执行清单

1. 固定提交/制品、真实 SITE_URL、环境类型和索引开放配置；配置缺失默认不开放索引，不能靠 NODE_ENV=production 推断是正式公网。
2. 在受保护预发验证第 4 节每种页型、真实 HTTP、metadata/XML 一致性、内容分类、图片资源头和下架缓存链路。
3. 按原 Release A 运行门槛放行最终入口；仅正式入口移除环境级索引限制，测试/审查/demo 和账号保护保持。
4. 由具备权限的负责人验证正式域名工具所有权，再提交唯一正式 sitemap 索引；记录提交日期与样本，不承诺立即收录。
5. 首月按周查看抓取/索引异常与搜索展示/点击，之后保持固定可比周期；数据不足如实标记。负责人未分配属于上线准备缺项，不重新把 D-08/D-09 标为产品待决策。

上述步骤是后续开发/部署工作，不是本次已经执行的结果。

## 9. 官方参考与访问范围

2026-09-16 首轮已通过 Google 官方 `developers.google.cn` 中文站读取 10 份指南；本次再读取 robots meta/X-Robots-Tag，并复核 noindex、分页和露骨内容，共列 11 个官方入口。此前访问 `developers.google.com` 和 `web.dev` 超时，故采用官方中文入口；实施时仍需复核目标引擎的最新要求。不将搜索引擎规则视为本站已实现能力，也不将技术建议当作收录或排名保证。中文范围、首批引擎基准、保守图片预览及保留年龄访问限制属于本产品取舍，已与官方机制区别表述。

- [Google：使用 noindex 阻止索引](https://developers.google.cn/search/docs/crawling-indexing/block-indexing?hl=zh-cn)
- [Google：robots meta、图片预览和 X-Robots-Tag](https://developers.google.cn/search/docs/crawling-indexing/robots-meta-tag?hl=zh-cn)
- [Google：合并重复网址与 canonical](https://developers.google.cn/search/docs/crawling-indexing/consolidate-duplicate-urls?hl=zh-cn)
- [Google：HTTP 状态及网络错误](https://developers.google.cn/search/docs/crawling-indexing/http-network-errors?hl=zh-cn)
- [Google：构建并提交 sitemap](https://developers.google.cn/search/docs/crawling-indexing/sitemaps/build-sitemap?hl=zh-cn)
- [Google：分页与增量加载](https://developers.google.cn/search/docs/specialty/ecommerce/pagination-and-incremental-page-loading?hl=zh-cn)
- [Google：露骨内容网站指南](https://developers.google.cn/search/docs/specialty/explicit/guidelines?hl=zh-cn)
- [Google：结构化数据通用政策](https://developers.google.cn/search/docs/appearance/structured-data/sd-policies?hl=zh-cn)
- [Google：Core Web Vitals](https://developers.google.cn/search/docs/appearance/core-web-vitals?hl=zh-cn)
- [Google：以用户为中心的实用内容](https://developers.google.cn/search/docs/fundamentals/creating-helpful-content?hl=zh-cn)
- [Google：图片 SEO 指南](https://developers.google.cn/search/docs/appearance/google-images?hl=zh-cn)
