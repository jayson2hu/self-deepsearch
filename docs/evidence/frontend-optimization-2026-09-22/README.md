# 首批前端优化验证 · 2026-09-22

对应[全面优化计划](../../FRONTEND_OPTIMIZATION_PLAN.md)。本批包括公开站搜索/故障表达、采集列表/详情、密码二次确认，以及设计方向预览。尚未覆盖全部 51 个页面模板，也未宣称全站 WCAG 符合、生产性能或真实用户任务效率达标。

## 真实采集后台

[浏览器报告](browser-report.json)使用上轮双项目联调的真实 Go API、专用 PostgreSQL 和 4,196 条人物候选/9 批次。更新前端构建并重启原验收后台后，完成全字段对账、匿名/普通用户拒绝、运营角色读取、搜索/空态/分页、第二页与筛选条件从详情返回、手机核对与退出登录。数据保持暂存，公开接口中仍为零条。

320、390、760、761、768、1024、1440px 检查无整页横向溢出。手机以卡片显示核对入口，不需要横向拖动表格。此前[原布局截图](../collector-e2e-2026-09-22/collector-mobile.png)保留作为修改前证据。

- [新版桌面全页](collector-desktop.png) / [桌面首屏裁剪](collector-desktop-firstscreen.png)
- [新版手机全页](collector-mobile.png) / [390×844 首屏裁剪](collector-mobile-firstscreen.png)
- [候选详情](collector-detail.png)

首屏裁剪只裁去原截图下方内容，不修改页面元素。首轮新增严格标签定位发现候选状态选择器的可访问名称不明确，记录在[首次失败报告](collector-browser-failed-attempt-1.json)；补 `aria-labelledby`、重建并重跑后通过。

复现脚本为 [test_collector_e2e_browser.mjs](../../../scripts/test_collector_e2e_browser.mjs)，运行需要已有隔离验收环境、私有凭据及 `expected.json`。完整新建环境可按[双项目验收文档](../../COLLECTOR_E2E_ACCEPTANCE.md)运行。敏感凭据留在 `.cache/collector-e2e-20260922-final/access.json`，未复制到证据目录。

## 公开站

[公开站浏览器报告](public-browser-report.json)为正式 Next 构建 + 随机回环端口合成 API，11 项检查通过。覆盖三种类型的首页 GET 查询、提交关键词与类型返回、四种视口、实际代理 503 保留游标重试、400 返回目录、无外网请求及无运行错误。不是实际来源作品资料或生产性能验收。

代表截图：[首页桌面](public-home-1440.png)、[首页手机](public-home-390.png)、[搜索桌面](public-search-1440.png)、[搜索窄屏](public-search-320.png)、[故障手机](public-latest-unavailable-390.png)。其中安全测试标题含转义后的 `<script>` 文本，属于合成样本。

可复现命令：

```sh
node scripts/test_frontend_public_optimization.mjs
```

该脚本使用当前构建的独立副本，结束后清理临时服务。代理故障页保留 400/503、no-store、noindex 与 503 的 Retry-After，未放宽 CSP。

## 密码确认与通用检查

独立浏览器脚本 [test_ops_reauth_browser.mjs](../../../scripts/test_ops_reauth_browser.mjs)覆盖遮挡输入、键盘循环、取消/Escape、错误密码、断网、重试上限、并发及调用者取消，使用合成密码与接口。管理页面的实际组件流程另用既有 Playwright 合成 API 回归。

[密码确认验证记录](reauth-validation.json)：15 条相关单测、独立 Chromium 9 场景、既有管理流程桌面 22/22 与手机 22/22 均通过。首次集成运行受 EPIPE/143 中断；随后分组完整重跑取得 exit 0，未把中断运行算作通过。覆盖邀请、角色、审核发布、权利处置、媒体、用量和上传控制；未使用真实生产密码或执行生产操作。

两端 production build（含 TypeScript）与全量 ESLint 已通过；采集导航参数、密码确认、媒体上传客户端、公开页面契约、截图基线及既有前端冒烟单测通过。测试结果明细与构建标识见[验证记录](validation.json)。

## 视觉方向预览

[方向页面](../../frontend-optimization/index.html)只用于说明公开站和后台的版式、颜色及实施路线，不连接业务接口。[预览报告](direction-preview-report.json)覆盖五种宽度、三面板切换、键盘、示意查询和详情展开。

代表图：[公开方向](direction-public-desktop.png)、[后台方向](direction-ops-desktop.png)、[后台方向手机](direction-ops-mobile.png)。这些图不作为已上线页面的证据。

## 本批剩余范围

会话三态、其他后台的手机规则与权限导航、全站视觉变量/组件、五类完整样板、目录分页/对象选择、结构化审核、性能采样和全站无障碍仍按计划推进。公开站现有双主题也需要后续完整状态回归；本批不宣称这些项目已完成。

## 提交内容独立验证

为避免依赖其他未提交功能，在 HEAD 加本批选定代码的隔离副本中重新通过两端正式构建、lint、相关单测、类型生成校验和文档链接检查；真实 PostgreSQL 合约确认采集接收与后台接口可在 schema 27 独立运行，4,196 条/9 批及重放通过。未纳入访客统计 migration 28。见[提交快照验证](commit-snapshot-validation.json)和[schema 27 真库报告](collector-schema27-contracts.json)。此前使用 schema 28 工作树的历史报告保持原样。
