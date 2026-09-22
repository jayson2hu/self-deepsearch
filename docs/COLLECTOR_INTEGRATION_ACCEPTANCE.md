# 双项目真实数据联调与验收

日期：2026-09-19。结论：**真实数据暂存联调通过；正式产品发布链路未通过、尚待开发。** 范围：`self-deepsearch` 与同级 `self-deepsearch-collector` 的归档交接、测试库暂存、受保护预览。

2026-09-22 续作：已新增[人物元数据接收 v1](COLLECTOR_INGEST_V1.md)，完成版本化合同、来源凭据、批次幂等及隔离 PostgreSQL 验收；默认关闭、未部署，正式实体映射和审核发布仍待开发。下文保留 2026-09-19 固定归档测试工具的历史范围。

当前验收入口：2026-09-22 已完成用户确认的[数据接收与后台验收](COLLECTOR_E2E_ACCEPTANCE.md)，采集项目客户端 → 实际 API → 数据库 → 后台浏览器整链路通过；下文旧预览与固定归档导入记录继续保留。

## 基线与边界

- 采集代码基线：`129dba2d72ccdfeef1da89416e2bbab0ae2556c6`，数据基线：`95cac10a4579a4fd20f15575c2e77a351bb1284b`。
- 数据包：`runtime/jable-archive/20260919-final`；4,196 人物，130 头像，4,066 无头像；`needs_review / staging`。
- 网站测试栈：`self-deepsearch-review`，前台 13000，后台 13001，API 18080，PostgreSQL 15432；不触碰其他项目。
- 原始归档只读；不重爬外站、不热链、不将未知年龄标为已确认、不自动公开或建立已发布媒体。
- 现有网站尚无正式采集 ingest API。本轮提供**测试专用**适配器，只写 `collector.sources/publication_batches/source_records/normalized_records/media_staging`。不冒充已经完成正式人物实体、来源映射、审核任务和发布闭环。
- 现有未提交的 collection-demo 等文件属于其他工作，保留且不纳入本轮提交。

## 实施与验收计划

| 阶段 | 工作 | 通过标准 | 最终状态 |
| --- | --- | --- | --- |
| IT-01 | 两端运行、基线及包校验 | 服务健康；归档 manifest 全文件 hash/大小正确；SQLite 完整性正常 | 通过，804 文件 |
| IT-02 | 测试库暂存适配 | 4,196 个来源候选、130 个媒体候选；保留状态；事务失败不残留 | 通过 |
| IT-03 | 幂等重放 | 同包二次导入新增 0；来源键、hash、媒体关系可对账；不改人工状态 | 通过 |
| IT-04 | 内部真实数据展示 | 服务端分页/搜索；头像本地读取；无头像有缺省说明；详情可查 | 通过，24 条/页，共 175 页 |
| IT-05 | 自动化与浏览器 | 130 张真实解码；10 有图+10 无图；中文/日文/拉丁搜索；桌面/手机；无外站请求/绝对路径 | 通过 |
| IT-06 | 权限与公开隔离 | 未登录/普通用户不可访问；后台角色可访问；测试 noindex；公开数量不变 | 通过；真实 owner/user 会话，admin/editor 角色单元测试 |
| IT-07 | 证据、进度、提交 | 记录真实结果、命令、失败原因和未覆盖项；仅提交本轮文件 | 完成；实现提交 e7fd7ab，文档与证据独立提交 |

## 暂存合同

来源使用由 `jable_reference` 派生的固定 UUID。人物以来源+external_id+内容 hash 保存不可覆盖观测；归一化候选保留源稳定 performer_id。媒体 UUID 从源 media_candidate_id 派生，通过 source_record_id 关联；`entity_id=NULL`，避免把外部 UUID 冒充正式平台实体。不按姓名合并。

同归档重复导入只命中既有记录，不更新人工审核状态。批次记录原包 hash、来源提交和数量。全部验证成功后单事务写入；应用仅允许明确确认的测试环境。适配器固定本次 manifest hash 与数量，**不接收任意新归档**；今后的增量 hash/冲突处理必须在正式接收接口开发中单独设计和验收。

