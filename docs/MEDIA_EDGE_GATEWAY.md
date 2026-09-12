# 图片全入口控制：Cloudflare 边缘网关

更新：2026-09-11。Release A 已加入默认关闭的 Cloudflare 媒体网关代码和 Go API 鉴权策略端点；没有部署 Worker、绑定真实 R2、修改现有 bucket 公开设置或调用 Cloudflare。当前 Schema 仍为 v21，不新增数据库表、迁移或日本常驻服务。

## 目标与选择

目标是在保留图片缓存的同时，让新请求无法通过旧 HTML、已知图片 URL或边缘对象缓存绕过 `default_only`。浏览器已经下载到本机的文件和已打开页面无法远程撤回，不属于服务器可实现的保证。

比较了三种方案：

| 方案 | 优点 | 主要问题 | 结论 |
| --- | --- | --- | --- |
| 继续使用公开 R2/custom domain，只做 purge | 改动最小 | purge 后源站仍可读；已知 R2 URL绕过策略；动态切换需要大量清理 | 拒绝 |
| 日本新增 Go 图片代理 | 完全自控，API 技术栈统一 | 所有缓存 miss/回源占用日本 2C4G 带宽、连接和故障预算 | 暂不采用，作为付费 Worker 或边缘能力不足时的回退方向 |
| Cloudflare Worker + 私有 R2 binding | 策略检查发生在对象缓存之前；不暴露 R2 密钥/直连 URL；不增加日本常驻进程 | 每个图片请求仍消耗 Worker 请求；免费额度和真实延迟必须验收 | Release A 采用 |

这里的 `S3_PUBLIC_BUCKET` 表示“只保存可公开派生图的存储范围”，**不再表示 bucket 本身允许匿名读取**。启用网关前必须关闭 `r2.dev`、旧 R2 custom domain 或其他匿名对象入口；`S3_PUBLIC_BASE_URL` 改为 Worker route 的 `media` HTTPS origin。母版桶继续完全私有，也不会绑定到 Worker。

## 请求链

```text
浏览器 / 抓取工具 → https://media.example/.../media-public/...
  → Cloudflare Worker 严格校验 host、无 query、Release A key 结构、GET/HEAD
  → 先读取 1～30 秒短缓存的媒体策略（默认 5 秒）
      → GET https://api.example/edge/v1/media-delivery-policy
      → 独立 Bearer token；Go API 复用人工/动态 effective policy
  → normal：再读取 Workers Cache → private R2 binding
  → default_only / 策略超时或异常：跳过对象缓存/R2，返回内置 SVG 默认图，no-store
  → R2 缺对象/异常：返回内置 SVG 默认图，no-store
```

关键顺序是“**策略在对象缓存之前**”。对象可以保持一年 immutable 缓存，但每次 Worker 请求都先经过最多 5 秒的策略缓存；所以旧边缘对象不能在停图状态下直接返回。默认传播目标是 5 秒，不声称瞬时或零请求。策略缓存也会短暂缓存 fail-closed 结果，避免 API 故障时每张图都放大请求风暴。

## 合同与配置

日本 Go API：

```text
MEDIA_EDGE_POLICY_MODE=off|enforce
MEDIA_EDGE_POLICY_TOKEN=<32～4096字符、独立随机值>
```

默认 `off`；关闭或凭据不正确时精确路径表现为 404。`enforce` 时只开放无 query 的 `GET /edge/v1/media-delivery-policy`，正确 Bearer 才返回 `{ "mode": "normal|default_only" }`。所有 200/400/404/405 均 `no-store`；多 Authorization、前后空格、错误 token 拒绝。token 不能复用账号 HMAC 或 metrics 凭据。生产 Nginx 只代理这一条精确 `/edge/` 路径，其他 `/edge/` 和全部 `/internal/` 仍返回 404。

Cloudflare Worker 变量和 binding 见 `infra/cloudflare/media-gateway/wrangler.toml.example`：

