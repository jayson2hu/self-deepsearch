# OPT-07 搜索报表与性能

实现提交：`8f01b3c`。Schema v26 与后台 `/seo/reports` 支持 owner-only CSV 导入；限制 5 MiB/10,000 行、精确表头、公式拒绝、哈希幂等和真实/演示隔离。真实 PostgreSQL 导入合同通过。无授权 Search Console 导出，OA-20 PARTIAL；未执行固定 10k/浏览器性能和联网生产依赖审计，OA-21 BLOCKED。