预览是单独的测试工具，读取实际 PostgreSQL 暂存数据，不让浏览器下载全部 JSONL；使用现有 API 校验 `sd_session` 与 owner/admin/editor 角色，所有图片和数据路由同样鉴权。只支持读取，媒体仅允许经过完整性校验的本地文件。查询用只读事务，密码只从环境变量读取。2026-09-19 用户明确批准公网预览后，13004 改为公网监听，鉴权未取消；13005 仍仅绑定 loopback，不能只依靠 noindex 保护资料。

## 未完成即不得宣称通过的项目

正式批次 ingest API、正式人物/来源映射与独立审核发布、对象存储及 CDN、定时增量采集、全站覆盖、生产 HTTPS 与负载压测均不由本轮测试适配器替代。头像使用权和人物成年状态未确认时不能通过公开发布验收。

## 执行进展

1. 发现正式 ingest 接口缺口，先记录测试边界，没有把已有合成演示作为真实数据证据。
2. 校验归档，运行采集项目原有审计、重新导出和展示构建；新导出逐条匹配原归档。
3. 新建测试暂存适配器和受登录保护的预览，写入 4,196 条来源记录、4,196 条归一化候选及 130 条媒体暂存。没有创建正式人物、来源实体映射或审核任务。
4. 重放归档新增人物/媒体均为 0。临时将一条本轮媒体标为 reviewing，再重放，状态仍为 reviewing；完成后恢复 pending。事务异常探针未留下记录。
5. 真实浏览器遍历 175 页并比对全部原字段；解码全部头像，检查详情、搜索、空结果、非法输入、手机布局和公开页面无服务器错误。
6. 未登录和伪造 cookie 均被拒绝。真实 owner 可访问；使用本机 Mailpit 注册一个合成 user 账号，页面/API/图片/脚本全部被拒绝，随后注销测试会话。
7. 运行采集端和网站相关回归，保留原始失败记录、通过报告及桌面/手机截图。重启最终版预览并完成健康复查。
8. 原有网站容器未重建、未更换镜像。采集仓库恢复干净；本轮未改采集代码。现有网站其他未提交修改原样保留。
9. 用户回复“是的”，批准带后台登录保护的公网预览。仅重启 13004 为 `0.0.0.0:13004`，未开放无鉴权的 13005，未调整其他服务或防火墙。服务端通过公网 IP 的匿名访问测试通过；真实登录和头像验证仅使用 loopback，未通过公网 HTTP 发送真实密码或会话。

## 结果与证据

| 验收项 | 实际结果 | 证据 |
| --- | --- | --- |
| 归档完整性 | 804/804 文件匹配；SQLite ok | [归档校验](evidence/collector-integration-2026-09-19/archive-verification.json) |
| 首次导入 | 4,196 来源 + 4,196 归一化 + 130 媒体；正式人物/公开发布增量均 0 | [首次回执](evidence/collector-integration-2026-09-19/import-first.json) |
| 幂等 | 第二次新增 0/0；总数不变 | [重放回执](evidence/collector-integration-2026-09-19/import-replay.json) |
| 数据库事务与审核状态 | 全字段匹配、孤儿媒体 0、非法状态 0、回滚无残留、保留人工状态 | [数据库验收](evidence/collector-integration-2026-09-19/database-report.json) |
| 网站侧浏览器 | 14 项检查通过；全量字段对账、130 解码、20 详情、搜索与手机布局 | [浏览器报告](evidence/collector-integration-2026-09-19/browser-report.json) |
| 采集侧浏览器 | 4,196 人物、130 解码、0 外站请求、0 运行时错误 | [采集预览报告](evidence/collector-integration-2026-09-19/collector-browser-report.json) |
| 真实普通账号权限 | 4 类预览路由均拒绝；注销成功 | [权限报告](evidence/collector-integration-2026-09-19/auth-report.json) |
| 单元与回归 | 103 用例通过：适配器 10；采集 Python 37 + JS 11；网站相关 JS 45 | [回归报告](evidence/collector-integration-2026-09-19/regression-report.json) |
| 视觉 | 1440px 桌面、390px 手机检查；无横向溢出 | [桌面](evidence/collector-integration-2026-09-19/preview-desktop.png)、[手机](evidence/collector-integration-2026-09-19/preview-mobile.png) |
| 获批公网预览 | 公网 IP 匿名/伪造会话均拒绝；本机登录查询及 24 张头像解码、注销撤销访问通过 | [开放后验证](evidence/collector-integration-2026-09-19/public-preview-report.json) |

