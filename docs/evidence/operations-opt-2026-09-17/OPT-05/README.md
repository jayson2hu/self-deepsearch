# OPT-05 SEO 清单与总览

实现提交：`c60773b`。后台 `/seo` 提供三类实体的实际 title/description/canonical/robots、索引与 sitemap 资格、排除原因和修订入口，复用既有公开资格并对 unknown fail-closed。真实扫描先验证 130 条合格内容无问题，再切换为 unknown 后产生 130 个问题；重复扫描保持 130 个问题行并累计 260 个事件。OA-16 PASS；OA-17 仍缺双管理员完整 UI 修订、审核、发布链，保持 PARTIAL。
