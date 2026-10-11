# 项目梳理与第一轮重构

日期：2026-10-11（Asia/Shanghai）。基线：`3cb9471`，开始时工作区干净，`master` 比本地记录的 `origin/master` 超前 73 个提交。这些已有提交未重置、改写或推送。

本轮是工程重构与回归修复，不是产品重新设计，不代表 RD/OPT 需求已由用户验收。数据库维持 Schema 28，没有迁移、权限扩张或审核环境数据清理。

## 1. 当前架构

| 目录 | 职责与边界 |
| --- | --- |
| `apps/display-web` | Next 公开目录、搜索、详情、账号、SEO 与同源 API 入口 |
| `apps/ops-web` | Next 运营后台：录入、版本、审核发布、媒体、用户、采集暂存、SEO 与流量 |
| `packages/api-contracts` | OpenAPI 与生成的 TypeScript 类型；不承载运行时代理逻辑 |
| `packages/web-runtime` | 本轮新增：两站共用、无 Next 运行时依赖的请求代理 |
| `services/platform-api` | Go 模块化单体；身份、目录、运营事务、受限采集接收、统计及审计 |
| `services/platform-worker` | Go 异步任务、outbox、缓存失效及受开关控制的媒体维护 |
| `workers/collector-python`、`workers/media-python` | 采集合约与受控媒体处理；不能把代码存在等同于真实采集已获授权 |
| `db`、`infra`、`scripts` | PostgreSQL 16 / 28 个迁移、部署设施、验收与运行工具 |

关键依赖：两站 → 同源代理 → Go API → PostgreSQL；业务事务写 outbox → Worker → 缓存/媒体任务。保留现有单库和服务边界，没有引入新的消息队列、缓存服务或微服务拆分。

## 2. 已落地的优化

### 共享代理与资源控制

- 两份完全相同的代理合并到 [web-runtime](../packages/web-runtime/src/platform-proxy.ts)，应用层仅保留环境配置适配。同步更新 npm workspace、锁文件、Next 转译配置和两站 Dockerfile 的依赖/源码复制。
- 原实现先 `arrayBuffer()` 读完整请求，再检查 2 MiB。现在逐块检查实际字节数，超过限制立即取消读取，不再依赖客户端声明的 Content-Length；缓冲区按需增长且容量不超过配置上限。不声称能限制传输层在交付单个 chunk 前已分配的内存。
- 默认 15 秒预算覆盖请求体读取与上游请求；客户端取消向上游传播。超限、异常流、超时均返回受控且不可缓存的错误。
- 保留请求/响应头白名单、多 Set-Cookie、手动重定向、响应流与 `no-store`。SEO 报表导入的 `5 MiB + 128 KiB` 特例不变。拒绝会被 URL 规范化的 `.` / `..` 路径段。
- 新增 10 项行为测试：路径/头/cookie/响应流、UTF-8 字节边界、缺失或虚报长度、超限立即停止、默认/上传特例、坏流、增量缓冲区扩容、慢流/取消、上游失败及路径越界。取消处理仅注册一个监听器，不随分块数量积累等待回调。

### 后台按业务职责拆分

| 原文件 | 改造后 |
| --- | --- |
| `database/operations_store.go`：2,081 行 | 12 个职责文件，最大 293 行；原文件只保留 84 行共享底层辅助函数 |
| `httpapi/operations_handlers.go`：1,900 行 | 12 个职责文件，最大 320 行；原文件只保留 80 行共享鉴权、错误和格式辅助函数 |

业务分组包括 review、conflict、catalog、revision、publication、users、system、takedown；数据层另外区分 import、provenance、media，请求层另外区分 ads、invitations、validation。文件命名为 `operations_<业务>_store.go` / `operations_<业务>_handlers.go`。

搬移前后核对了数据层 58 个和请求层 116 个顶层声明，声明内容逐字一致；仅调整文件归属和必要 import。没有更改 SQL、事务边界、路由、响应字段或角色规则。既有静态合同更新为读取对应职责文件。

### 验收入口与测试可靠性

