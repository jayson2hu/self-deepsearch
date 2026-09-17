# OPT-07 搜索报表与性能

实现提交：`8f01b3c`。Schema v26 与后台 `/seo/reports` 支持 owner-only CSV 导入；限制 5 MiB/10,000 行、精确表头、公式拒绝、哈希幂等和真实/演示隔离。演示 CSV 已真实走 multipart API：2 行成功、重复导入复用 ID、公式文件返回 400。10,000 个普通测试账号下三个用户 API 各 200 样本 P95 均低于 80ms；公开页和用户后台 Chrome LCP/CLS 达标。无授权 Search Console 导出，OA-20 PARTIAL；联网生产依赖审计未获明确授权，OA-21 仍为 BLOCKED。