```text
MEDIA_EDGE_GATEWAY_MODE=enforce
MEDIA_PUBLIC_ORIGIN=https://media.example.invalid
MEDIA_POLICY_URL=https://api.example.invalid/edge/v1/media-delivery-policy
MEDIA_POLICY_CACHE_TTL_SECONDS=5
MEDIA_BUCKET=<私有派生图 R2 binding>
MEDIA_EDGE_POLICY_TOKEN=<wrangler secret，不能写 toml/Git>
```

Worker 只接受当前管线产生的 `media-public/{site}/works|performers/{entity UUID}/{asset UUID}/vN/{purpose}-w320|640|960.webp`。拒绝 query、路径穿越、母版前缀、人物/作品 purpose 混用、其他尺寸、方法和 host；不会 fallback 到外部 URL。正常对象固定输出 `image/webp`、跨域图片许可、immutable 缓存和 `X-Media-Delivery-Mode: normal`。默认图、缺对象、策略/R2 异常不进入边缘缓存，并通过有界响应头标识原因，不输出 key、token 或底层错误。

## 下架、恢复与回滚

现有权利下架仍先删除并 HEAD 确认 R2 对象，再按公开 URL 调用 Cloudflare 单 URL purge，最后删除北京副本。启用 Worker 后必须在真实 Zone 验证此 purge 同时清除 Workers Cache API 项；未验证前不能把本地替身测试当作物理删除完成。

动态停图恢复仍要求管理员复核得到新鲜安全状态；策略缓存最长再保留配置 TTL。对象缓存没有被停图删除，恢复后可继续命中。HTML/OG/JSON-LD/RSC 仍按原 outbox 失效，二者互不替代。

建议切换顺序：

1. 保持 API/Next 人工 `default_only`，停止新上传，核对公开派生对象和北京副本；
2. 部署 API 的 edge policy 端点但先保持 Worker route 未接流量；
3. 以同一派生 bucket 建立私有 R2 binding，关闭 `r2.dev`/旧 custom domain，确认匿名 provider URL为 403/404；
4. 部署 Worker route，确认 default SVG、错误 fail-closed、无 R2 直连及 5 秒传播；
5. 切到 `normal`，验证真实图片、Workers Cache 命中、删除 purge、桌面/移动与抓取工具；
6. 最后再恢复上传。任何阶段失败都保持 `default_only`，优先 roll-forward；不要为了回滚重新开放匿名 R2 URL。

## 资源与未验收边界

- 网关不占日本常驻 CPU/内存，API 只承受各 Cloudflare PoP 每约 5 秒一次的策略请求，而不是每张图都查询 PostgreSQL；实际 PoP 数、Worker 请求额度、R2 Class B、延迟和费用仍须以真实账户验证。
- Worker 对象缓存避免正常请求持续读取 R2，但不能承诺免费额度不会超出。策略 TTL 越短，停图越快，API/Worker 请求越多；Release A 默认 5 秒，允许 1～30 秒。
- 当前代码只处理管线生成的 WebP 三档派生图。以后新增尺寸/格式必须先扩展白名单、默认图和测试，不能放宽为任意 bucket key。
- 本地合同使用内存 Cache/R2 和合成策略响应；没有证明 Cloudflare Cache API、R2 binding、purge、route、免费计划限制或跨区延迟。
- 两个专用桶的当前对象与未完成 multipart 部件现已进入上传前容量核算；仍需完成账户 10 GB/GB-month、供应商版本历史、其他 bucket 和共享账户消耗的可信计量。

本地可复跑：

```powershell
node --test scripts/tests/media_edge_gateway.test.mjs
go test ./services/platform-api/internal/config ./services/platform-api/internal/httpapi
pytest -q scripts/tests/test_release_a_env_check.py infra/reverse-proxy/tests/test_nginx_contract.py infra/security/tests/test_media_edge_gateway_contract.py
```

验证结果和未执行范围见[本轮证据](./evidence/release-a-media-edge-gateway-2026-09-11.md)。
