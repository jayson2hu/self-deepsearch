# 公开站体验迭代验收证据

验收日期：2026-09-17（Asia/Shanghai）

范围：列表广告、详情浏览计数、公开聚合数量、浅色/深色主题。

结论：审查环境内所有可执行项通过；可以进行产品视觉确认，尚不能据此宣称生产环境或真实商业投放已验收。

实现与验收基线见[执行书](../../redesign/PUBLIC_EXPERIENCE_ITERATION_2026-09-17.md)。本目录只使用合成目录、模拟广告和审查环境聚合数据，不包含真实用户、IP、个人浏览历史或真实商业广告。

## 环境基线

| 项目 | 验收值 |
| --- | --- |
| 公开审查入口 | `http://117.72.170.254:13000/`，`/latest` 返回 HTTP 200 |
| API 镜像 | `self-deepsearch/platform-api:ux2-d34ab2a`，容器健康 |
| 公开站镜像 | `self-deepsearch/display-web:ux2-e65fafa`，容器健康 |
| 模拟广告 | 11 个已发布活动：原首页 1 个，本轮列表 10 个；五类列表的信息流与桌面侧栏广告位均存在 |
| 数据 | 合成目录与审查数据库；不代表生产流量 |

## 关键验收结果

| 标准 | 结果 | 证据 |
| --- | --- | --- |
| UXA-01/02 列表广告 | 通过 | 最新发行桌面第 8 项后恰有 1 个信息流广告并有 1 个右侧栏；五类列表共 10 个本轮广告位均已发布；广告选择 API 返回对应模拟素材 |
| UXA-03 响应式广告 | 通过 | 1440px 显示信息流和侧栏；390px 仅显示信息流，侧栏隐藏，横向溢出值为 0 |
| UXA-04 浏览口径 | 通过 | 打开列表并点击下一页，30 天/累计保持 15/15；进入详情后变为 16/16，恰好增加 1 |
| UXA-05 统计显示 | 通过 | 详情 API 返回 `view_count_30d` 与 `view_count_total`；作品详情显示“近 30 天”和“累计” |
| UXA-06/07 主题 | 通过 | 浅色、深色与手机截图完成；切换深色后刷新仍为 `dark`，画布为 `#101418`；手机无横向溢出 |
| UXA-08 回归交付 | 通过 | 合同测试、API 类型同步、前端类型检查、lint、生产构建、Go 变更包测试、真实浏览器和目标审查栈均通过 |

机器可读浏览器结果见 [results.json](./results.json)。截图包括[列表浅色桌面](./latest-light-desktop.png)、[列表深色桌面](./latest-dark-desktop.png)、[列表深色手机](./latest-dark-mobile.png)和[详情统计](./work-detail-stats.png)。

## 已执行检查

| 检查 | 结果 |
| --- | --- |
| `node --test scripts/tests/redesign_ad_policy.test.mjs scripts/tests/redesign_detail_contract.test.mjs scripts/tests/public_experience_iteration.test.mjs` | 通过 |
| `npm run check:api-types` | 通过 |
| `npm exec tsc -- --noEmit`（`packages/api-contracts`、`apps/display-web`） | 通过 |
| `npm run lint`（`apps/display-web`） | 通过 |
| `npm run build`（`apps/display-web`） | 通过 |
| Go `internal/httpapi` 与 `internal/database` 包测试 | 通过 |
| Chromium 真实页面、翻页、详情上报、Cookie 主题刷新和响应式矩阵 | 通过 |
| 目标审查栈容器健康、固定镜像与公网 HTTP | 通过 |

## 尚不能完成的验收及原因

| 项目 | 原因 | 上线前动作 |
| --- | --- | --- |
| 真实商业广告的素材、点击和结算 | 当前没有接入真实广告供应商；本轮按产品要求使用无跳转、无第三方脚本的模拟广告 | 接入供应商后在预发布环境验证素材审核、落地页、点击归因、失败回退与财务口径 |
| 生产域名、HTTPS、CDN 与缓存 | 当前为 IP + HTTP 的审查环境，不是生产拓扑 | 域名与证书就绪后执行 canonical、缓存头、跨区访问、失效刷新和安全头验收 |
| 真实流量统计准确性与并发 | 审查数据为合成数据，单浏览器只能验证写入边界和聚合贯通 | 上线前压测上报接口；上线后对日志、小时聚合和公开总数做分时抽样对账 |
| 全量辅助技术与设备实验室 | 已完成 Chromium 尺寸与键盘可见性合同，但当前没有实体设备/读屏实验室 | 发布前补 VoiceOver/TalkBack、Safari、Firefox 及实体手机抽检 |

这些项目不是本轮代码缺陷，但属于从“审查环境通过”升级为“生产可发布”的必要门槛。
