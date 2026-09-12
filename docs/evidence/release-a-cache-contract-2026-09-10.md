# Release A Go→Next 缓存运行态验证

日期：2026-09-10（Asia/Shanghai）。机器报告：[本机 JSON](./release-a-cache-contract-2026-09-10.json)。

## 本轮完成

新增 `scripts/release_a_cache_contract.mjs` 和环境门控的 Go 合同，使用实际 `RevalidationProcessor` 的签名/HTTP/完成回执逻辑调用当前 Next.js production build。不是手工拼一个成功回执：断言直接读取首页、作品详情 HTML 和 sitemap，并检查合成上游的真实请求次数。

| 场景 | 实际结果 |
| --- | --- |
| 初始化 | 有效 Go 请求清除构建期首页缓存，三类页面读取 before 版本；各回源 1 次 |
| 未请求失效 | 上游改成 after，页面仍为 before；各回源计数仍为 1 |
| 错误签名 | 真实接收器 HTTP 401，Go 返回 revalidate_rejected；页面和回源计数不变 |
| 过期签名 | 真实接收器 HTTP 401；页面和回源计数不变 |
| 有效发布 | 首页/详情更新为 after，sitemap 更新时间同步变化；三类回源计数均增至 2 |
| 隐藏 | 首页移除标题和详情链接，sitemap 移除 URL，详情实际 HTTP 404，正文不再出现 before/after |
| 重复事件 | 同一隐藏事件重放成功，资料仍不可见，没有被恢复 |

发布和隐藏阶段必须在 240 秒安全窗口内完成，比 300 秒正常 TTL 短，防止自然过期掩盖失效缺陷。Go 合同默认在普通单测中跳过；运行器显式提供临时配置并检查实际 PASS，任何 SKIP 都视为失败。

## 修复测试间缓存污染

最初直接在共享 standalone 目录执行，缓存合同本身通过，但紧随其后的 HTTP 冒烟报“公开首页缺少默认图”：已隐藏的空首页被写入 `.next/server`，影响了另一个 Next 实例。这是新测试的隔离缺陷，不是资料发布逻辑的修复。

现每轮先复制一份完整可写 standalone 及静态资源到 `.cache/cache-contract-*`，禁止用硬链接/符号链接共享可变缓存；启动的是真实生产 server。结束后只清理验证过路径的自有目录。新增文件级回归验证副本更新不会修改源缓存、拒绝清理源目录。公开站已重新 build，随后连续执行：

1. 原 standalone HTTP 冒烟：通过。
2. 隔离缓存合同：7/7 通过。
3. 原首页缓存 SHA-256：与合同前一致。
4. 原 standalone HTTP 冒烟：再次通过。
5. `.cache/cache-contract-*`：无残留测试目录。

## 本机回归

| 检查 | 结果 |
| --- | --- |
| Go API/Worker vet 与非缓存 test | 全部包通过；显式启用实际 Go→Python HTTP 合同 |
| 全套 Python | 169 passed, 73 subtests passed；77.53 秒 |
| 发布工具合同 | 54 passed；包含缓存断言、隔离副本、输出边界与预检接线 |
| 前端单元烟测 | 13 passed |
| 公开站 production build | Next 16.3.3 编译、TypeScript 与静态页面生成通过 |
| standalone HTTP 冒烟 | 隔离缓存合同前后均通过 |
| ruff | 全仓既有 Python 检查范围通过 |
| CI 安全合同 | 9 passed，包含 build→缓存合同→证据上传→镜像交付依赖 |

本轮没有改动页面组件、数据库 Schema 或生产刷新协议。新增缓存合同进入默认统一预检与 CI web 的必过步骤，CI 保存脱敏 JSON 14 天；远程 CI 尚未触发或执行。

## 复验

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
npm run test:preflight
npm run test:frontend:smoke
npm run smoke:frontend:release-a
npm run test:cache:release-a -- --output docs/evidence/release-a-cache-contract-local.json
npm run smoke:frontend:release-a
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
```

需要当前两站 standalone 产物；缺少时先 `npm run build`。报告只保留场景、耗时、HTTP 状态和回源计数，不保存响应正文、临时 HMAC、事件/作品标识或真实用户信息。

## 未覆盖边界

目录 API 是内存合成替身；没有启动 Docker、真实 PostgreSQL/SMTP/S3/Cloudflare，没有执行数据库 outbox 的 Claim、租约、重试状态事务，也没有运行真实 Go 业务 API、Nginx/Cloudflare 页面缓存、多实例传播或目标域名验收。Go race、真实核心服务 CI 及完整部署放行仍待后续；Release A 整体目标保持未完成。
