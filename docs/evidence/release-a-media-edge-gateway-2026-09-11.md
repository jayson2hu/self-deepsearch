# Release A Cloudflare 私有 R2 媒体网关验证

日期：2026-09-11（Asia/Shanghai）

## 结论

Release A 已完成默认关闭的 Cloudflare Worker 媒体网关和日本 Go API edge policy 端点。网关只读取私有 R2 binding，在读取 Workers Cache/R2 之前先检查人工/动态媒体策略；`default_only`、策略异常、缺对象或 R2 异常均返回内置 SVG 默认图且不缓存。生产 Nginx 只代理精确的鉴权策略路径，其他 `/edge/` 与 `/internal/` 路径返回 404。

本轮离线代码、合同和质量门槛全部通过，可进入隔离测试环境，结论为 **conditional-go**。没有部署 Worker、绑定真实 R2、关闭现有匿名入口、执行 purge、启动 Docker或连接真实 PostgreSQL/SMTP/S3/R2/Cloudflare/Analytics。Release A/M5 与公网仍为 **no-go**。

## 关键实现边界

- `S3_PUBLIC_BUCKET` 只表示可公开派生图范围，不表示 bucket 可匿名读取；启用前必须关闭 `r2.dev`、旧 custom domain 和其他匿名对象入口。
- Worker 只接受当前管线生成的 `media-public/{site}/works|performers/.../vN/...-w320|640|960.webp`，拒绝 query、路径穿越、母版、错误 purpose、未知尺寸、错误 host 和非 GET/HEAD。
- 策略检查位于对象缓存之前。正常对象可保持 immutable 缓存；策略最多短缓存 5 秒，旧边缘对象不能绕过新的 `default_only`。
- 策略协议为独立 Bearer、固定小 JSON、严格响应头和 fail-closed。错误 token、重复 Authorization、query 和非 GET 均拒绝；所有 API 结果 `no-store`。
- edge token 只注入日本 `platform-api` 和 Cloudflare Worker secret，不进入公开站、运营后台、北京服务或仓库示例值。
- 该网关不撤回浏览器已经下载的字节，也不证明 Cloudflare 免费额度、Cache API、R2 Class B、跨区延迟或单 URL purge 的真实行为。

## 验证结果

```text
Cloudflare 网关专项 Node: 8 passed in 0.47s
环境/Nginx/安全专项 Python: 37 passed, 74 subtests passed in 3.14s
前端/工具单元总门槛: 31 passed in 6.50s
Python 全量: 280 passed, 281 subtests passed, 1 skipped in 115.62s
Ruff: 全范围通过
Go API: go test、go vet、go build 全量通过
Go Worker: go test、go vet、go build 全量通过
前端: display/ops lint、typecheck 通过
OpenAPI TypeScript: current，无漂移
```

全量 pytest 的唯一 skip 是当前 Windows 账号不能创建 symlink 的上传防护场景；单独复验为 5 passed、1 skipped、2 subtests，明确给出 `this account cannot create symlinks`，不是断言失败。真实 PostgreSQL 合同由 Go 测试以显式环境开关控制，本轮没有执行，不能算目标数据库验收通过。全量运行还报告 `.pytest_cache` 无法写入的本机缓存 warning，不影响测试执行或断言；专项复验使用 `-p no:cacheprovider` 后无 warning。Go 首次尝试访问用户级构建缓存被当前 Windows 沙箱拒绝，随后改用仓库内临时 `GOCACHE`，API/Worker 的 test、vet、build 均通过；这不是代码失败。

## 本轮命令

```powershell
node --test scripts/tests/media_edge_gateway.test.mjs
.\.venv\Scripts\python.exe -m pytest -q -p no:cacheprovider scripts\tests\test_release_a_env_check.py infra\reverse-proxy\tests\test_nginx_contract.py infra\security\tests\test_media_edge_gateway_contract.py
.\.venv\Scripts\python.exe -m pytest -q
.\.venv\Scripts\ruff.exe check .
npm run test:frontend:smoke
npm run lint
npm run typecheck
npm run check:api-types
go test ./...
go vet ./...
go build ./...
```

Go 命令分别在 `services/platform-api` 与 `services/platform-worker` 执行，并将 `GOCACHE` 指向仓库内临时目录。

## 真实环境仍需验收

1. 将派生图 bucket 设为私有，确认 `r2.dev`、旧 custom domain 和 provider URL 匿名访问为 403/404；母版桶不得绑定到 Worker。
2. 部署 Worker route、私有 R2 binding 和 secret，确认公网只经受控 `media` origin 访问。
3. 验证 normal、人工/动态 `default_only`、策略超时/错误、R2 缺对象/错误，以及策略变化在 5 秒目标内传播。
4. 验证 Workers Cache 命中、下架删除后的单 URL purge 确实清除 Cache API 对象，并用 HEAD 确认 R2 物理状态。
5. 验证 `self_deepsearch_media_edge_policy_enabled = 1`、动态策略可用性指标、Prometheus scrape 与目标告警；回滚时保持默认图，不重新开放匿名 R2。
6. 完成 10 GB/GB-month、版本、multipart、其他 bucket、共享账户、Worker 请求和 R2 操作量的可信计量与停止规则。

在这些真实门槛完成前，不承诺源站已经私有化、停图传播一定达到 5 秒、删除 purge 已生效、免费额度不会超出或公网可以开放。
