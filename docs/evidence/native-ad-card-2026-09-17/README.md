# 原生作品卡广告验收证据

验收日期：2026-09-17（Asia/Shanghai）

范围：作品列表原生广告卡、推荐区原生广告卡、列表下方横幅、无广告作品补位、运营管理与数据库约束。

结论：审查环境内所有可执行项通过；可以进行产品视觉确认。当前素材为模拟广告，不能据此宣称真实商业投放已经验收。

实现基线见[原生作品卡广告与推荐位回退执行书](../../redesign/NATIVE_AD_CARD_ITERATION_2026-09-17.md)。机器可读结果见 [results.json](./results.json)。

## 环境基线

| 项目 | 验收值 |
| --- | --- |
| 公开审查入口 | `http://117.72.170.254:13000/`，`/latest` 返回 HTTP 200 |
| API 镜像 | `self-deepsearch/platform-api:ux3-d020592`，`/readyz` 与 `/healthz` 返回 200，容器健康 |
| 公开站镜像 | `self-deepsearch/display-web:ux3-fb7bb70`，容器健康 |
| 运营后台镜像 | `self-deepsearch/ops-web:ux3-7a1d4a5`，容器健康 |
| 数据库 | schema 27；`native` 已加入广告形态约束 |
| 广告活动 | 共 21 个；18 个已发布且未暂停，3 个旧 `after8` 模拟活动已暂停 |
| 本轮活动 | 7 个原生卡、3 个列表下方横幅，均由正式运营 API 创建并发布 |

## 关键结果

| 标准 | 结果 | 证据 |
| --- | --- | --- |
| UX3-A01 列表原生卡 | 通过 | 最新发行、最近收录广告位为第 8 个网格单元；热门浏览为第 4 个网格单元；均未横跨整行 |
| UX3-A02 推荐原生卡 | 通过 | 首页、作品详情、人物详情、厂牌详情推荐/关联区均在第 4 位显示广告卡 |
| UX3-A03 无广告回退 | 通过 | 同版本禁用广告后 native 和 banner DOM 均为 0，第 8 个位置恢复为作品 `article` |
| UX3-A04 作品完整性 | 通过 | 有广告和无广告时最新发行均有 24 个作品，作品链接数组完全一致；有广告时只增加 1 个网格子项 |
| UX3-A05 下方横幅 | 通过 | 最新发行、最近收录、热门浏览的作品网格下方均显示横幅；禁用广告时不输出横幅 DOM |
| UX3-A06 响应式 | 通过 | 1440px 原生卡占四列中的一列；390px 占两列中的一列；侧栏隐藏；横向溢出为 0 |
| UX3-A07 广告辨识 | 通过 | 原生卡显示“广告”、标题和广告主，不显示作品排名、编号或推荐偏好按钮 |
| UX3-A08 回归 | 通过 | OpenAPI 类型、TypeScript、lint、前端生产构建、迁移合同、Go 变更包、真实 Chromium 和审查栈均通过 |

## 浏览器机器结果摘要

- 最新发行：24 个作品、1 个原生广告卡，共 25 个网格子项；广告在第 8 位。
- 最近收录：24 个作品；广告在第 8 位；下方横幅可见。
- 热门浏览：5 个作品；广告在第 4 位；下方横幅可见。
- 首页推荐、作品相关、人物关联和厂牌关联：广告均在第 4 位。
- 无广告对照：24 个作品、0 个原生广告、0 个横幅；第 8 位是作品；作品链接与有广告状态完全一致。
- 390px 手机：原生广告和横幅可见，侧栏不可见，横向溢出为 0。

## 截图

- [最新发行原生卡桌面](./latest-native-desktop.png)
- [最新发行无广告作品补位](./latest-no-ads-fallback.png)
- [首页推荐原生卡](./home-recommendation-native.png)
- [作品详情相关推荐原生卡](./work-related-native.png)
- [最新发行原生卡手机](./latest-native-mobile.png)

## 已执行检查

| 检查 | 结果 |
| --- | --- |
| `node --test scripts/tests/redesign_ad_policy.test.mjs scripts/tests/redesign_detail_contract.test.mjs scripts/tests/public_experience_iteration.test.mjs` | 通过 |
| `npm run check:api-types` | 通过 |
| 公开站与运营后台 TypeScript、lint、生产构建 | 通过 |
| `node scripts/run_python.mjs db/tests/test_migrations.py` | 17 项通过 |
| API `internal/httpapi` 与 `internal/database` 包测试 | 通过 |
| schema 27 顺序锁定迁移 | 通过，`applied=1`、`status=current` |
| Chromium 有广告/无广告、三类列表、四类推荐区、桌面/手机矩阵 | 通过 |
| 固定镜像、容器健康、API 就绪与公网 HTTP | 通过 |

## 尚不能完成的验收及原因

| 项目 | 原因 | 上线前动作 |
| --- | --- | --- |
| 真实广告素材与点击归因 | 当前为无跳转模拟素材，没有接入广告供应商 | 在预发布环境验证素材审核、落地页、点击归因、失败回退和财务口径 |
| 真实填充率与频次控制 | 审查环境固定返回单个活动，没有真实竞价和流量分布 | 接入供应后补充按用户/会话的频次上限、填充率、收入和异常监控 |
| 生产域名、HTTPS 与 CDN | 当前为 IP + HTTP 审查入口 | 域名证书就绪后验证缓存、素材 CDN、安全头和跨区访问 |

这些项目不阻塞当前产品效果审查，但属于生产发布前门槛。
