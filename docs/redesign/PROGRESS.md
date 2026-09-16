# 重设计进度台账

更新时间：2026-09-16（Asia/Shanghai）。这是 RD v1 改版的唯一任务状态源，不覆盖[原 Release A 进度与证据](../PROGRESS.md)。

## 1. 当前结论

- 初次审核原文已归档；最新为 4 项初次采纳、3 项调整、2 项后续授权采用正式推荐。D-08/D-09 已通过 CHG-20260916-01 解除产品决策阻塞，详见[决策记录](./DECISIONS.md)。
- 9 份文档已编制：入口、审核原文、决策、产品设计、技术设计、开发计划、DEV-01 基线、验收计划、本台账；待产品负责人复核实现解释。
- 本次 SEO 范围的文档链接、编号/状态/映射和差异空白检查通过；全仓检查另发现采集相关文档 3 处缺失链接，未改动该范围，详见最新日志。首版通过结果保留历史。
- 新版产品代码已实施 DEV-02～11：查询、认证续接、广告、推荐、匿名说明、搜索、关联分页及“我的”入口/反馈状态均完成代码、类型、构建和重点合同验证；没有执行生产迁移、部署、真实广告启用、账号数据清理或搜索引擎提交。
- 开发工作包：1 / 15 已验收；12 项待验收、1 项未开始、1 项待决策。DEV-02～11、DEV-13/14 均缺各自真实浏览器、正式配置或完整发布链门禁，不计完成。
- 下一步保留 DEV-12 的历史生命周期决策门禁并推进 DEV-15 集成验收，同时继续 DEV-02～11/13/14 的补验。DEV-14 实际对外提交等待部署条件具备。

## 2. 状态规则

未开始 → 进行中 → 待验收 → 已验收；可标记待决策/阻塞/暂缓并记录原因。只有对应 AC 通过、证据可追踪且交付边界明确，才能标为已验收。完成数只统计已验收，不用文档页数或已写代码估算百分比。

认领任务时填写执行人、开始日期与计划；没有约定的时间一律“未排期”。每次会话/提交结束至少更新本表和日志。失败/skip 不删除；状态恢复时注明解锁记录。

## 3. 工作包状态

