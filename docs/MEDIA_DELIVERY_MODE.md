# 图片主动降级：配置、范围与恢复

更新：2026-09-11。Release A 已实现手动配置驱动的主动停图，以及可选的用量状态动态执行链。它是保守的展示保护，不是供应商账单或存储计量器。当前整体 Schema v21；本轮复用 `platform.media_usage_state`，没有新增表或迁移。

已新增[一次性只读查询](./MEDIA_USAGE_OBSERVATION.md)及[持久观察/可选定时器](./MEDIA_USAGE_STATE.md)：Go Worker 可读取 Analytics 并保存高水位/复核建议，但不是账单/存储计量。观察器和动态展示均默认 off；只有同时明确启用 `observe` 与动态 `enforce`，API/Next 才消费该持久状态。缺状态、过期/未来时间、错误账期、`high/unknown`、复核锁存、停止建议或数据库/内部 API 错误均保守停图。低用量新样本不能自行清除复核锁存，恢复仍须管理员复核。

## 架构选择

新增的[手动跨区上传开关](./MEDIA_UPLOAD_CONTROL.md)可以在全部北京上传器升级后控制运行中的准入/Listing/PUT，并保留可信签名回执；它只影响上传，不修改图片源站。即使上传开关恢复 enabled，本文人工 `default_only` 仍优先拒绝上传和公开展示。现另有[逐操作上传准入/系统暂停](./MEDIA_UPLOAD_ADMISSION.md)，默认 off；它与动态展示读取同一份粘性用量状态，但分别控制北京新上传和日本页面展示，互不伪造彼此的执行结果。

本轮比较三种方式：只停上传无法减少浏览读取；API 去掉图片地址仍会被已有 HTML/浏览器预加载绕过；日本 Go 图片代理可以集中拦截，但会把缓存 miss、带宽和连接压到 2C4G。最终新增默认关闭的[Cloudflare 媒体网关](./MEDIA_EDGE_GATEWAY.md)：派生图 bucket 保持私有，Worker 在对象缓存/R2 binding 之前读取同一策略，不增加日本常驻服务。

当前代码已形成配套执行端：北京停新上传、日本 API 按请求去图、Next Proxy 用 CSP 限制旧目录 HTML，Cloudflare Worker 在对象缓存之前控制已知图片 URL并从私有 R2 binding 读取。任一策略依赖错误均 fail-closed。不是把完整目标缩小为一个前端开关；真实 bucket 私有化、Worker route/Cache API/purge、供应商账单和存储计量仍列为未验收。

## 配置与行为

```text
MEDIA_DELIVERY_MODE=normal
MEDIA_DELIVERY_MODE=default_only
MEDIA_DELIVERY_DYNAMIC_MODE=off
MEDIA_DELIVERY_DYNAMIC_MODE=enforce
```

每组是两个可选值，不是同时填写两行。`MEDIA_DELIVERY_MODE` 是最高优先级人工紧急开关，根级、北京和日本模板默认 `normal`，两地必须一致。`MEDIA_DELIVERY_DYNAMIC_MODE` 只注入日本 API 与公开站，默认 `off`；`enforce` 要求 `MEDIA_USAGE_MONITOR_MODE=observe`、明确账期和有效只读 Analytics 配置。生产 API/环境检查拒绝无效模式。Next 对动态模式的非空异常值保守启用并 fail-closed，但部署仍必须修正拼写，不能长期依赖异常值运行。

