# Release A 未完成 multipart 容量保护验证

日期：2026-09-11（Asia/Shanghai）

## 结论

北京媒体处理器的上传前容量核算已从“两个专用桶的当前对象”扩展为“当前对象 + 未完成 multipart 的已上传部件”。每次 `prepare` 仍在同一单写锁内、任何北京副本或 S3 PUT 之前完成核算；缺权限、分页异常、重复证据、竞态、整数溢出或请求预算耗尽均 fail-closed，不把部分结果作为容量总量。

本轮只修改 Python 媒体处理器、测试、静态安全合同与文档；没有启动 Docker、访问真实 S3/R2、创建或中止 multipart、修改 bucket、连接 PostgreSQL/SMTP/Cloudflare/Analytics。离线代码可进入隔离测试，结论为 **conditional-go**；Release A/M5 与公网仍为 **no-go**。

## 实现边界

- 对私有母版桶和公开派生桶分别执行完整 `ListObjectsV2`，继续统计非媒体前缀、孤儿和旧 key。
- 对每个桶执行有界 `ListMultipartUploads`；每个未完成上传再以有界 `ListParts` 汇总已经提交的 part 字节。
- 对象 key、upload ID、part number、size、完成标记和 next marker 均严格校验；跨页重复对象、重复 upload/part、分页循环和矛盾 marker 直接拒绝。
- 单项和总量限制为有符号 64 位非负范围，避免异常响应制造整数溢出或不合理容量结果。
- 一次容量核算最多允许 256 次对象/multipart 列表请求。预算在整个双桶扫描中共享，耗尽时在发出下一次请求前拒绝，避免保护逻辑自身无限消耗 A 类操作。
- 每一次对象页、multipart 页和 part 页请求前仍重新读取现有上传控制/逐操作准入；状态变化可阻止后续请求。
- 只读取和计数，不自动 `CompleteMultipartUpload` 或 `AbortMultipartUpload`。自动中止可能破坏其他受控恢复任务，必须由人工核对或独立生命周期策略处理。

## 验证结果

```text
容量/上传控制/准入/静态合同专项: 53 passed, 101 subtests passed in 10.52s
media-python 全量: 77 passed, 118 subtests passed, 1 skipped in 16.85s
media-python mypy: 9 source files, no issues
受影响文件 Ruff: passed
仓库 Python 全量: 288 passed, 288 subtests passed, 1 skipped in 81.50s
仓库 Ruff: 全范围通过
Markdown: 50 files, 331 local links, 0 missing
```

专项覆盖正常双桶、稀疏对象分页、multipart/part 多页累计、跨页重复对象、重复 marker、provider ClientError/NoSuchUpload、结构/类型/大小异常、64 位溢出、全局 256 请求预算，以及上传控制在对象、multipart 和 part 请求之间切换后拒绝继续。唯一 skip 是当前 Windows 账号不能创建 symlink 的既有上传防护场景，不是断言失败。

## 真实环境仍需验收

1. 为北京媒体处理凭据增加两个专用桶的 `ListMultipartUploads` 和 `ListParts` 只读权限；缺少任一权限会安全拒绝新上传。
2. 在真实 R2/S3 创建隔离 multipart，上传若干部件但不完成，确认容量总数包含部件；随后中止并确认容量回收。
3. 验证分页、上传完成/中止竞态、SDK 超时、逐操作暂停以及 256 请求预算在目标 Linux 共享卷和真实网络下的表现。
4. 核对新增 `ListMultipartUploads`/`ListParts` A 类请求量；图库增长后不能无限按每批全量扫描，应转为可信快照或供应商账单/库存能力。
5. 当前仍不包含其他 bucket、共享账户其他应用、供应商版本历史和 GB-month 日峰值平均；10 GiB 当前占用保护不等于 10 GB-month 免费账单硬上限。

因此，本轮关闭的是“两专用桶未完成 multipart 部件漏计”这一具体缺口，不应扩大为账户级零费用保证。
