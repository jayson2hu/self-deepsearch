# RD v1 / DEV-03 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成，自动化与重点真实 SQL 通过，完整发布/回滚浏览器链待验收。基线提交：`e6aac9d`。

## 实际交付

- OpenAPI 为作品、人物、厂牌详情追加可选 `public_updated_at`；作品追加可选 `studio` 对象，并保留 `studio_name`。
- Go 目录层从三类公开视图的 `publication.published_at` 读取公开时间；作品厂牌只在厂牌自身公开时返回真实 ID/name/href。
- 三类详情显示 `资料更新：YYYY-MM-DD`，使用 `<time datetime="RFC3339">` 并按 Asia/Shanghai 转换；旧 API 缺字段时不显示，不用浏览器当前时间补齐。
- 作品详情使用服务端返回的厂牌 href，不从名称拼 slug；JSON-LD publisher 与页面厂牌名称保持一致。
- 合成 API、生成 TypeScript 类型、Go HTTP/数据库合同和浏览器用例规格同步。

## 验证结果

| 命令/环境 | 结果 | 范围 |
| --- | --- | --- |
| `node scripts/tests/redesign_baseline.test.mjs` | 6/6 通过 | DEV/AC/合同基线与新 OpenAPI 一致 |
| `node scripts/tests/redesign_detail_contract.test.mjs` | 3/3 通过 | 三类详情、时间格式、厂牌链接与旧字段降级 |
| `npm run check:api-types` | 通过 | OpenAPI 生成类型无漂移 |
| display-web typecheck / lint / production build | 通过 | 前端类型、静态检查、15 个构建页面 |
| `go test ./services/platform-api/internal/httpapi ./services/platform-api/internal/database` | 通过 | Go HTTP/数据库包单元与编译；无数据库环境的合同用例按既有守卫跳过 |
| 隔离 PostgreSQL 16、Schema v21、受限 `platform_api_login` | 通过 | 三类详情读取正式发布时间；作品返回真实公开厂牌；草稿 revision 不改变公开快照；正式 publication 时间转换后正文与时间同一读取结果 |
| `npm run smoke:frontend:release-a` | 通过 | 生产制品三类详情、JSON-LD、安全头、代理错误态 |
| `git diff --check` | 通过 | 文本差异 |

Go 锁定依赖通过 `goproxy.cn` 下载到 `/tmp`；未修改 go.mod/go.sum。真实 SQL 使用独立临时 PostgreSQL 容器和仓库现有迁移/合成种子，验收后容器及其中数据已删除，不接触现有 review 数据库。

## 未关闭门禁

- AC-02 的接口、兼容降级和 DOM 实现有自动证据；AC-03 已验证草稿不改变公开时间及 publication 时间刷新后的原子读取，但尚未完成“运营 UI 创建新草稿 → 异人审核 → 重新发布 → 选择历史版本回滚”的完整真实 API/Worker/浏览器链。
- AC-04 的 DTO 与页面没有新增来源、核验人或审核面板；浏览器截图仍受 DEV-02 相同的 Chrome/Chromium 缺失阻塞。
- AC-27 的发布、Worker 缓存失效、隐藏/下架完整闭环留到 DEV-15。本批因此标记“待验收”，不计已完成 DEV。

## 回退

前端可先回退并继续容忍后端附加字段；后端保留可选字段不会破坏旧客户端。无需数据库迁移。不得通过把 `public_updated_at` 改为实体 `updated_at`、构建时间或当前时间来维持显示。
