# DEV-14 验收记录

日期：2026-09-16（Asia/Shanghai）
状态：待验收

## 已实现

- Schema v22 为出版实体增加内容分类、SEO 质量结论与审核信息，为媒体资产增加内容分类、外部预览权限与审核信息；历史数据默认 unknown，约束阻止半审核状态和 explicit + allow。
- sitemap 只收录 `eligible` 且分类已知的正式实体；公开详情只给前端最终 `seo` 策略和 `external_preview_allowed`，不泄露内部操作人、理由或原始字段。
- 三类详情页按实体结论输出 index/noindex、成人标记、图片预览策略；OG/JSON-LD 只使用获准外部预览的图片。站内图片展示不受外部预览权限替代。
- 运营修订/发布、媒体 manifest、北京媒体处理器和运营导入界面已接入分类与权限，发布审计保留最终结论。
- 正式域名与 Search Console 提交流程落在[DEV-14 SEO 正式放行单](../../redesign/DEV14_SEO_RELEASE_GATE.md)，未伪造外部权限或提交结果。

## 自动验证

| 项目 | 结果 |
| --- | --- |
| Go API HTTP/数据库单元测试 | 通过 |
| API contracts、display-web、ops-web TypeScript | 通过 |
| OpenAPI 生成一致性 | 通过 |
| SEO Node 合同 | 通过，含缺失/畸形策略 fail-closed |
| Python DB 迁移合同 | 通过 |
| media-python pipeline | 通过，含 unsafe 预览组合预解码拒绝 |
| PostgreSQL 16 Schema v22 Up、受限账号、合成夹具 | 通过 |
| PostgreSQL Up/Down roundtrip | 独立 PostgreSQL 16 容器通过；含夹具幂等、可见性、备份保留与最终 schema/role 清理 |
| 完整 API PostgreSQL 契约 | 独立空库、受限 `platform_api_login` 与显式 owner URL 一次执行通过（8.731s） |
| DEV-14 专用 PostgreSQL 合同 | 通过；覆盖 unknown 排除、eligible 入图、non-explicit 标准预览、explicit 成人标记/禁图及 explicit + allow 约束拒绝 |
| media-python 全套 | 81/81 通过；含真实回环 HTTP 与 unsafe 预览组合预解码拒绝 |
| 全仓 Python / pytest 回归 | 325 passed，348 subtests passed；回环 HTTP 用例在授权网络沙箱外执行 |
| 真实浏览器 | 未执行；服务器缺 `/opt/google/chrome/chrome` |

## 未关闭门禁

- 生产存量数据尚未统计、抽样和回填，不能把 unknown 批量推断为可索引或允许图片预览。
- 缺最终非回环 HTTPS 域名、边缘/CDN 入口及真实浏览器复验。
- 缺 Search Console 已验证所有者权限，未提交正式 sitemap。

因此 AC-25/AC-30/AC-28 仅完成代码与本地数据合同部分，DEV-14 保持“待验收”。
