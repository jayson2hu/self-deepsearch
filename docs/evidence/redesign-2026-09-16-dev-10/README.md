# RD v1 / DEV-10 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，合同/真实 SQL/生产构建/HTTP 冒烟通过，真实浏览器待验收。基线提交：`24c852c`。

## 实际交付

- 新增 `GET /api/v1/performers/{slug}/works?cursor=` 与 `GET /api/v1/studios/{slug}/works?cursor=`，统一返回 `{items, next_cursor}`，不返回无法可靠维护的总数。
- 排序固定为 `release_date DESC NULLS LAST, published_at DESC, work_id ASC`。未知发行日期放在已知日期之后；同日期、同发布时间最终由 UUID 升序稳定打破并列。
- v1 游标包含父类型/slug 绑定 scope、发行日期、正式发布时间和作品 UUID。服务端拒绝坏游标、跨父级和跨类型游标；继续查询直接比较完整元组，不依赖锚点仍在公开集合中。
- 仓储先从 `public_published_performers` / `public_published_studios` 确认父实体公开，再只从 `public_published_works` 读取作品。父实体隐藏或下架返回 404，作品隐藏后不再出现。
- 人物关系来自 `work_performers`；厂牌关系来自作品真实 `studio_id`。作品卡 href 使用作品正式 slug，厂牌信息仍由公开厂牌对象连接，不从名称拼接 slug。
- 原人物/厂牌详情响应中的 `works` 字段和 100/200 摘要上限保留兼容；新前端详情页不再把该摘要声称为全集，而是单独请求分页端点。
- 人物/厂牌详情的下一页是服务端输出的普通 `<a href>`，关闭 JavaScript 仍可直接访问。新页、尾页、坏游标和服务故障分别显示准确状态；有 cursor 的空尾页不再误报父实体“没有作品”。
- OpenAPI 和 DEV-01 合同快照已把两条关联分页路径从 planned 移到 implemented；没有数据库迁移。

## 真实 PostgreSQL 边界结果

一次性 PostgreSQL 16、Schema v21、受限 `platform_api_login` 写入随机合成资料：

| 场景 | 结果 |
| --- | --- |
| 人物 101 条关联作品 | 5 页全部可达，ID 与一次性全集查询完全一致，无重复、无遗漏 |
| 厂牌 201 条关联作品 | 9 页全部可达，ID 与一次性全集查询完全一致，无重复、无遗漏 |
| 日期与并列 | 每 3 条共享发行日期，全部共享正式发布时间；UUID 稳定排序，另有 11 条未知日期全部位于末尾 |
| 锚点退出公开集合 | 记录人物第 24 条完整元组并隐藏该作品及 publication，续页仍精确返回原全集第 25 条以后 |
| 父级隐藏 | 人物和厂牌实体及 publication 分别隐藏后，两类仓储方法均返回 `catalog.ErrNotFound` |
| 真实关系/链接来源 | 201 条作品均保留真实厂牌名称与正式作品 slug；人物仅通过 101 条 `work_performers` 关系进入结果 |

容器与全部合成数据在测试结束后自动停止并删除；未连接长期或生产数据库。

## 验证结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node scripts/tests/redesign_related_works.test.mjs` | 4/4 通过 | 101/201 边界、OpenAPI、公开父级、完整元组和前端直达链接 |
| DEV-01 基线合同 | 6/6 通过 | 新路径状态、旧详情兼容和全部 DEV/AC 可追踪性 |
| display-web lint / typecheck / production build | 通过 | 独立分页请求、尾页/坏游标/故障状态和生产制品 |
| `npm run check:api-types` | 通过 | OpenAPI 与生成类型一致 |
| platform-api `go test ./...` | 通过 | HTTP、游标、媒体投影、安全与仓储全量回归 |
| 一次性 PostgreSQL 16 / Schema v21 / 受限账号 | 通过 | 101/201 全集、日期并列、未知日期、锚点消失和两类父级隐藏 |
| `node scripts/release_a_frontend_smoke.mjs` | 通过 | 详情首屏、普通下一页 href、空尾页、失效游标及既有两站回归 |
| Playwright 用例发现 | desktop 48 条、两项目合计 96 条 | 新增人物/厂牌关联分页浏览器场景已被发现 |
| DEV-10 定向 Playwright desktop | 未进入浏览器步骤 | 当前服务器缺 `/opt/google/chrome/chrome`，错误为 Chrome distribution not found |

## 未关闭门禁

- AC-15 的版本、父级/类型作用域、坏游标、稳定元组、锚点消失和空尾页已由 HTTP 与真实 PostgreSQL 通过；翻页期间新发布/改期不承诺快照一致，刷新第一页可获得新集合。
- AC-16 的 101/201 全集、未知日期、真实关系、真实 href、无总数和父级隐藏已通过 API/SQL/服务端 HTML；仍缺真实 Chrome 下关闭 JavaScript、键盘导航、desktop/mobile 布局截图。
- 分页 canonical、prev/next 搜索引擎策略属于 DEV-13；本项维持详情主 canonical，不提前宣称 SEO 分页验收完成。
- 当前服务器没有 Chrome/Chromium，因此 DEV-10 保持“待验收”，不计入已验收完成数。

## 回退

前端可暂时回到旧详情内嵌摘要，但必须明确它是有限预览，不能显示“共 N 部”或声称全量。新端点没有迁移，可独立回退；若未来改变排序，必须升级 cursor version。不得回退为只携带作品 ID、查询已消失锚点后重放首页、从厂牌名称拼 slug，或为分页伪造总数。
