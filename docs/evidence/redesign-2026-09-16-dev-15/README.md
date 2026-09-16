# DEV-15 集成验收记录

日期：2026-09-16（Asia/Shanghai）
状态：待验收
功能冻结 SHA：`0bb78378ab766e589651f3b88f94ddf0a6ec6bd6`

## 自动检查

| 项目 | 结果 |
| --- | --- |
| Markdown 链接 | 108 个文件、698 个本地链接、0 缺失 |
| OpenAPI 生成一致性 | 通过 |
| TypeScript | display-web、ops-web、api-contracts 全部通过 |
| ESLint | display-web、ops-web 通过 |
| RD v1 Node 合同 | 14/14 测试文件通过，含 DEV-15 交接闭环合同 |
| 前端安全/媒体/交接 smoke | 8/8 通过 |
| 发布工具预检 | 首次在限制沙箱内 2 个回环端口文件级失败；授权后串行复跑 76/76 通过 |
| Python / pytest | 325 passed，348 subtests passed；回环 HTTP 用例在授权环境复跑 |
| 生产构建 | display-web 与 ops-web 均通过 |
| standalone HTTP smoke | 通过 12 类公开站/后台/错误态/SEO 检查 |
| Playwright 用例发现 | desktop/mobile 共 98 条；未执行浏览器步骤 |
| Go 包与真实 PostgreSQL | 功能冻结 SHA 未再改代码；沿用 DEV-14 同一代码的两 Go 模块全包、Schema v22 Up/Down、受限账号和专用 SEO SQL 通过证据 |

## 中断、失败与未执行

- `npm run test:cache:release-a` 本次未进入断言：服务器没有 Go 可执行文件，工具报“无法启动 Go 合同”。旧的 2026-09-12 passed JSON 已恢复，没有用旧证据冒充当前复跑。
- 真实 Chrome/Chromium 未执行：`/opt/google/chrome/chrome` 不存在，因此没有 320/390/768/1440 截图、键盘遍历、真实浏览器功能与性能报告。
- 未执行目标 HTTPS/CDN/两地部署、真实对象存储、真实邮件、Search Console 和真实广告。
- Q-01 仍未签署，所以 AC-20/21 没有开发或验收对象。

## AC 收口

| AC | 当前结论 | 说明 |
| --- | --- | --- |
| 01～18 | 开发自测部分通过，待产品/浏览器补验 | 代码、类型、HTTP、SQL 或权限证据按各 DEV 归档；涉及真实浏览器的条件未全部具备 |
| 19 | 通过 | 未开放假历史开关、期限或永久删除承诺 |
| 20～21 | 待决策 | 依赖 Q-01 的新/旧用户策略、期限和恢复规则 |
| 22～25 | 代码/本地合同通过，待目标环境 | fail-closed、sitemap、结构化数据、成人标记和图片许可已落地；缺最终 HTTPS/边缘/媒体头复验 |
| 26 | 阻塞 | 缺真实 Chrome 和目标环境性能样本 |
| 27 | 部分通过 | 各链路的 API/SQL/独立证据存在；本次缓存合同因缺 Go 未复跑，也未执行完整目标栈 |
| 28 | 文档就绪，待负责人放行 | 已固定代码 SHA、本地指纹、配置、迁移、顺序和回退；未生成正式镜像 digest |
| 29 | 通过 | 审核→决策→设计→DEV→AC→证据可追踪；Markdown 链接 0 缺失 |
| 30 | 开发阶段通过，预发/正式阶段阻塞 | 缺生产回填统计、最终 HTTPS、已验证所有者和正式 sitemap 提交 |

## 结论

15 个工作包都已有独立实施/决策或交接记录，但只有 DEV-01 已达到本轮完整验收口径。DEV-15 可交接，不可写成“可直接上线”。产品负责人应先签署 Q-01 或明确继续排除 DEV-12，再按[DEV-15 交接](../../redesign/DEV15_HANDOFF.md)完成浏览器、目标环境与放行签字。