| 位置 | default_only 行为 |
| --- | --- |
| 北京 CLI / S3 上传准入 | `prepare` 在 SDK 创建、解码、Listing、副本写入前拒绝；S3 适配器也拒绝新的上传准入，避免直接调用绕过 CLI |
| 日本公开 API | 人工停图，或动态状态不安全/不可读时，首页各区、搜索、作品/人物/厂牌详情和列表中 `image_url/image` 为 null，`images` 为空数组；派生 URL 随图片对象一起不输出 |
| 登录用户目录 API | 收藏、关注、关注作品、不感兴趣和历史同样去图；标题、ID、链接、精确浏览时间不变 |
| 运营证据/数据库 | 不修改来源、对象 key/hash/URL、状态或权利证据；不把下架或删除状态伪造为停图 |
| Next 新渲染 | 人工模式仍在 Next DTO 层做无副作用去图；动态模式的新目录响应由 Go API 去图。首页请求时渲染，但目录 API 数据继续使用 300 秒标签缓存；Next Server Component 不增加 `no-store` 策略读取 |
| 旧 Next HTML | Next Proxy 每次文档请求读取私有 API 策略并增加完整 CSP，`img-src` 仅允许本站/data/blob；旧远程 img/srcset 不能发起网络请求，SafeImage 切本地默认图；策略接口异常同样停图 |
| Cloudflare 图片边缘 | 默认 off；启用后只允许管线生成的 WebP 派生 key，先读取最多 5 秒缓存的鉴权策略，再读取 Workers Cache/私有 R2。停图、策略/R2 错误或缺对象返回内置默认 SVG 且 no-store |
| 缓存/观测 | 停图响应带 `X-Media-Delivery-Mode: default_only` 和 no-store；Nginx 尊重此标志。API 私有指标导出有效停图、动态开关、边缘策略端点和策略可用性，避免数据库错误被误报为正常执行 |

正常模式仍保留原图片、srcset、目录缓存和 SEO。默认图为公开站自身的两张固定 SVG，不能指向被禁的远程域名。CSP 保留原脚本、框架、防嵌入、表单和 Turnstile 限制，不用单一 img-src 覆盖完整安全策略。Nginx 与 Next 同时返回 CSP 时策略取交集，不能删除 Next 的限制头。

本轮不增加运营网页切换按钮、数据库策略表或日本代理服务。Next 内部只读路径固定为 `/internal/v1/media-delivery-policy`，不进入公开 OpenAPI，生产 API Nginx 对公网 `/internal/` 返回 404。Cloudflare Worker 使用另一个默认 off、独立 Bearer 保护的精确 `/edge/v1/media-delivery-policy`；Nginx 只代理这一条，其他 `/edge/` 返回 404。首次打开/关闭动态或边缘功能仍需配套部署配置；功能处于 `enforce` 后，用量状态变化无需重启 Next/API，边缘请求最多在策略 TTL 后生效。已有在途上传不会因此被自动终止。

动态停图立即保证“浏览器不再请求远程图”，但不会重写已经由 Next 或公共边缘缓存的 HTML 字节。停图后应沿用缓存失效 outbox，让新 HTML/OG/JSON-LD/RSC 从已去图的 API DTO 重建；恢复时也应失效曾在停图状态生成的页面，否则本地默认图可能保留到原 TTL。首页已经改为请求时渲染以隔离构建期环境，但目录 API 数据缓存和公共入口的 5 分钟缓存仍保留；不能把运行时渲染误写成完全无缓存。

## 启用顺序

以下是后续部署手册，本轮没有启动容器或操作真实供应商：

