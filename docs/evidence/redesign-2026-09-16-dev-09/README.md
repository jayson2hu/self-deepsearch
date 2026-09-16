# RD v1 / DEV-09 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，OpenAPI/HTTP/真实 SQL/生产构建通过，真实浏览器与生产规模计划待验收。基线提交：`9393aad`。

## 实际交付

- 新增 `GET /api/v1/search/performers?q=&cursor=` 与 `GET /api/v1/search/studios?q=&cursor=`；旧 `GET /api/v1/search/works` 的路径、响应和默认前端行为保持兼容。
- 人物查询覆盖规范显示名、原文名、罗马字名和别名。排序为规范名精确 → 别名/原文/罗马字精确 → 前缀 → 其他文本，再以小写显示名和 UUID 稳定打破并列。
- 厂牌查询覆盖公开名称和归一化名称。排序为精确 → 前缀 → 其他文本，再以归一化名称和 UUID 稳定打破并列；两个同名厂牌保持两个实体。
- 两个仓储查询都同时连接 `public_published_*` 和 `public_search_documents`，因此 entity/publication 任一不是 published 都不会成为候选。人物还沿用 `adult_status = verified` 的公开视图边界。
- 新游标为版本 1，包含实体/查询绑定 scope、排名、稳定名称和 UUID 排序元组；最大 1024 字符。服务端拒绝坏游标、跨查询和跨类型游标，不从数据库重新查锚点，因此末项退出公开集合后仍能继续。
- 新实体搜索不调用作品 `RecordSearchOutcome`，避免把人物/厂牌零结果混入旧作品指标；错误和分页同样不产生作品零结果统计。
- `/search` 在端点和类型生成完成后开放作品/人物/厂牌标签。缺省仍为作品；URL 使用 `type=performer|studio`，提交新查询或切换类型清除旧 cursor，重试/翻页保留真实 q/type/cursor。
- 非法 type 在前端明确关闭，不向未知端点发请求；人物和厂牌结果使用真实实体 DTO、链接和图片，不用作品卡冒充，也不输出总数或虚构 match reason。
- OpenAPI 新增两个路径、两个响应 schema 和生成 TypeScript 类型；DEV-01 合同快照把这两个路径从“待新增”移至“已实现新增”，其余 DEV-10 路径仍保持 planned。

## 排序与游标合同

| 实体 | 排名顺序 | 同档稳定键 | 游标内容 | 锚点消失 |
| --- | --- | --- | --- | --- |
| 人物 | 规范名精确、别名/原文/罗马字精确、前缀、其他文本 | 小写显示名、UUID | v1 + query/type scope + rank + name + UUID | 直接按元组比较继续 |
| 厂牌 | 名称/归一名精确、前缀、其他文本 | 归一化名称、UUID | 同上 | 同上 |

游标不承诺并发更新下的快照一致性；结果集在翻页期间新增或改名时，用户可刷新重查。排序规则若改变，需升级 cursor version，而不是让旧游标静默解释为新规则。

## 真实 PostgreSQL 结果

一次性 PostgreSQL 16 / Schema v21 / 受限 `platform_api_login` 仅使用 development fixtures 和本轮随机合成资料：

- 人物 7 条 DEV-01 查询通过：中文规范名、罗马字别名、两个同名实体、日文名、拉丁别名，以及 hidden/takedown 排除。
- 厂牌 5 条 DEV-01 查询通过：两个同名实体、draft/hidden/takedown 排除和真实零结果。
- 另建 26 个人物与 26 个厂牌同档前缀结果；记录第 24 条排序元组后让该实体及 publication 退出公开集合。两类下一页均准确返回原全集第 25～26 条，无重复、无漏项、无首页重放。
- HTTP 层验证首屏 25 条仓储结果只返回 24 条和 next cursor，空尾页返回 `items=[] / next_cursor=null`；坏游标、跨词和跨类型均在仓储调用前返回 `INVALID_CURSOR`。

容器和全部合成数据在测试后自动删除，不可恢复；未连接长期或生产数据库。

## EXPLAIN 与索引决定

在同一 PostgreSQL 16 合成库以受限 API 账号执行人物/厂牌核心查询 `EXPLAIN (ANALYZE, BUFFERS)`：

| 查询 | 执行时间 | 观察 |
| --- | --- | --- |
| 人物“测试人物” | 1.874 ms | 使用 `search_documents_entity_type_entity_id_key`、人物主键和 `publications_public_idx`；25 条排序内存 27 kB |
| 厂牌“测试厂牌” | 0.669 ms | 使用搜索文档实体索引、厂牌主键和发布索引；当前 development seed 无厂牌搜索文档，返回 0 条 |

该结果只证明当前 SQL 在真实 PostgreSQL、现有 Schema/index 和小型合成集上可执行，不代表生产数据规模容量结论。由于现有索引已参与、DEV-01/本轮规模无慢查询证据，本项不增加迁移、不引入外部搜索服务；生产代表性数据量的 EXPLAIN/延迟门槛保留到 DEV-15 发布验收。

## 验证结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node scripts/tests/redesign_multi_entity_search.test.mjs` | 5/5 通过 | 标注集、OpenAPI、公开视图/排序、元组游标、type 分发与指标隔离 |
| DEV-01/08 合同回归 | 9/9 通过 | 基线快照、作品阶段 A 与新增路径状态同步 |
| display-web lint / typecheck / production build | 通过 | 三类型页面、真实 DTO、URL 条件和生产制品 |
| `npm run check:api-types` | 通过 | OpenAPI 与生成类型一致 |
| platform-api `go test ./...` | 通过 | 回环授权后全包通过；HTTP、媒体投影、安全及仓储回归 |
| 一次性 PostgreSQL 16 / Schema v21 / 受限账号 | 通过 | 12 条标注查询、两类 26 条同档分页及锚点消失续接 |
| `node scripts/release_a_frontend_smoke.mjs` | 通过 | 作品/人物/厂牌结果、真实 href、非法 type 关闭及既有两站回归 |
| Playwright 用例发现 | 94 条成功 | desktop/mobile 均发现新增三类型切换场景 |
| DEV-09 定向 Playwright desktop | 未进入浏览器步骤 | 当前服务器缺 `/opt/google/chrome/chrome`，错误为 Chrome distribution not found |
| Markdown / `git diff --check` | 通过 | 文档链接和差异空白检查 |

## 未关闭门禁

- AC-14 的 API、真实 SQL、公开过滤、同名实体、准确类型与 HTTP 页面已通过；仍缺 desktop/mobile 真实 DOM、键盘标签切换、图片降级和进入详情的浏览器证据。
- AC-15 的版本/作用域/排序元组、坏游标、跨词/跨类型、空尾页和锚点消失已通过；并发新增/改名不承诺快照一致，生产代表性规模性能仍需 DEV-15 环境复核。
- 服务器没有 Chrome/Chromium，因此 DEV-09 保持“待验收”，不计入已验收完成数。
- DEV-08 已记录的 DEV-05 广告环境模板 Python 合同失败仍未在本项混入修复；它不由搜索改动引入，需单独关闭。

## 回退

前端可隐藏人物/厂牌标签并回到默认作品搜索，旧作品 API 不需变更。回退新端点不涉及数据迁移；若保留端点但改变排序，必须升级游标版本并明确旧游标返回 `INVALID_CURSOR`。不得回退为跨类型复用游标、查询锚点已消失就重放首页，或把人物/厂牌零结果计入作品指标。
