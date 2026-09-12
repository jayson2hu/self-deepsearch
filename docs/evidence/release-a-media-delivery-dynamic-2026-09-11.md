# Release A API/Next 动态默认图验证

日期：2026-09-11（Asia/Shanghai）

## 结论

Release A 已完成默认关闭的动态展示执行链：日本 Go API 按请求读取现有粘性用量状态并对公开/用户目录 DTO 去图；Next Proxy 对每个文档请求读取私有策略，用完整 CSP 立即阻断旧 ISR HTML 的远程图片请求。人工 `MEDIA_DELIVERY_MODE=default_only` 仍是最高优先级。没有新增数据库表、迁移、常驻服务或公开 API。

本轮代码与合成浏览器验证可继续进入隔离测试环境，结论为 **conditional-go**。它不是供应商账单硬上限，也不能阻止直接访问已知图片 URL、旧已打开标签页、Cloudflare 已缓存对象或图片源站读取；Release A/M5 和公网仍为 **no-go**。

## 实现边界

- `MEDIA_DELIVERY_DYNAMIC_MODE=off|enforce` 只进入日本 API/公开站；环境检查要求 enforce 配套 `MEDIA_USAGE_MONITOR_MODE=observe`。北京 Compose 不注入该变量。
- API 只读 `platform.media_usage_state`。只有当前账期内、最后成功和统计窗口均不在未来且 30 分钟内新鲜、无 `review_required/stop_recommended`、最后状态为 `low_estimate|warning` 才返回 normal。
- 状态缺失、过期、未来时间、错误账期、`high/unknown`、数据库错误、仓储缺失或截止超时均返回 default_only。低用量新观察不会自行清除既有复核/停止锁存。
- API 对首页、列表、搜索、作品/人物/厂牌详情和登录用户收藏/关注/历史去图；不修改反馈证据、运营 manifest、对象 URL 证据、数据库或权利删除流程。
- 私有策略路径固定为 `/internal/v1/media-delivery-policy`，只接受无 query 的 GET，固定小 JSON、`Content-Length`、`Cache-Control: no-store` 与 `X-Media-Delivery-Mode`。它不在 OpenAPI，生产 API Nginx 对公网 `/internal/` 返回 404。
- Next 严格校验状态、Content-Type、唯一字段、模式响应头和 no-store；1 秒内任一错误 fail-closed。Proxy 每次文档请求执行，因此旧 ISR HTML 不需重启即可停止远程图。
- 私有指标区分有效 `default_only`、动态执行已启用和策略输入可用；新增 `PlatformMediaDeliveryPolicyUnavailable` 告警，避免数据库故障被误当作健康停图。

## ISR 取舍与失败修正

首版尝试在 Next Server Component 每次渲染执行 `no-store` 内部策略查询。生产构建本身通过，但在“构建时 off、运行时 enforce”的真实 standalone 场景中，Next 返回 `Page changed from static to dynamic at runtime` 和 HTTP 500。该失败没有被记为通过。

最终保留首页 5 分钟 ISR/SEO 缓存：Go API 负责新目录 DTO 去图，Next Proxy 负责旧 HTML 的请求级 CSP。动态切换后浏览器立即停止远程图片网络请求；已经生成的 HTML/OG/JSON-LD/RSC 字节不会被 Proxy 改写，必须沿用受控 outbox 和 Cloudflare 失效后重建。恢复时也需要失效停图期间生成的正文，否则默认图可保留到原 TTL。强制所有页面动态渲染会损失当前以搜索流量为核心的缓存能力，因此 Release A 不采用。

## 验证结果

```text
Go API/Worker: go vet + go test + go build 全量通过
Python: 273 passed, 275 subtests passed, 1 skipped in 115.52s
Ruff: 全范围通过
OpenAPI TypeScript: current，无漂移
前端单元: 23 passed
display-web: lint、typecheck、Next production build 通过
ops-web: lint、typecheck 通过
媒体 Playwright: 4 passed（手动/动态 × desktop/mobile）in 2.0m
```

浏览器动态场景使用同一个 Next standalone 进程和同一份已预热 ISR/data cache：先确认 normal 会请求合成远程图，再只修改 mock 私有策略为 default_only；下一次旧 HTML 响应带 no-store、严格 CSP 与 default_only，页面显示本地默认图，远程请求数组为空；随后改回 normal，不重启 Next 即恢复远程图片。手动模式另验证重启切换、缓存失效后的 HTML/OG/JSON-LD/RSC 去图、登录页和账号入口继续可用。

Go HTTP 覆盖人工优先、动态 normal/default/error/缺仓储、目录响应不变字段、内部端点 method/query/headers/body，以及 metrics enabled/available。operations 单元覆盖账期、新鲜度、未来时间、缺时间、review/stop、high/unknown；Node 单元覆盖严格协议、错误 fail-closed、人工短路和 CSP 保留 Turnstile/frame/form 防护。

收尾复验统一为私有策略端点的 200、400、405 响应都带 `Cache-Control: no-store`，防止方法或查询参数错误被中间层缓存。重新执行受影响 Go API/operations 测试与 API 全量 `go vet` 均通过；随后再次执行全范围 Ruff、两站 lint/typecheck、OpenAPI TypeScript 漂移检查，并检查 52 份 Markdown 的本地文件链接，结果全部通过。本次收尾未修改前端运行逻辑，因此没有重复执行约两分钟的媒体浏览器套件。

## 未执行与剩余风险

- 未启动 Docker、PostgreSQL、SMTP、S3/R2、Cloudflare 或 Analytics，未连接日本/北京服务器，未部署或触发远程 CI。
- 真实 `platform_api` 只读权限和查询延迟仍须在 PostgreSQL 16 验收；本轮数据库合同的既有真实 SQL 场景保持 skip，不视为通过。
- 目标 Nginx/Cloudflare 必须验证公网内部路径 404、重复 CSP 取交集、default_only 不被边缘 TTL 覆盖、目录 purge 和恢复传播时间。
- CSP 不是对象存储鉴权。下一步仍需图片域名/源站/直连/已有缓存的全入口停止与恢复设计，以及 10 GB/GB-month、版本、分段和账户共享额度的真实计费口径。
