# OPT-01 用户口径与接口

实现提交：`5374f1b`。Schema v23 新增 `registration_origin`、`is_test`、`registered_at`；注册完成幂等写入来源事实。新增用户 summary、稳定分页/筛选与 OpenAPI 合同，205 个合成账号真实 PostgreSQL 遍历无重漏。权限、近期验证、角色保护和审计回归通过。OA-03～06 PASS。
