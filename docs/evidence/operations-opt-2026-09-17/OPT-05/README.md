# OPT-05 SEO 清单与总览

实现提交：`c60773b`。后台 `/seo` 提供三类实体的实际 title/description/canonical/robots、索引与 sitemap 资格、排除原因和修订入口，复用既有公开资格并对 unknown fail-closed。真实扫描先验证 130 条合格内容无问题，再切换为 unknown 后产生 130 个问题；重复扫描保持 130 个问题行并累计 260 个事件。随后 owner 通过 UI 提交 TEST-001/002 分类修订，两个不同合成管理员分别审核，owner 发布；可索引 0→2，正式门槛临时前台输出 `index, follow` 且 sitemap 含两条 URL。OA-16/17 PASS。