| ID | 当前状态 | 执行人 / 排期 | 本轮结果与下一步 | 实施/验收证据 |
| --- | --- | --- | --- | --- |
| DEV-01 | 已验收 | Codex / 2026-09-16 | 36 条核心样本、24 条标准查询、101/201/50,001 边界与合同快照完成；开发者自测通过，待产品复核 | [基线](./BASELINE.md)、[证据](../evidence/redesign-2026-09-16-dev-01/README.md) |
| DEV-02 | 待验收 | Codex / 2026-09-16 | 查询优先首页、四级导航和移动搜索入口已实现；构建/HTTP 通过，缺四宽度浏览器证据 | [证据与阻塞](../evidence/redesign-2026-09-16-dev-02/README.md) |
| DEV-03 | 待验收 | Codex / 2026-09-16 | 可选公开时间和厂牌对象已贯通 OpenAPI/Go/前端；重点真实 SQL 通过，缺完整重发/回滚/Worker/浏览器链 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-03/README.md) |
| DEV-04 | 待验收 | Codex / 2026-09-16 | 安全 returnTo、15 分钟标签页意图、返回确认/取消/下架处理与真实 SQL 幂等性已实现；缺真实浏览器认证矩阵 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-04/README.md) |
| DEV-05 | 待验收 | Codex / 2026-09-16 | empty/ready/error/preview、默认无 DOM、布局折叠与安全素材配置已实现；缺真实浏览器视觉矩阵，Q-05 继续阻塞真实广告启用 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-05/README.md) |
| DEV-06 | 待验收 | Codex / 2026-09-16 | 推荐/目录显式 context、准确文案/撤销、关注作品仓储语义与管理员隐藏优先级已实现；缺真实浏览器两账号/匿名隔离和故障矩阵 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-06/README.md) |
| DEV-07 | 待验收 | Codex / 2026-09-16 | `/about`、`/privacy`、注册/历史/设置/页脚入口和当前行为清单已实现；缺正式联系地址核实与真实浏览器长文布局/跳转证据 | [事实清单](./PRIVACY_FACTS.md)、[证据与剩余门禁](../evidence/redesign-2026-09-16-dev-07/README.md) |
| DEV-08 | 待验收 | Codex / 2026-09-16 | 作品阶段 A、四类状态、条件恢复、标注查询与模糊误匹配修正已实现；缺真实浏览器 desktop/mobile 恢复链 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-08/README.md) |
| DEV-09 | 待验收 | Codex / 2026-09-16 | 人物/厂牌端点、稳定排名元组游标、三类型切换与公开过滤已实现；缺真实浏览器和生产代表性规模性能复核 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-09/README.md) |
| DEV-10 | 待验收 | Codex / 2026-09-16 | 人物/厂牌独立分页、父级绑定完整元组游标、101/201 真实 SQL 边界与服务端直达 href 已实现；缺真实浏览器 desktop/mobile/关闭 JS 证据 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-10/README.md) |
| DEV-11 | 待验收 | Codex / 2026-09-16 | “我的”总入口、收藏/关注空态与去向、最多 12 条关注作品准确说明、反馈中文真实状态及本人/内部备注隔离已实现；缺真实浏览器账号闭环 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-11/README.md) |
| DEV-12 | 待决策 | 产品负责人 / 待确认 | Q-01：历史默认/期限/迁移/恢复未定；已提供签署模板，没有开发假开关或清理数据 | [决策门禁](./DEV12_DECISION_GATE.md)、[核对记录](../evidence/redesign-2026-09-16-dev-12/README.md) |
| DEV-13 | 待验收 | Codex / 2026-09-16 | fail-closed 环境门禁、真实 HTTP、分页 canonical、50,001 sitemap 分片/50 MB 边界、公开过滤及面包屑结构化数据已实现；缺真实 Chrome、最终 HTTPS/边缘和 DEV-14 分类媒体合同 | [证据与剩余门禁](../evidence/redesign-2026-09-16-dev-13/README.md) |
| DEV-14 | 待验收 | Codex / 2026-09-16 | Schema v22、实体资格/成人标记、媒体外部预览许可、运营审核与 fail-closed 输出已实现；缺生产回填、最终 HTTPS、真实 Chrome 与 Search Console 所有者提交 | [放行单](./DEV14_SEO_RELEASE_GATE.md)、[证据与剩余门禁](../evidence/redesign-2026-09-16-dev-14/README.md) |
| DEV-15 | 未开始 | 待认领 / 未排期 | 冻结实际交付范围后跑本轮完整验收 | 无；旧版 passed 不迁移到此行 |

## 4. 决策阻塞与非阻塞事项

| 项目 | 状态 | 影响/解除条件 |
| --- | --- | --- |
| Q-01 历史策略 | 未确认 | 补齐政策值与老数据处置，更新合同后解锁 DEV-12 |
| Q-02 后台审核去留 | 未确认，保留现状 | 只阻塞对后台发布规则的修改；公开站不展示工作流可先做 |
| Q-03/Q-04 SEO 市场/索引 | 已关闭（采用推荐方案） | D-08/D-09 已确定中文/Google 技术基准与正式索引/预览矩阵；DEV-14 解除产品阻塞 |
| Q-05 真实广告 | 未确认 | 不影响广告状态开发；真实素材/供应方启用需另确认 |
| Q-06 公开标题含义 | 已记录实现解释，待复核 | 默认资料自身标题，不新增来源列表；如要来源标题需补充 |
| 真实对象存储/边缘/域名等 | 原门槛仍未完成 | 以 Release A 就绪审计为准；不因本次文档而解除 |

## 5. 本轮日志

### 2026-09-16｜文档落地

执行：Codex；输入：负责人导出时间 16:23:44 的审核原文及“先文档、后自行开发”的要求。

完成：归档原文；逐项解释 D-01～D-09；形成 15 个开发工作包和 30 个验收用例组；明确标题/更新时间、访客广告、推荐偏好、内部审核隔离；区分可开工、局部待决策和不在范围。产品规划同步方向，架构/原进度增加新入口，旧候选稿标为历史。

证据范围：只读核对公开 DTO、OpenAPI、Go 仓储与发布时刻、认证返回、广告组件、偏好过滤、历史写入、npm/CI 测试入口。未执行产品构建、浏览器回归、SQL、迁移或目标部署。

