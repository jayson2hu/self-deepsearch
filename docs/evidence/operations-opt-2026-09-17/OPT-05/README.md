# OPT-05 SEO 清单与总览

实现提交：`c60773b`。后台 `/seo` 提供三类实体的实际 title/description/canonical/robots、索引与 sitemap 资格、排除原因和修订入口，复用既有公开资格并对 unknown fail-closed。测试栈 130 条发布记录均因分类未知排除，符合保护策略。OA-16 PASS；OA-17 缺真实分类修订的完整浏览器链，PARTIAL。