1. 先暂停上传调度，部署[边缘网关](./MEDIA_EDGE_GATEWAY.md)但保持人工 `default_only`；将派生 bucket 改为私有 R2 binding，关闭 `r2.dev`/旧 custom domain 并确认匿名 provider URL为 403/404。仓库已有 Worker 和策略端点，但真实供应商切换尚未执行，不能靠配置文件存在假装完成。
2. 紧急人工停图时，停止旧上传器或等待本批确认；按[上传恢复手册](./MEDIA_UPLOAD_CAPACITY.md)保留 pending。两地配置改为 `default_only`，执行 full 环境核对，再替换/重启北京媒体、日本 API、公开站与新版 edge。计划使用动态执行时，先验证 observe 状态、明确账期和管理员复核流程，再只在日本 API/公开站设置 `MEDIA_DELIVERY_DYNAMIC_MODE=enforce`。权利下架服务保持可用。
3. 在日本使用既有受控 outbox 机制请求全目录失效，等待真实 Worker → Next 成功回执；不得手动把任务标为 completed。单实例之外须逐实例核验，不把一次回执当全局保证。
4. 清理 Cloudflare 已缓存的目录 HTML，并确保停图期间不使用忽略源站 Cache-Control 的强制缓存规则。已有 HTML 仍含远程 URL 时，Next CSP 和边缘网关都会阻断真实对象；新一轮失效后的 HTML/OG/JSON-LD 才去掉旧字段。
5. 核对有效 mode、dynamic/edge enabled/available 指标、API edge 鉴权、默认图 HTTP、Worker Cache/R2 binding，以及桌面/手机/直接图片 URL在 5 秒目标内停止。数据库、策略或 R2 不可用演练必须得到默认图，不能把 fail-closed 当正常依赖可用。文字详情/搜索、账号、管理员和下架仍可用；保留验证证据和操作日志。

可沿用现有、需要受控数据库权限的刷新任务形式（不是修改业务资料）：

```sql
INSERT INTO platform.outbox_events (aggregate_type, aggregate_id, event_type, payload, dedupe_key)
VALUES ('site', gen_random_uuid(), 'cache_purge',
        '{"reason":"media_delivery_mode_transition"}'::jsonb,
        'media-mode:<unique-approved-change-id>')
ON CONFLICT (event_type, dedupe_key) DO NOTHING;
```

每次切换使用新的已批准变更 ID，不能复用已完成事件让后续切换没有实际刷新。该 SQL 只供之后受控执行，本轮未执行数据库或 Cloudflare 操作。

## 恢复

先确认真实用量、待处理批次和图片权利/发布状态允许恢复。人工模式需协调两地切回 `normal` 并重启对应进程；动态模式必须由管理员复核清除锁存，普通低用量新样本不会自行恢复。两种方式都要等待 Next/Cloudflare HTML 失效并验证正常图片恢复；确认公开域名读取策略、默认图和正常图片都正常后，再恢复上传入口。勿通过清除 pending 或改写资产状态来恢复展示。

升级需要 API/公开站/北京媒体与 edge 配置配套；回退旧公开站或旧 edge 会失去请求级停图/缓存保护，期间应保持图片源站入口关闭。没有数据库迁移或底层图片状态回滚。

## 明确不能保证的内容

- A/B 账户估计、预留建议、上传/定时对账准入及 API/页面动态执行已开发；仍无账单硬上限、GB-month 计量，动态停图也不是“供应商已确认超额”信号。
- 单独使用 CSP 不是远程存储鉴权；只有在派生 bucket 私有、匿名 provider 入口关闭且 Cloudflare Worker route 已真实启用后，已知对象 URL、抓取工具和旧边缘对象才会经过策略。已经下载到浏览器或已打开标签页中的字节仍无法远程撤回。
- HTML/OG 的旧缓存需按上述步骤失效，不能保证所有爬虫立刻丢弃旧图。
- 删除、权利下架、HEAD/对账仍允许，仍产生供应商请求；自动额度保护还需为这些必要请求预留预算。
- 当前运行态证据是本机 Next 生产构建、浏览器、Node 内存 Cache/R2 和合成数据；不是目标 Nginx/Cloudflare Worker、真实 Go+PostgreSQL 或 R2 binding 验收。

完整目标仍要求实际供应商计量/统计延迟策略、私有 R2/Worker route/purge 的停止与恢复、目标资源预算和整个业务链联调。**Release A/M5 未整体验收，公网 no-go。**

手动模式历史验证见[静态停图证据](./evidence/release-a-media-delivery-2026-09-11.md)；动态状态链、fail-closed 与不重启 Next 的浏览器验证见[本轮证据](./evidence/release-a-media-delivery-dynamic-2026-09-11.md)。
