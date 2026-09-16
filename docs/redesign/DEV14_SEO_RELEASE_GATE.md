# DEV-14 SEO 正式放行单

更新时间：2026-09-16（Asia/Shanghai）

本文件是正式索引的部署签署单，不是开发环境开关说明。代码完成不代表允许搜索引擎抓取；只有下列三阶段分别满足，才可将 `SEO_INDEXING_ENABLED` 与 `SEO_CONTENT_POLICY_READY` 同时设为 `true`。

## 1. 已落地的数据合同

实体出版记录必须保存：

- `content_classification`：`unknown` / `non_explicit` / `explicit`；
- `seo_quality_status`：`unknown` / `eligible` / `ineligible`；
- 非 unknown 结论的审核时间与审核人。

图片资产必须保存：

- `content_classification`：同上；
- `external_preview_permission`：`unknown` / `allow` / `deny`；
- 非 unknown 结论的审核时间与审核人。

安全规则：任一结论未知时不索引、不进入 sitemap、不允许外部图片预览；`explicit` 图片不能设置 `allow`。站内已获权展示与外部搜索/社交预览是两个独立权限，不因站内可见自动允许外部预览。公开 DTO 只输出最终布尔/展示策略，不输出审核人、内部理由或原始分类字段。

## 2. 正式矩阵

| 实体结论 | 页面索引 | sitemap | 成人标记 | 外部图片预览 |
| --- | --- | --- | --- | --- |
| unknown / 任意 unknown 组合 | 否 | 否 | 不推断 | 否 |
| non_explicit + eligible | 是 | 是 | 无 | 仅图片为 non_explicit + allow 时 standard |
| explicit + eligible | 是 | 是 | `rating=adult`，结构化数据非家庭友好 | 否 |
| 任意分类 + ineligible | 否 | 否 | 可保留真实成人标记 | 否 |

此外仍受 DEV-13 环境门禁约束：必须是 production、真实非回环 HTTPS origin、两个显式 SEO 开关均开启。任一条件不满足时全站 fail-closed。

## 3. 三阶段放行

### A. 开发与数据准备

- [x] Schema v22 迁移可 Up/Down；旧数据默认 unknown。
- [x] 运营修订与发布可记录实体分类、质量结论、审核人和审计日志。
- [x] 媒体处理器/运营导入可记录图片分类与外部预览权限。
- [x] sitemap、公开 API、页面 robots、OG 与 JSON-LD 使用同一最终策略。
- [ ] 存量生产数据完成统计、抽样和回填；不得全量推断为 allow。

### B. 最终 HTTPS 入口预发

- [ ] 正式域名：`____________________________`
- [ ] DNS / TLS / CDN / origin 负责人：`____________________________`
- [ ] 预发核验日期：`____________________________`
- [ ] 逐项验证 200/301/400/404/503、canonical、robots、X-Robots-Tag、sitemap 分片、缓存与图片头。
- [ ] 抽查 unknown、eligible non-explicit、eligible explicit、ineligible 四类页面及允许/拒绝图片。
- [ ] 确认测试域、IP、临时域名仍为 noindex，且未提交 sitemap。

### C. Search Console 正式提交

- [ ] 产权资源：`Domain / URL-prefix（圈选）`
- [ ] 已验证所有者账号：`____________________________`
- [ ] 验证方式与日期：`____________________________`
- [ ] 提交的正式 sitemap index：`____________________________`
- [ ] 提交时间与提交人：`____________________________`
- [ ] 首次覆盖率/抓取异常复核日期：`____________________________`

当前状态：B/C 未完成。仓库没有真实域名和 Search Console 所有者权限，因此不得填写为已通过，也不得提交测试入口。

## 4. 回退

发现误索引、分类错误、图片许可错误或入口配置异常时，先将 `SEO_INDEXING_ENABLED=false`，确认 robots 与 HTML/X-Robots-Tag 恢复 noindex，再修正数据或配置。回退不会即时清除搜索引擎已缓存结果；由已验证所有者按正式工具流程处理临时移除或重新抓取，并保留操作证据。
