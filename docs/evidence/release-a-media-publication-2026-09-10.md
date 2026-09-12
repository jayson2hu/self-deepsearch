# Release A 图片登记与父资料公开隔离

日期：2026-09-10（Asia/Shanghai）。本轮仅本地开发与测试；没有启动 Docker、PostgreSQL、SMTP、S3/R2、Cloudflare、自动采集或广告，也未提交、推送或触发远程 CI。

## 结果

1. 新图片登记在父资料行锁内拒绝 `takedown`、`merged` 和未知状态；保留草稿、审核中、已批准、已发布、普通隐藏资料的提前准备能力。
2. 图片 manifest、对象、派生关系、关联、审计与 `cache_purge` 同事务。图片新增不需要修改文字 revision 也能触发缓存刷新；outbox 写入失败必须回滚并返回失败。
3. 新迁移 17 收紧 `platform.public_entity_media`：复用作品/人物主站公开发布视图，不再只检查图片自身状态；保留列、角色授权和原媒体过滤条件，增加 security barrier。API readiness、种子、监控、CI、受限 SQL 测试库门槛同步为 Schema v17；PostgreSQL 引擎仍为 16。迁移没有在真实库执行。

接口未改变 JSON 字段；OpenAPI 补充 201 的异步刷新含义、409 状态边界和 503 的不确定提交说明。新登记不是主图替换接口，不会覆盖既有不可变对象。

## 验证证据

登记问题先用事务替身复现：旧流程没有 status 检查与 outbox，已下架目标仍被写入；注入 outbox 失败也无法阻止旧流程提交。修复后状态守卫、link → outbox → audit → commit 顺序及失败回滚通过。HTTP 合同确认冲突/缺父资料/数据库错误分别返回 409/404/503，不泄漏内部诊断或返回成功 manifest，写响应保持 no-store。

| 检查 | 本轮结果 | 边界 |
| --- | --- | --- |
| Go API/Worker vet/test、API 编译 | 通过 | 使用本地依赖缓存，含实际 Go→Python HTTP 合同；API 二进制仅编译未启动 |
| 媒体登记专项 | 事务替身和 HTTP 通过 | PostgreSQL 登记两组明确 SKIP |
| Go→Next 实际缓存合同 | 9/9 通过 | 真实 Go 签名处理器、生产 Next 单实例、合成目录；不含 PostgreSQL/outbox 调度或 Cloudflare |
| Python 全量回归 | 180 项 + 88 个 subtests 通过，83.85 秒 | 离线测试/静态 SQL 合同，不是执行迁移 |
| TypeScript、ESLint、OpenAPI 漂移、相关 ruff | 通过 | 未重新执行 npm registry 审计 |
| JS 工具合同/前端单元烟测/standalone HTTP | 56/56、13/13、通过 | 已有 production 产物；本轮未重新 build 或运行 Playwright |
| 新 SQL 状态矩阵 | 已编写并纳入 roundtrip，未执行 | 作品/人物各 11 种组合、隐藏及重新发布，使用 public_reader 读取，不增加底表授权 |

缓存的机器可读记录见 [9 场景 JSON](./release-a-media-cache-contract-2026-09-10.json)。新增测试先只修改图片字段，不改标题或 sitemap 时间：未发事件时页面仍无新图且不回源，收到 `cache_purge` 后首页/详情都出现实际 `img` 标签并回源，检查在原 300 秒 TTL 到期前完成。隐藏后同时检查标题、链接和图片地址消失。测试通过独立 standalone 副本隔离缓存，结束后停止自有进程并清理自有临时目录，HTTP 请求不会下载合成 `.test` 图片。

## 重跑

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-api/internal/database ./services/platform-api/internal/httpapi -run TestMedia -count=1 -v
node scripts/release_a_cache_contract.mjs --output docs/evidence/release-a-media-cache-contract-local.json
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
npm run check:api-types
npm run typecheck
npm run lint
npm run test:preflight
npm run test:frontend:smoke
npm run smoke:frontend:release-a
```

`PLATFORM_API_TEST_DATABASE_URL` 未配置时，SQL 合同必须 SKIP；不要把整体退出码 0 当作数据库通过。准备一次性 PostgreSQL 16 / Schema v17 测试库后，按[运行手册](../RELEASE_A_RUNBOOK.md)执行受限登录合同与 roundtrip；本轮没有请求或使用真实数据库凭据。

## 未完成与升级影响

- M5 仍缺主图替换、旧母版私有保留 30 天/到期清理；重复下架对已撤下的公开对象可能再次生成 `public_url=null` 的删除任务，需保留原 URL 并统一逐对象幂等身份。本轮没有修改该删除逻辑。
- 已上传公开桶对象即使未入目录，也可能经已知 URL 访问。v17 只修数据库公开边界，不能宣称它提供对象存储保密；CLI 仅处理事先已确认允许公开的图片。
- 已有 v16 环境在维护窗口执行新迁移并更换所有 API，刷新 Next 与边缘缓存后再恢复入口。v17 Down 会恢复旧视图条件，只供一次性回滚合同；公网回滚优先 roll-forward。
- SQL 锁与事务、对象存储、边缘缓存、主图完整生命周期和目标资源占用仍待验证。此前“无已知核心代码缺口”的文档判断已修正；本轮不把 Release A 或图片模块标为完成，公网仍 no-go。
