# 运营优化开发自测与验收记录

- 日期：2026-09-17（Asia/Shanghai）
- 固定实现：`57818cd5150e36a4dc4a2fdfee80495267869b31`
- 范围：OPT-00～08 / OA-01～24
- 结论：开发自测完成，待独立测试与产品验收；不满足生产放行条件。

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
| OPT-00 | `ec0ac54` | 已提交，OA-01 PASS、OA-02 PARTIAL |
| OPT-01 | `5374f1b` | 已提交，OA-03～06 PASS |
| OPT-02 | `795c903` | 已提交，OA-07/08 PARTIAL |
| OPT-03 | `ee5b51e` | 已提交，OA-09～11 PARTIAL |
| OPT-04 | `f054616` | 已提交，OA-12/13/15 PASS、OA-14 PARTIAL |
| OPT-05 | `c60773b` | 已提交，OA-16 PASS、OA-17 PARTIAL |
| OPT-06 | `78d0bc7` | 已提交，OA-18 PARTIAL、OA-19 PASS；每日调度和稳定续批仍缺 |
| OPT-07 | `8f01b3c` | 已提交，OA-20 PARTIAL、OA-21 BLOCKED |
| OPT-08 | `6075181`、`57818cd` | 已提交，OA-22 PASS、OA-23/24 PARTIAL |

固定镜像均带实现 SHA 标签：

| 镜像 | Image ID |
| --- | --- |
| `self-deepsearch/platform-api:opt-57818cd` | `sha256:52fa1122d89bb71eccf5f8b2f303be356ad7c0d286fe2bfae5eba08ee9d04ea7` |
| `self-deepsearch/platform-worker:opt-57818cd` | `sha256:34d6c73442dd0b752da104ff182afee33aa93477b1863046925cd8d7321401dd` |
| `self-deepsearch/display-web:opt-57818cd` | `sha256:36eb0da08ef72ca08f71daba203f7702b305b067d79ffbff177a3bd54b18a464` |
| `self-deepsearch/ops-web:opt-57818cd` | `sha256:c109ccc13ccf284b011843bb419a2f57e840daf0bcf907f528ea7f1b557b117d` |

## 3. 已执行验证

固定源码快照执行并通过：

- 固定源码快照的 Markdown 链接为 104 个文件、678 个本地链接、0 缺失；加入本验收证据后复跑为 125 个文件、769 个本地链接、0 缺失。
- OpenAPI 类型生成检查；display、ops、api-contracts 三工作区 typecheck；全工作区 lint。
- `test:frontend:smoke`：45 项通过；`test:preflight`：77 项通过；改版合同：68 项通过。
- Python：317 项通过、356 个子测试通过；静态集成合同 103 项及 22 个子测试通过；迁移合同 16 项通过；Ruff 通过；MyPy 15 个源文件通过。
- Go：platform-api 与 platform-worker 全量测试通过；两服务 `go vet` 通过。
- display-web 与 ops-web production build 通过；四个最终 Docker 镜像构建成功。
- PostgreSQL 16 / Schema 26：用户、广告、SEO 问题、SEO 报表真实合同通过；205 个合成用户的分页遍历无重漏。
- 备份恢复：隔离库 dump/restore 后 Schema 26、205 个用户和表清单一致；dump SHA-256 为 `542984d87320fd6cf30100d31daf4918e9e8c22d85cfc2b768a8979b75797025`。
- 测试栈升级前备份 SHA-256 为 `1ed7d9268c124ae07d9b0e5a029d1b3f3ac856c61983b970e72e82416c2e03ec`。

## 4. 运行态验收

测试栈数据库已升级到 Schema 26。API、Worker、公开站和运营后台四个最终镜像均为 healthy；API `/healthz` 返回版本 `opt-57818cd`。

HTTP 结果：公开首页、后台登录页、`/users`、`/ads`、`/seo`、`/seo/issues`、`/seo/reports` 均为 200；完整 HTML 中未发现 `self-deepsearch-stream-error`、`Internal Server Error` 或 `Application error`。owner 账号通过产品自身的 Mailpit 密码重置、登录和 `/api/v1/me` 链路验证。

运行态数据是测试数据：用户总数 3，其中管理账号 3、关闭 2、普通用户 0；广告活动 0；SEO 发布记录 130，因内容分类未知而全部 fail-closed，不进入 sitemap。这些数字只说明当前测试库，不代表生产业务数据。

## 5. 未达项与原因

- OA-02/07～11/14/17：服务器没有 Chrome、Chromium 或 Firefox。Playwright 能发现 98 条用例，但未执行真实浏览器、键盘/触摸、四宽度/1199～1200px 边界、截图、trace、LCP/CLS。
- OA-18：当前手动扫描单批最多 1,000 条并显式标记 `truncated`，但尚未实现每日调度和超过 1,000 URL 的稳定续批；外部抓取目标的真实执行链也未建立。该项属于功能缺口，不是单纯环境阻塞。
- OA-20：样本、错误格式、上限、公式拒绝、幂等和数据集隔离已测；没有 Search Console 所有权和授权的真实导出，因此真实来源部分未验。
- OA-21：未按 10,000 用户/实体、200 次样本与真实浏览器条件跑完整性能预算。生产依赖联网审计会向外部 registry 发送依赖清单，本轮未获授权，记录 BLOCKED；镜像构建期间 `npm ci` 的输出不能代替正式审计证据。
- OA-23/24：隔离备份恢复和固定镜像部署已完成，但未执行生产/目标环境发布与应用回退，且缺独立测试、产品和生产负责人签收。

详见 [results.json](./results.json)、[manifest.json](./manifest.json)、[blockers.md](./blockers.md) 和各 OPT 子目录。任何 PARTIAL/BLOCKED 均不得用于生产放行。