实际验证：`npm run check:markdown-links` 通过（80 文件、579 本地链接、0 缺失）；一次性只读 Node 校验通过（8 文档、9 决策、6 个 Q、15 个任务定义/表格/状态行、30 组 AC、所有引用有效、13 未开始 + 2 待决策、4 采纳 + 3 调整 + 2 待审查与原文一致）；`git diff --check` 通过。编号检查不执行产品用例，AC-29 整组仍待开发交付复核。

### 2026-09-16｜CHG-20260916-01：SEO 正式基线

输入：用户要求“seo的意见，按照正式的推荐来即可，符合正式的产品”。完成：将 D-08/D-09 与 Q-03/Q-04 由待审查转为推荐方案落地；同步 SEO 方案、产品/技术合同、DEV-13/14、AC-22～AC-25/AC-30 和根级入口。当前 14 未开始 + 1 待决策，开发完成数仍为 0。

核对 Google 官方 noindex、robots meta/X-Robots-Tag、分页和露骨内容指南；文档采用中文官方入口。确定：中文产品、Google 官方机制首批技术基准；统一质量资格与正式页型矩阵；合格派生图标准预览、受限图限制、实际成人分类；测试/演示站保护不变。官方抓取建议与本产品保持既有年龄访问限制的取舍已明确，不假称所有产品配置都是官方强制。

未执行：产品代码、分类字段迁移、域名配置、工具账号验证、对外索引开放或 sitemap 提交。DEV-14 产品阻塞解除不等于发布验收通过；部署条件缺项在对应执行步骤记录。

本次文档校验结果：8 份改版文档的编号/映射、D-08/D-09 授权、Q-03/Q-04 关闭、DEV-14 未开始及 14/1 计数检查通过；初次审核原文未改写。本轮涉及的 SEO/改版/根级入口本地链接无缺失，`git diff --check` 通过。

全仓链接检查快照：87 份 Markdown、632 个本地链接，3 处失败均指向采集模块尚不存在的 `docs/data-acquisition/VALIDATION.md`：来源为 `docs/collection-demo/README.md`、`docs/data-acquisition/DEVELOPMENT_PLAN.md`、`docs/data-acquisition/README.md`。这些是本次 SEO 修改范围外的工作区内容，未替其他工作补文件或删除链接；不将本轮范围通过说成全仓通过。后续采集文档完成后再运行全仓检查。

### 后续追加模板

```text
日期 / 执行人 / DEV：
上次状态 → 本次状态：
实际完成、提交/工作树：
AC、执行环境、通过/失败/跳过、证据路径：
未完成或阻塞（不能省略）：
下一步、责任人、排期：
```

### 2026-09-16｜DEV-01 样本与合同基线

执行：Codex；状态：未开始 → 进行中 → 待验收 → 已验收。基于 `d79b304` 与 Schema v21 建立 36 条核心合成资料、24 条带标准答案查询、人物 101/厂牌 201 关联边界、50,001 sitemap 边界及当前/计划合同快照。未使用真实账号、邮箱、浏览历史或生产数据。

验收：`node scripts/tests/redesign_baseline.test.mjs` 6/6 通过；`npm run check:markdown-links` 与 `git diff --check` 通过。AC-29 仅在基线追踪范围内通过；其余功能用例未执行。证据见[DEV-01 记录](../evidence/redesign-2026-09-16-dev-01/README.md)。下一步 DEV-02 查询优先首页与页头。

### 2026-09-16｜DEV-02 查询优先首页与页头

执行：Codex；状态：未开始 → 进行中 → 待验收。首页收敛为主查询、最新发行和三条关联浏览入口；页头导航调整为查询/浏览/人物/我的，移动端保留搜索与账号入口；公开文案不再解释内部维护或审核工作流。未修改后台保护和数据合同。

自动验证：首页合同 4/4、display-web typecheck/lint、production build、Release A HTTP smoke、差异检查通过。浏览器环境无 Chrome/Chromium；Playwright 下载在 0% 停滞超过 3 分钟后中止，320/390/768/1440 截图、焦点与溢出证据未生成，AC-26 未通过，故不计已验收。详见[DEV-02 记录](../evidence/redesign-2026-09-16-dev-02/README.md)。

### 2026-09-16｜DEV-03 公开更新时间与厂牌链接

执行：Codex；状态：未开始 → 进行中 → 待验收。三类详情从公开 publication 读取 `public_updated_at`，作品返回可点击 `studio` 对象并保留 `studio_name`；前端按 Asia/Shanghai 显示日期且兼容旧 API 缺字段。未增加迁移、公开来源/核验/操作人字段。

