# Release A：对象存储任务清单复核

日期：2026-09-11（Asia/Shanghai）  
结论：Release A 当前所有对象存储数据面入口已分类。唯一非必要自动后台任务是定时全量图片对账，已有调度前和跨区派发前双重用量准入；北京受控上传已有逐操作准入；权利删除明确不受非必要闸门阻断；公开读取由默认关闭的私有 R2 网关策略降级。此前文档中的“仍需补其他非必要作业限流”不再作为当前代码缺口。

## 新增合同

- [`infra/policy/media-storage-tasks.json`](../../infra/policy/media-storage-tasks.json)：机器可读任务、操作、必要性、默认开关、控制和失败行为；
- [`test_media_task_inventory_contract.py`](../../infra/security/tests/test_media_task_inventory_contract.py)：检查四类任务、直接 SDK 客户端、源码入口和双阶段/逐操作保护；
- [`MEDIA_STORAGE_TASK_POLICY.md`](../MEDIA_STORAGE_TASK_POLICY.md)：架构决策、扩展规则、失败和回退边界。

静态扫描当前只发现两个直接对象存储客户端：北京 Python `storage.py` 与 Cloudflare 媒体网关。每日默认图检查、历史归档、邮件、目录缓存刷新和 Analytics 观察均不访问对象数据面，已在清单中明确排除。

## 验证

```text
python -m pytest -q infra/security/tests/test_media_task_inventory_contract.py
ruff check infra/security/tests/test_media_task_inventory_contract.py
npm run preflight:release-a
```

结果：定向合同 4/4、Ruff 通过；后续北京资源合同、文档链接门禁和公开账号 CSP 加固加入后，当前默认统一预检完整通过，Python 全量 295 passed、288 subtests passed、1 skipped，工具合同 62/62、前端单元 38/38、两站 production build、standalone HTTP 和真实 Go→Next 缓存合同均通过。新增部署与资源证据后，检查 59 份 Markdown 的 376 个本地链接，无缺失。默认预检按设计没有执行 Docker Compose、Playwright 或性能探针；浏览器与性能同日已记录为 76/76 和本地门槛通过。

本地合同不能替代真实 PostgreSQL、R2/S3、Cloudflare、Linux 共享锁、跨区网络或供应商账单验收。存储类别、版本历史、共享应用完整性和真实水位仍是 Release A/M5 的外部门禁；公网继续 no-go。