最终复查：API/Worker readyz、网站首页、后台登录、13004 healthz、13005 采集预览均 HTTP 200；13004 匿名数据请求 HTTP 401；登录后查询数量仍为 4196/130/4066。

合成账号为 `integration-denied-1789782862654@example.test`，仅测试普通用户权限；保留在测试库，随机密码未记录，测试会话已注销。测试环境用户统计因此增加 1，不能当作真实注册增长。

## 失败、修正和未覆盖项

| 项目 | 原因与处理 | 当前结论 |
| --- | --- | --- |
| 浏览器第 1 次 | 统计缺省图时把已关闭详情弹窗也算入，24→25；选择器限定到列表 | [原始失败](evidence/collector-integration-2026-09-19/browser-attempt-1.json)，完整重跑通过 |
| 浏览器第 2 次 | Next 流式加载阶段出现两个 main，测试严格选择器报错；定位最终 main | [原始失败](evidence/collector-integration-2026-09-19/browser-attempt-2.json)，完整重跑通过 |
| 普通账号第 1 次 | 测试脚本未使用既有开发环境规定的 test-pass，人机校验按预期拒绝 | [原始失败](evidence/collector-integration-2026-09-19/auth-attempt-1.json)，按合同修正后通过；不是生产验证码绕过 |
| 采集捕获测试命令 | Node 22 不支持 package 脚本中的 --test-isolation=none | 初次以直接执行文件通过 3 用例；2026-09-19 续作已在 collector 提交 `5e436b4` 修复 npm 入口，`npm run test:jable-capture` 3/3 通过，未重采或修改归档 |
| SQLite 审计副作用 | 上游 mode=ro 仍生成空 WAL 与 SHM；原数据库 hash 未变化 | 两个临时文件移入本项目缓存留存；所有上游命令改在副本执行；原仓库已干净 |
| 公网联调预览 | 最初安全审核拒绝；随后用户明确批准带登录保护的公网访问 | 已开放 13004；13005 保持 loopback；匿名不可读取资料 |
| 公网登录测试及传输 | 安全审核拒绝通过公网 HTTP 发送真实管理员密码 | 改为公网匿名探测 + 本机鉴权验收；未测试异地网络可达性和携带真实凭据的公网浏览器登录；正式使用需 HTTPS，当前推荐 SSH 隧道 |
| 正式 ingest/发布 | 暂存适配器不包含正式人物实体、映射、审核、发布和对象存储流程 | 未完成，不通过正式上线验收 |
| 头像完整率 | 来源仅取得 130，另外 4,066 未取得 | 全部人物保留并显示缺省；不是下载失败，也不是 100% 人物有图 |
| 资料合法发布 | 权利 needs_review，人物成年状态未确认 | 不自动公开；不能将采集成功视为发布许可 |
| 新归档、全站及生产 | 仅固定归档，source full_site_coverage=false；未做生产压力/HTTPS/CDN验收 | 未覆盖，不宣称通过；本轮没有重新爬取外站 |

## 当前访问方式