验证：OpenAPI 生成检查、基线 6/6、详情合同 3/3、前端 typecheck/lint/build、Go HTTP/数据库包及生产 HTTP smoke 通过。另在独立 PostgreSQL 16 / Schema v21 / 受限登录上验证三类读取、草稿不改变公开时间和 publication 时间刷新后的正文/时间一致性；临时容器已删除。完整运营审核重发、历史版本回滚、Worker 缓存与浏览器链仍待后续环境，详见[DEV-03 记录](../evidence/redesign-2026-09-16-dev-03/README.md)。

### 2026-09-16｜DEV-04 认证续接

执行：Codex；状态：未开始 → 进行中 → 待验收。登录、注册和密码重置共享受限 `returnTo`；收藏/关注意图只在明确点击后写入当前标签页并保留 15 分钟，认证成功返回目标后必须再次确认才发送 POST。普通登录仍回首页，取消/过期/已完成/存储禁用/目标不可用均有降级或清理。

验证：returnTo 与意图合同 5/5、DEV-01/03 回归、display-web typecheck/lint/build、Go HTTP/数据库包、生产 HTTP smoke 通过。另在一次性 PostgreSQL 16 / Schema v21 / 受限登录上验证作品收藏和人物关注重复设置两次均只保留一条有效关系；容器与合成数据已删除。新增 Playwright 用例覆盖确认前后关系与恶意目标，但本机缺 Chrome/Chromium 未执行，故 AC-05/06 尚未完整通过，详见[DEV-04 记录](../evidence/redesign-2026-09-16-dev-04/README.md)。

### 2026-09-16｜DEV-05 广告按实际可用性显示

执行：Codex；状态：未开始 → 进行中 → 待验收。固定测试占位改为 empty/ready/error/preview 状态层；默认 empty/error 不输出广告 DOM，rail/footer 包装同步折叠。ready 要求显式开关、唯一槽位、固定尺寸、有效期和素材/目的地允许域名；preview 仅非生产显式夹具。未接广告联盟、追踪、结算或管理后台。

验证：广告策略 5/5、前端 smoke 回归、display-web typecheck/lint/build、Python 3.12 产品/部署合同 51/51、默认空态生产 HTTP smoke 通过；另以一次性进程注入合成 ready 素材，匿名与登录 Cookie 均输出相同 1 个 ready 槽位且无 preview/第三方脚本。Playwright 84 条用例发现成功但本机无 Chrome/Chromium，未完成四宽度空白/布局稳定性截图，Q-05 也未批准真实素材，详见[DEV-05 记录](../evidence/redesign-2026-09-16-dev-05/README.md)。

### 2026-09-16｜DEV-06 推荐偏好场景拆分

执行：Codex；状态：未开始 → 进行中 → 待验收。工作网格显式区分 recommendation/catalog；只有首页推荐、热门和相关作品读取并应用推荐偏好，搜索、完整目录和关注人物作品不再过滤。统一“不再推荐”、可搜索找回、撤销/恢复及失败降级文案；保留原 API、表名和路径。

验证：推荐语义合同、OpenAPI 生成检查、display-web typecheck/lint/build、Go 数据库/HTTP 包、Python 产品合同和生产 HTTP smoke 通过。另在一次性 PostgreSQL 16 / Schema v21 / 受限登录上证明设置偏好后关注人物作品仍可见，管理员 hidden 后关注列表与偏好管理均不可见；容器与合成数据已删除。Playwright 84 条用例发现成功，定向用例在获得回环权限后因本机缺 `/opt/google/chrome/chrome` 未进入浏览器步骤，故 AC-09/10 尚未完整通过，详见[DEV-06 记录](../evidence/redesign-2026-09-16-dev-06/README.md)。

### 2026-09-16｜DEV-07 真实说明

执行：Codex；状态：未开始 → 进行中 → 待验收。新增匿名 `/about` 与 noindex `/privacy`，从注册、历史、设置、关闭账号区和页脚可达；逐条说明默认历史写入、软清除、默认关闭的归档、关闭账号不全量删除及尚无统一期限/永久删除能力。内部事实来源另存 `PRIVACY_FACTS.md`，用户页面不暴露内部字段。

