# 运营优化开发自测与验收记录

- 日期：2026-09-17（Asia/Shanghai）
- 固定实现：`90a09612ea6ce1ca5f5784a2d83c35e337bc018d`
- 范围：OPT-00～08 / OA-01～24
- 结论：开发自测与当前服务器可执行的真实/合成数据验收已完成；19 PASS、4 PARTIAL、1 BLOCKED。仍待独立测试与产品验收，不满足生产放行条件。

## 1. 已交付能力

- 页面 smoke 能识别 HTTP 200 中的 React/Next.js 流式渲染错误，不再只看状态码。
- Schema v23 增加注册来源与测试账号事实，后台提供同快照用户统计、稳定分页、筛选和角色/账号状态信息。
- 用户后台展示总量、普通账号、管理账号、公开注册、今日/7 日注册和统计覆盖说明；保留既有 owner/admin 权限与近期验证约束。
- 广告按页面、自然内容数量、槽位和宽度治理；搜索、工具、空态、错误页和无有效配置时不占位。
- Schema v24 增加广告活动与不可变版本；admin/owner 可维护草稿，只有 owner 可发布/暂停，前台 backend 模式 fail-closed。
- SEO 中心统一展示作品/人物/厂牌的 metadata、canonical、robots、sitemap 资格及排除原因。
- Schema v25 增加 SEO 扫描、问题去重、分派、复测、关闭和再打开历史。
- Schema v26 增加 CSV 报表导入；限制 5 MiB/10,000 行，拒绝公式单元格，按文件哈希幂等，真实与演示数据隔离。
- CI、监控、Schema 版本合同、数据模型文档和 Alpine/BusyBox 迁移脚本已同步至 v26。

## 2. 提交与制品

| 工作包 | 实现提交 | 结果 |
| --- | --- | --- |
| OPT-00 | `ec0ac54` | 已提交，OA-01/02 PASS |
| OPT-01 | `5374f1b` | 已提交，OA-03～06 PASS |
| OPT-02 | `795c903`、`7964fad` | 已提交，OA-07/08 PASS |
| OPT-03 | `ee5b51e` | 已提交，OA-09～11 PASS |
| OPT-04 | `f054616` | 已提交，OA-12～15 PASS |
| OPT-05 | `c60773b` | 已提交，OA-16/17 PASS |
| OPT-06 | `78d0bc7` | 已提交，OA-18 PARTIAL、OA-19 PASS；每日调度和稳定续批仍缺 |
| OPT-07 | `8f01b3c` | 已提交，OA-20 PARTIAL、OA-21 BLOCKED；性能部分已补实测 |
| OPT-08 | `6075181`、`57818cd`、`253b5d5`、`7964fad`、`90a0961` | 已提交，OA-22 PASS、OA-23/24 PARTIAL |

固定镜像均带实现 SHA 标签：

| 镜像 | Image ID |
| --- | --- |
| `self-deepsearch/platform-api:opt-57818cd` | `sha256:52fa1122d89bb71eccf5f8b2f303be356ad7c0d286fe2bfae5eba08ee9d04ea7` |
| `self-deepsearch/platform-worker:opt-57818cd` | `sha256:34d6c73442dd0b752da104ff182afee33aa93477b1863046925cd8d7321401dd` |
| `self-deepsearch/display-web:opt-90a0961` | `sha256:8283e6e557927841277fef16084b37ff771cd23ac986e4bf890a3d8c6ffc9075` |
| `self-deepsearch/ops-web:opt-90a0961` | `sha256:2ce9e7b083154484e08be992949a02859d3f2792264dbf06f4eb0d3c8c2e2538` |

## 3. 已执行验证

固定源码快照执行并通过：