- 原网站：`http://117.72.170.254:13000/`；原后台：`http://117.72.170.254:13001/`。
- 新的网站暂存预览：`http://117.72.170.254:13004/`，需要现有后台账号；匿名先显示登录提示。正常访问顺序为同主机后台 13001 登录后，再打开 13004。
- 采集项目自身预览：服务器 `127.0.0.1:13005`，仅本机的隔离展示产物。它不是正式网站。
- 两个新增预览为测试进程，不承诺主机重启后自动恢复；停止仅处理对应监听端口的已核对 PID，不停止原 review 栈。
- 公网地址当前为 **HTTP**，仅临时测试，不应使用生产密码或在不可信网络传输会话；推荐下述 SSH 隧道。已验证服务端访问自身公网 IP，尚无独立异地网络探测。

从个人电脑通过 SSH 访问（将 `SSH_USER` 替换为你的服务器用户）：

```bash
ssh -N -L 13001:127.0.0.1:13001 -L 13004:127.0.0.1:13004 -L 13005:127.0.0.1:13005 SSH_USER@117.72.170.254
```

先打开 `http://127.0.0.1:13001/login` 登录，再打开 `http://127.0.0.1:13004/`；采集侧展示为 `http://127.0.0.1:13005/`。务必使用同一主机名，不能将 `localhost`、`127.0.0.1` 和公网 IP 混用，否则 cookie 不共享。若本机端口被占用，先解决端口冲突。

## 复现与维护

在 `self-deepsearch` 根目录执行。依赖：同级固定版本采集仓库、Python 3.12、Node 22、psql、Chrome、现有 review 测试栈及正常的后台账号。没有依赖外部采集网络。

```bash
# 无数据库写入的完整性校验
python3 scripts/collector_integration.py verify

# 需将 INTEGRATION_DB_PASSWORD 设置为 review 测试库 platform 账号密码
python3 scripts/collector_integration.py import --confirm self-deepsearch-review-staging-only
python3 scripts/collector_integration.py import --confirm self-deepsearch-review-staging-only
INTEGRATION_TEST_CONFIRM=self-deepsearch-review-staging-only python3 scripts/test_collector_integration_database.py

# 原有测试、采集工作副本审计/导出/构建、采集浏览器；产生 .cache/collector-integration
python3 scripts/run_collector_integration_checks.py

# 另一个终端：将 INTEGRATION_DB_PASSWORD 设置为 platform_api_login 密码，仅开启 loopback
python3 scripts/collector_integration.py serve

# 用户明确批准的临时公网模式，仍保留全部后台鉴权；不是生产 HTTPS 部署
python3 scripts/collector_integration.py serve --expose-test-preview

# 再一个终端：只服务生成产物，不服务采集仓库根目录
python3 -m http.server 13005 --bind 127.0.0.1 --directory .cache/collector-integration/collector-showcase

# 需环境变量 INTEGRATION_OWNER_EMAIL / INTEGRATION_OWNER_PASSWORD，不写入证据
node scripts/test_collector_integration_browser.mjs

# 公网仅匿名探测，真实凭据仅用于本机登录；不会新建普通账号
node scripts/test_collector_public_preview.mjs

# 此项每次会新建一个合成普通账号，并在结束后注销会话
INTEGRATION_TEST_CONFIRM=self-deepsearch-review-staging-only node scripts/test_collector_integration_auth.mjs
```

证据报告由脚本写入固定目录；再次执行前应另存旧报告，不能抹去历史失败。源码实现提交：`e7fd7ab`（9 个本轮文件）。本文与证据独立提交，可用 `git log --oneline -- docs/COLLECTOR_INTEGRATION_ACCEPTANCE.md` 查看文档提交；未自动 push。

## 下一阶段开发顺序

1. 定义正式版本化 ingest API、服务身份认证、批次幂等与拒收回执；保持当前 4196/130 固定样本作为回归基线。
2. 实现候选→正式人物/来源映射→资料版本/字段来源→审核任务，审核状态不可被采集覆盖。
3. 实现头像隔离存储、来源与使用权限审核、媒体发布；无头像仍正常展示，不从其他网站静默补图。
4. 审核与成年状态满足要求后，验收公开人物页、站内搜索、SEO 和撤回缓存；未发布候选不得出现在 sitemap。
5. 再验收定时增量、失败重试、限额、生产部署和负载，不能把本轮测试工具部署成正式生产服务。