验证：说明合同、display-web typecheck/lint/build、Python 产品合同和生产 HTTP smoke 通过；HTTP 验证匿名 200、privacy noindex、about sitemap 和进程内测试联系邮箱。Playwright 用例发现由 84 增至 86 条，但当前无 Chrome/Chromium。正式联系邮箱尚未由负责人核实，因此 AC-11 仍未完整通过；Q-01 未改变，DEV-12 继续待决策。详见[DEV-07 记录](../evidence/redesign-2026-09-16-dev-07/README.md)。

### 2026-09-16｜DEV-08 作品搜索阶段 A

执行：Codex；状态：未开始 → 进行中 → 待验收。保留作品搜索 API 和 q/cursor 兼容，明确当前只返回作品；人物/别名/厂牌词只查关联作品。前端区分非法输入、查询绑定游标失效、真实零结果和服务故障，空查询不拉全量，新查询清游标，故障重试保留 q/cursor，分页 URL 使用真实服务端条件。

验证：DEV-08 合同 3/3、DEV-01/06 回归 10/10、display-web lint/typecheck/build、OpenAPI 生成、Go HTTP/database 和生产 HTTP smoke 通过。一次性 PostgreSQL 16 / Schema v21 / 受限登录执行 12 个标注场景；首轮发现 `DRAFT-008` 被过低 trigram 阈值误配到公开 `DEMO-*008`，收紧末级阈值后全通过。Playwright 92 条用例发现成功，定向 desktop 因服务器缺 `/opt/google/chrome/chrome` 未进入浏览器。额外 56 条 Python 部署合同有 1 条 DEV-05 广告环境模板遗留失败，已如实记录并留待独立修复。详见[DEV-08 记录](../evidence/redesign-2026-09-16-dev-08/README.md)。

### 2026-09-16｜DEV-09 多实体搜索阶段 B

执行：Codex；状态：未开始 → 进行中 → 待验收。新增人物/厂牌公开搜索端点与生成类型，按规范名/别名/前缀/文本稳定排序；v1 游标携带查询/类型 scope 和完整排序元组，不依赖锚点继续存在。端点完成后前端才开放作品/人物/厂牌标签；非法 type 不发未知请求，新查询/切换清 cursor，重试与翻页保留真实条件。新实体搜索不混入作品零结果指标，无迁移。

验证：DEV-09 合同 5/5、DEV-01/08 回归 9/9、display-web lint/typecheck/build、OpenAPI 生成、platform-api 全包和生产 HTTP smoke 通过。一次性 PostgreSQL 16 / Schema v21 / 受限账号验证 12 条标注查询和人物/厂牌各 26 条同档分页；第 24 条锚点退出公开集合后仍准确返回尾页。合成库 EXPLAIN 使用现有搜索文档/主键/发布索引并低于 2ms，暂不新增索引，生产代表性规模留 DEV-15。Playwright 94 条用例发现成功，定向 desktop 仍因缺 Chrome 未进入浏览器。详见[DEV-09 记录](../evidence/redesign-2026-09-16-dev-09/README.md)。

### 2026-09-16｜DEV-10 人物/厂牌关联作品真正分页

执行：Codex；状态：未开始 → 进行中 → 待验收。新增两类父级绑定分页端点，排序固定为发行日期降序（未知置后）、正式发布时间降序、UUID 升序；v1 游标携带完整元组并绑定父类型/slug，不查询锚点。旧详情内嵌 works 保留兼容，前端改为独立分页并提供普通下一页 href、准确尾页/坏游标/故障状态，不再显示截断数量为总数。

验证：DEV-10 合同 4/4、DEV-01 回归 6/6、display-web lint/typecheck/build、OpenAPI 生成、platform-api 全包和生产 HTTP smoke 通过。一次性 PostgreSQL 16 / Schema v21 / 受限账号验证人物 101 条、厂牌 201 条全集无重漏，11 条未知日期置后，同日期/同发布时间由 UUID 稳定排序；第 24 条锚点隐藏后仍准确续接，人物/厂牌父级隐藏均 404。Playwright 两项目 96 条用例已发现，定向 desktop 因缺 Chrome 未进入浏览器。详见[DEV-10 记录](../evidence/redesign-2026-09-16-dev-10/README.md)。

### 2026-09-16｜DEV-11 “我的”入口与反馈真实状态