- 固定源码快照的 Markdown 链接为 104 个文件、678 个本地链接、0 缺失；加入本验收证据后复跑为 125 个文件、769 个本地链接、0 缺失。
- OpenAPI 类型生成检查；display、ops、api-contracts 三工作区 typecheck；全工作区 lint。
- `test:frontend:smoke`：45 项通过；`test:preflight`：77 项通过；改版合同：68 项通过。
- Python：317 项通过、356 个子测试通过；静态集成合同 103 项及 22 个子测试通过；迁移合同 16 项通过；Ruff 通过；MyPy 15 个源文件通过。
- Go：platform-api 与 platform-worker 全量测试通过；两服务 `go vet` 通过。
- display-web 与 ops-web production build 通过；四个最终 Docker 镜像构建成功。
- Google Chrome 153 桌面/移动真实回归 98/98 通过；预检 77/77、前端 smoke 8/8 复跑通过。
- PostgreSQL 16 / Schema 26：用户、广告、SEO 问题、SEO 报表真实合同通过；205 个合成用户的分页遍历无重漏。
- fresh Schema 26 容器实测发现 bootstrap `exec` 导致初始化后退出，`253b5d5` 修复后新容器持续运行，迁移合同 16/16 通过。
- 备份恢复：隔离库 dump/restore 后 Schema 26、205 个用户和表清单一致；dump SHA-256 为 `542984d87320fd6cf30100d31daf4918e9e8c22d85cfc2b768a8979b75797025`。
- 测试栈升级前备份 SHA-256 为 `1ed7d9268c124ae07d9b0e5a029d1b3f3ac856c61983b970e72e82416c2e03ec`。

补充真实运行与合成数据验收：

- 隔离真实数据库写入 10,000 个普通测试账号；统计、100 条列表和筛选列表各执行 200 次、并发 5，P95 分别为 56.14ms、35.27ms、79.36ms，0 错误。
- 公开首页和后台用户页在 320/390/768/1199/1200/1440px 共 12 项真实 Chrome 检查中均为 200、无横向溢出、无后台服务告警。
- 公开首页/详情及后台用户页浏览器 LCP/CLS 均达标；公开页完整数据见[性能报告](../operations-opt-performance-2026-09-17.json)。
- 广告发布到 API+SSR 为 222.27ms；暂停后公开 API 8.18ms、新 SSR 105.48ms、已开页面 29.704 秒隐藏。
- SEO 演示报表真实走 multipart 导入、幂等与公式拒绝；130 URL 的重复扫描验证问题去重和发现历史。
- 两条 SEO 修订通过 owner 提交、不同管理员审核、owner 发布的真实 UI 闭环，可索引 0→2；正式门槛临时前台验证 `index, follow` 与 sitemap URL。

## 4. 运行态验收

测试栈数据库已升级到 Schema 26。API、Worker、公开站和运营后台均为 healthy；API/Worker 仍使用功能实现镜像 `opt-57818cd`，公开站/后台使用包含图标修复的 `opt-90a0961`。

HTTP 结果：公开首页、后台登录页、`/users`、`/ads`、`/seo`、`/seo/issues`、`/seo/reports` 均为 200；完整 HTML 中未发现 `self-deepsearch-stream-error`、`Internal Server Error` 或 `Application error`。owner 账号通过产品自身的 Mailpit 密码重置、登录和 `/api/v1/me` 链路验证。

运行态数据是测试数据：最终快照共有账号 7（均为管理测试账号，普通账号 0，公开注册 0）、广告活动 0、SEO 发布记录 130（技术可索引 2、排除 128）。其中 4 个 `seo-reviewer-*` 来自双管理员 SEO 验收，结束后均已锁定并撤销旧会话，仅保留审计关系；这些数字不代表生产业务数据。10,000 用户和测试广告在独立隔离栈执行后已清理。

## 5. 未达项与原因

- OA-18：当前手动扫描单批最多 1,000 条并显式标记 `truncated`，但尚未实现每日调度和超过 1,000 URL 的稳定续批；外部抓取目标的真实执行链也未建立。该项属于功能缺口，不是单纯环境阻塞。
- OA-20：样本、错误格式、上限、公式拒绝、幂等和数据集隔离已测；没有 Search Console 所有权和授权的真实导出，因此真实来源部分未验。
- OA-21：10,000 用户 API 与真实浏览器性能预算已完成并通过；生产依赖联网审计会向外部 registry 发送依赖清单，本轮未获明确授权，仍记录 BLOCKED。镜像构建期间 `npm ci` 的输出不能代替正式审计证据。
- OA-23/24：隔离备份恢复和固定镜像部署已完成，但未执行生产/目标环境发布与应用回退，且缺独立测试、产品和生产负责人签收。

详见 [results.json](./results.json)、[manifest.json](./manifest.json)、[blockers.md](./blockers.md) 和各 OPT 子目录。任何 PARTIAL/BLOCKED 均不得用于生产放行。
