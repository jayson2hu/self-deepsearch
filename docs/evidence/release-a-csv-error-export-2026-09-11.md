# Release A：作品 CSV 错误导出安全加固

日期：2026-09-11（Asia/Shanghai）  
结论：运营后台已有“下载错误 CSV”功能，但原实现仅处理逗号和双引号，没有中和表格公式。现已补齐公式注入防护、可测试的纯导出模块和默认回归门槛。

## 风险

作品番号来自操作者上传的 CSV。攻击者可提供以 `=HYPERLINK(...)`、`+`、`-`、`@` 或控制空白开头的值；即使 CSV 字段被双引号包裹，部分表格软件仍可能把它解释为公式。错误报告通常会被管理员下载并用 Excel 等软件打开，因此这是运营端输入输出边界，而不是普通显示问题。

## 实现

- `apps/ops-web/lib/work-csv-errors.mjs` 负责生成错误报告，`.d.mts` 提供严格 TypeScript 合同。
- 所有单元格在 CSV 引号转义前检查前置 U+0000～U+0020 空白/控制字符和 `= + - @`；命中时添加单引号文本前缀。
- 输出保留 UTF-8 BOM、CRLF、完整双引号转义、文件级错误、原始行号、番号、字段、错误码和消息。
- 通过行不进入错误报告；导出不自动打开文件、不访问其中 URL、不修改或提交原 CSV。
- Object URL 在点击后的下一个任务释放，避免下载事件尚未消费就提前撤销。

## 验证

```text
npm run test:frontend:smoke
npm run typecheck --workspace @self-deepsearch/ops-web
npm run lint --workspace @self-deepsearch/ops-web
npm run build --workspace @self-deepsearch/ops-web
node scripts/run_python.mjs -m pytest infra/security/tests/test_release_a_product_contract.py -q
```

定向结果：

- 前端单元 40/40，其中新增 2 项覆盖六种危险前缀、正常番号、BOM、逗号、双引号、文件级错误和通过行排除；
- ops TypeScript、ESLint、production build 通过；
- 产品安全合同 46 passed、7 subtests passed；
- Playwright 新增真实页面链路，desktop/mobile 2/2 通过：登录后台、上传含公式番号的 CSV、接收预检错误、触发浏览器下载并读取文件，确认危险番号被中和、BOM/CRLF/双引号保留且通过行不进入报告；
- 没有新增 API、数据库迁移、服务或外部依赖。

随后执行默认完整统一预检，结果仍为退出 0：Python 298 passed、296 subtests passed、1 skipped，工具合同 62/62，前端单元 40/40，两站 production build、standalone HTTP 与 9 项真实 Go→Next 缓存合同全部通过；完整 Playwright 为 78/78。文档引用更新后的 Markdown 门禁为 62 份文件、392 个本地链接、0 缺失。

本轮没有启动 Docker 或 PostgreSQL。真实 CSV 批次的事务提交、幂等重放和约束回滚仍按目标环境门槛执行；本证据只关闭错误文件的客户端公式注入风险。