执行：Codex；状态：未开始 → 进行中 → 待验收。新增“我的账号”总入口和关注人物作品页；收藏/关注补空态、重试与取消去向。关注作品明确为按发行日期排序的最多 12 条，不伪装成全集、新收录或未读通知。反馈类型/处理状态映射为中文真实枚举，提交后可进入本人状态页；登录续接不保存或自动提交反馈文本。

验证：DEV-11 合同 4/4、DEV-04 回归 5/5、display-web lint/typecheck/build、platform-api 全包和生产 HTTP smoke 通过。一次性 PostgreSQL 16 / Schema v21 / 受限账号证明两用户反馈严格隔离，内部审核 reason 只在审计日志中，用户列表不返回审核人、审核时间或 reason。Playwright 96 条用例发现成功，定向 desktop 仍因缺 Chrome 未进入浏览器。详见[DEV-11 记录](../evidence/redesign-2026-09-16-dev-11/README.md)。

### 2026-09-16｜DEV-12 历史生命周期决策门禁核对

执行：Codex；状态保持待决策。复核当前默认写入、软清除、默认关闭归档、无统一期限和关闭账号不等于删除的事实；新增可签署决策模板，覆盖新/旧用户、在线/已清除/归档/备份期限和恢复防复活。没有增加假开关、迁移、清理任务或期限承诺，没有触碰任何用户历史。AC-19 满足当前阶段要求；AC-20 等 Q-01 批准后执行。详见[DEV-12 门禁](./DEV12_DECISION_GATE.md)和[核对记录](../evidence/redesign-2026-09-16-dev-12/README.md)。

### 2026-09-16｜DEV-13 SEO 技术基础

执行：Codex；状态：未开始 → 进行中 → 待验收。新增正式环境+显式双开关+HTTPS origin 的 fail-closed 索引门禁；统一运行时 metadata origin、目录/详情/分页 canonical 与 robots；proxy 在流式正文前返回真实 301/400/404/503。sitemap API 增加确定性分页和总量，XML 按 50,000 URL/50 MB 分片，空目录与依赖故障不生成不完整地图，隐藏/下架由公开视图移除。三类详情新增可见面包屑及一致的 BreadcrumbList，未提前实现 DEV-14 的分类/媒体许可。

验证：SEO/基线/公开说明 Node 合同、20 条 Python 环境合同、OpenAPI 类型、display-web lint/typecheck/build、两 Go 模块全包、最终 standalone HTTP smoke 通过。一次性 PostgreSQL 16 / Schema v21 / 受限账号验证确定性跨页、公开过滤、真实 lastmod 和隐藏后移除；容器已删除。Playwright 定向用例已发现，但服务器缺 `/opt/google/chrome/chrome` 未进入浏览器；最终 HTTPS/边缘、内容分类和图片预览许可留 DEV-14/15。详见[DEV-13 记录](../evidence/redesign-2026-09-16-dev-13/README.md)。

### 2026-09-16｜DEV-14 SEO 正式策略

执行：Codex；状态：未开始 → 进行中 → 待验收。新增 Schema v22，把实体内容分类/SEO 质量结论与媒体内容分类/外部预览权限作为持久、可审核合同；旧数据默认 unknown。sitemap、公开 DTO、robots、成人标记、OG/JSON-LD 图片使用同一 fail-closed 决策；站内展示与外部预览分离。运营修订/发布、媒体 manifest、媒体处理器与后台导入均可录入结论并留审计，公开端不泄露审核人或内部理由。

验证：Go 单元包、三端 TypeScript、OpenAPI 生成、SEO Node、Python 迁移与媒体 pipeline 通过；独立 PostgreSQL 16 容器完成 Schema v22 全量 Up/Down、夹具/可见性/备份合同和最终 schema/role 清理，另一空库以受限账号和 owner URL 一次性通过完整 API 数据库合同；DEV-14 专用真实 SQL 通过 unknown 排除、eligible 入图、成人标记/禁图与非法许可约束；media-python 81/81（含回环 HTTP）通过，全仓 pytest 为 325 passed / 348 subtests passed。真实 Chrome、生产存量回填、最终 HTTPS 与 Search Console 所有者权限仍缺，因此 AC-25/28/30 未完整通过。详见[DEV-14 记录](../evidence/redesign-2026-09-16-dev-14/README.md)。
