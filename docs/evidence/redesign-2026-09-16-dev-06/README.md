# RD v1 / DEV-06 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，组件/HTTP/真实 SQL 合同通过，真实浏览器账号隔离矩阵待验收。基线提交：`3f6c13c`。

## 实际交付

- 工作网格必须显式声明 `context="recommendation" | "catalog"`。首页最新发行、热门和作品相关项使用 recommendation；搜索、最新/最近完整目录及关注人物作品使用 catalog。
- recommendation 才读取私有 `/api/v1/me/hidden-works` 并过滤；catalog 不读取、不展示“不再推荐”按钮，也不会因偏好返回零结果。偏好读取使用 `cache: "no-store"`，失败时保留完整公开推荐并提供重试。
- 用户文案统一为“不再推荐”；成功提示为“已减少这部作品的推荐，仍可搜索找到”，推荐列表提供即时撤销，详情和 `/account/hidden` 使用“恢复推荐”与“推荐偏好”。兼容原 API、表名和账号路径，无迁移。
- `ListFollowedWorks` 删除 `hidden_preferences` 条件。用户已经明确关注人物后，该人物的已发布作品仍在明确列表中；收藏、历史、直接详情、人物/厂牌关联继续使用原公开目录规则。
- 管理员 `hidden/takedown` 仍由 `public_published_works` 统一控制。偏好管理本身也只连接公开视图，因此个人恢复不能重新暴露管理员隐藏资料。
- OpenAPI 和有状态 mock 同步新语义；浏览器用例增加“推荐中消失、搜索可找回、关注作品仍可见、偏好页可恢复”的路径。

## 场景矩阵实现

| 场景 | context / 仓储 | 个人偏好结果 |
| --- | --- | --- |
| 首页最新发行、热门、作品相关项 | `recommendation` | 过滤，可不再推荐与撤销 |
| 搜索、最新、最近收录 | `catalog` | 不过滤 |
| 人物/厂牌关联作品 | 普通公开详情列表 | 不过滤 |
| 收藏、历史、关注人物作品 | 私有明确列表；关注查询只连公开视图 | 不过滤 |
| 直接详情、推荐偏好管理 | 公开详情 + 私有偏好状态 | 详情可访问，管理页可恢复 |
| 管理员隐藏/权利下架 | `public_published_*` 视图 | 所有入口不可见，个人不能恢复 |

## 验证结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node --test scripts/tests/redesign_recommendation_context.test.mjs` | 通过 | 页面 context 映射、推荐专用读取/过滤/撤销、准确文案、关注仓储/OpenAPI/mock 合同 |
| display-web typecheck / lint / production build | 通过 | 类型、静态规则及生产制品 |
| `npm run check:api-types` | 通过 | OpenAPI 与生成类型保持一致 |
| Go database / HTTP 包 | 通过 | 账号仓储与处理器回归 |
| Python 3.12 产品合同 | 46/46 通过 | 私有页面失败状态及既有产品边界 |
| 一次性 PostgreSQL 16 / Schema v21 / `platform_api_login` | 通过 | 同一作品设置推荐偏好后仍出现在关注人物作品；管理员 hidden 后从关注和偏好管理同时消失 |
| `node scripts/release_a_frontend_smoke.mjs --build` | 通过 | 两站生产构建、公开页面、安全头和 API 代理 no-store 回归 |
| Playwright 用例发现 | 84 条成功 | 定向 DEV-06 用例可被 desktop/mobile 项目发现 |
| DEV-06 定向 Playwright desktop | 未执行浏览器步骤 | 回环授权后确认 `/opt/google/chrome/chrome` 不存在；错误为 Chrome distribution not found |

真实 SQL 使用独立临时 PostgreSQL 容器和合成 UUID/文本，不加载生产数据、不访问图片或网络来源；验证后容器和合成数据已删除。第一次迁移运行发现 Alpine `mktemp` 后缀差异，使用仅存在于 `/tmp` 的兼容包装完成验收，没有修改生产迁移脚本。

## 未关闭门禁

- AC-09 的后端与页面合同已覆盖，真实 SQL 已证明关注明确列表不受偏好影响且管理员隐藏优先；但“标记→推荐消失→搜索/目录找回→撤销”的真实 DOM、键盘、加载时序仍需 Chrome/Chromium 执行 desktop/mobile 用例。
- AC-10 已确认私有 fetch 为 `no-store`、公开服务器响应未按 Cookie 分叉、偏好故障降级保留完整列表；仍缺两个独立账号加匿名会话的真实浏览器隔离与偏好服务故障网络证据。
- 当前服务器未安装 Chrome/Chromium。没有用静态源码检查冒充账号隔离或浏览器验收，因此 DEV-06 保持“待验收”，不计入已验收完成数。

## 回退

若偏好服务或客户端逻辑不稳定，优先关闭 recommendation 的个人过滤并展示完整公开推荐；搜索、目录、详情和个人明确列表不得退回全站过滤。回退不能改变管理员隐藏/下架的公开视图约束，也不删除既有 `hidden_preferences` 数据。
