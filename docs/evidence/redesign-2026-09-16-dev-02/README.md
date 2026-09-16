# RD v1 / DEV-02 开发与待验收记录

日期：2026-09-16（Asia/Shanghai）；执行人：Codex；状态：实现完成、自动检查通过，真实浏览器验收阻塞。基线提交：`c53ab83`。

## 实际交付

- 首页改为“先查询，再浏览”：首屏主搜索、一个主要目录“最新发行”、三个关联浏览入口；移除首页多榜单、编辑推荐和混排发现的主导位置。
- 页头主导航收敛为“查询 / 浏览 / 人物 / 我的”；手机页头保留独立搜索入口和账号入口。
- 用户侧首页与页脚移除维护、审核工作流表述；未改后台审核、发布、下架机制。
- 广告仍沿用旧 AdSlot，广告空态属于 DEV-05；本批只把正文广告位置移到主要内容之后。
- Next.js 生产构建改用 TypeScript 编译器 API，避免 CLI 捕获 `--showConfig` 空输出；没有启用 `ignoreBuildErrors`。

## 验证结果

| 命令 | 结果 | 范围 |
| --- | --- | --- |
| `node scripts/tests/redesign_home_contract.test.mjs` | 通过，4/4 | 首页顺序、导航、移动搜索入口、公开文案、构建类型检查配置 |
| `npm run typecheck --workspace @self-deepsearch/display-web` | 通过 | display-web TypeScript |
| `npm run lint --workspace @self-deepsearch/display-web` | 通过 | display-web ESLint |
| `npm run build --workspace @self-deepsearch/display-web` | 通过 | Next.js production build、完整 TypeScript 检查、15 个静态页生成 |
| `npm run smoke:frontend:release-a` | 通过 | 随机回环端口上的生产制品首页、详情、CSS、安全头、代理错误态 |
| `git diff --check` | 通过 | 文本差异 |

## 未通过门禁与阻塞

AC-01/AC-04 的结构、文案和生产 HTTP 证据已通过；AC-26 仍缺 320/390/768/1440 的真实浏览器截图、焦点和横向溢出证据，因此 DEV-02 不标记“已验收”。

环境没有 Chrome/Chromium。Playwright 固定版本下载在官方 CDN 0% 停滞超过 3 分钟，缓存仅 16 KiB，已中止；没有生成截图，也没有把静态 CSS 检查冒充浏览器结果。解除条件：提供可执行 Chrome/Chromium，或允许可用镜像完成 `npx playwright install chromium`，随后运行四宽度脚本并把证据补入本目录。

## 回退

可回退首页、导航、页头、页脚和 CSS；不涉及数据库、API 合同、用户关系或部署。若回退 Next 构建配置，须先证明 CLI `--showConfig` 捕获路径在目标构建机稳定工作，不能通过忽略类型错误替代。