- 新增 `npm run test:unit`，递归、排序发现 `scripts/tests/**/*.test.mjs`。CI 与统一 preflight 均使用该入口；保留原专项命令供快速定位。
- 原统一入口仅列举 17 个文件，而当时实际已有 35 个；现在自动覆盖 37 个文件。新增文件无需手工修改 CI，空测试集和子进程失败都不能被误报为成功。
- 修复 Python 收集 `test_search_variants.py` 时执行真实查询并覆盖历史证据的问题：仅显式运行脚本时执行，另加无数据库调用/无文件写入的导入回归测试。误写的历史报告已恢复。
- Schema 静态合同改为从迁移序列读取当前版本；API/OpenAPI 合同比较完整集合、路径参数和 operationId 唯一性，并解析共享常量形式注册的采集路由，不再写死路由数量。
- SEO 测试同步到现有游标分批机制；搜索/首页冒烟同步到当前页面文案，保留发布边界、故障状态和安全头检查。
- SEO SQL 验收增加专用库/受限角色守卫及唯一测试标识；不再尝试用业务账号删除报告或审计记录，不再假定目标问题一定在列表前 100 条。流量验收用单独受严格地址约束的管理员连接清理合成数据，真实查询仍用受限 API 账号。
- 整理了 7 个既有 Python 工具/测试的导入、未使用导入和复合语句，消除本轮发现的 30 个 Ruff 问题；未执行这些清理类工具的业务操作。
- 运营采集列表复用 `Intl.NumberFormat`，与时间格式器明确分离，保持原显示结果。

## 3. 验证记录

以下均为本轮本机执行，不代表远端 Actions 或公网验收：

| 检查 | 结果与边界 |
| --- | --- |
| Node 单元/合同 | 37 个文件、225 项通过，0 失败 / 0 跳过 |
| Python 全量 | 341 项测试、374 个子测试通过，0 失败 |
| Go | API/Worker `go test -race` 与 `go vet` 通过；需要数据库的测试另行启用 |
| TypeScript / ESLint | 两站、共享类型/运行时 package 的相应检查通过 |
| Python 静态 | Ruff 通过；mypy 20 个源文件通过；OpenAPI 生成类型无漂移 |
| 生产构建与 HTTP | 两站 production build 通过；12 组隔离 standalone HTTP 冒烟通过，包含代理 503/no-store |
| PostgreSQL 真实契约 | 独立 PostgreSQL 16、Schema 28、合成数据。分组累计覆盖 API 仓储套件全部 83 个顶层测试：基础 73 个；SEO/搜索/流量/UV 6 个；采集事务及固定归档 HTTP 4 个 |
| SQL 重复性 | SEO 问题流程、报表导入及流量 3 项连续执行两遍通过 |
| SEO 事务 | 2,478 个目标分 3 批；并发互斥、每日幂等、第二批注入失败后的全事务回滚及重试通过 |
| 采集归档 | 使用项目既有独立验收脚本；只读本地固定归档，9 批 / 4,196 条记录，包含重放与不直接发布的边界检查 |

复跑入口：

```sh
npm run test:unit
npm run typecheck
npm run lint
npm run build
npm run smoke:frontend:release-a
PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv/bin/python -m pytest -q
go test -race ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
```

SQL 测试必须使用各测试要求的临时库名、受限账号和显式环境变量；不能把这些命令指向审核库或生产库。固定归档测试使用 `scripts/run_collector_ingest_contracts.py --report <新的报告文件>`。本轮原始日志在 `/tmp/self-deepsearch-refactor-*`，未把连接字符串、凭据或原始数据纳入交付文档。

## 4. 仍建议分期推进的事项

1. **端到端与部署复核**：本轮没有更新在线审核服务；新 Docker 镜像构建/运行、真实浏览器全流程、最终 HTTPS 与公网策略仍需发布阶段复核。不能以本地编译和 HTTP 通过替代这些门槛。
2. **缩小静态字符串断言的比例**：保留它们作为安全兜底，但进一步用 Go 实际路由枚举、受限数据库事务和浏览器行为测试降低文案/文件名变更引起的误报。
3. **整理其余大模块**：`database/store.go` 及 `release_a_frontend_smoke.mjs` 仍较大，后续可分别按公开查询域、测试场景拆分。避免与本轮共享代理变更同时大幅调整业务查询。
4. **以测量驱动 SQL/页面性能优化**：需要代表性数据规模下的查询计划、延迟分位数及浏览器性能基线。本轮不宣称延迟提升百分比，也没有盲目新增索引。
5. **产品与数据链路**：采集候选到正式人物映射、权利/成年审核、头像入库和正式发布闭环，及历史策略 Q-01，仍以[重设计台账](./redesign/PROGRESS.md)和[前端优化计划](./FRONTEND_OPTIMIZATION_PLAN.md)为准，不因工程重构自动变为已验收。

## 5. 交付状态

本轮重构按用户授权作为独立本地 Git 提交交付，不执行 push 或重部署。已有 73 个本地提交保持原样。现有审核容器未重启，现有审核数据未修改。本轮两个临时 PostgreSQL 容器及合成测试库已清理，不可直接恢复，但可用迁移、种子和验收脚本重建；开发依赖、构建缓存和本轮原始测试日志保留。
