# 图片上传容量保护与中断恢复

更新：2026-09-11。适用 Release A 北京受控图片处理器；当前整体 Schema v21；上传保护本身没有新增迁移。此文描述已经实现的上传保护，**不代表账户免费额度已全部受控**。

已另行增加 Go 的[一次性观察命令](./MEDIA_USAGE_OBSERVATION.md)与[持久观察/可选定时器](./MEDIA_USAGE_STATE.md)；后者现有默认 off 的[逐操作上传准入](./MEDIA_UPLOAD_ADMISSION.md)消费者。它依据操作量风险独立阻断上传/写入系统暂停，不改变本文的字节预算、不计 GB-month，也不能把分析数据当账单。默认 10 GiB、本地共享锁和以下人工恢复规则保持不变。

## 已实现的范围

现已新增[手动跨区上传开关](./MEDIA_UPLOAD_CONTROL.md)：默认 off，Go→Python 显式下发并核验双向签名回执；北京共享卷持久状态控制准入、每页 Listing 和每次 PUT。[后台授权队列/页面](./MEDIA_UPLOAD_QUEUE.md)和逐操作自动暂停均已开发。它们不清除本文的 pending/复核日志，`acknowledge-upload` 也不解除该暂停；必要校验与权利删除继续可用，不改变以下容量计量边界。

所有上传必须在同一北京宿主机，使用同一私有持久卷和相同 `MEDIA_UPLOAD_LOCK_FILE`。处理器先取得非阻塞操作系统文件锁，再解码图片、生成四个对象、读取用量、写北京副本、上传、HEAD 校验和持久化输出 manifest；完成后才释放保护。并发命令返回 `MEDIA_UPLOAD_BUSY`，由操作者稍后重试。状态文件不是 HTTP 接口，不在公开目录。

每次容量核算列出两个专用 bucket 的**全部当前对象和未完成 multipart 已上传部件**，包括 `media-master/`、`media-public/` 以外的占用。对象页、multipart upload 页和 part 页均最多 1000 条并设页数上限；整次双桶扫描还共享最多 256 个列表请求预算。页结构、字节数、完成标记、计数、重复对象/upload/part、分页令牌或竞态异常时拒绝上传，不把部分结果当成总量。Listing 权限必须覆盖完整两桶，并包含 `ListMultipartUploads`/`ListParts`；对象 PUT/DELETE 仍限各自管理前缀，不扩大对象写权限。

`MEDIA_STORAGE_LIMIT_BYTES` 默认 `10737418240`（10 GiB）。当前对象与未完成 multipart 部件合计，再加本批四个对象预计超限时，在写任何北京副本或 PUT 前整批拒绝。已隐藏但未物理删除的旧对象、未登记孤儿和未中止的分段也占额度。SDK PUT 使用 `IfNoneMatch=*`，不会覆盖已有同键对象。

该计算仍不涵盖其他 bucket、供应商版本历史、账户中其他应用、GB-month 日峰值平均、A/B 类月操作次数或北京磁盘容量。用户提供的套餐“10 GB”不自动等于代码默认 10 GiB；上线前按实际供应商计费单位和账户共享占用调整，并预留安全余量。此护栏不承诺零费用。

## 未确认批次

| 状态 | 行为 |
| --- | --- |
| 无上传进行、无 pending | 下一批可取得锁，但仍重新检查容量 |
| 另一进程持有锁 | 拒绝并发上传/复核，返回 `MEDIA_UPLOAD_BUSY` |
| 曾尝试 PUT，但批次未完整确认 | 保留 pending，返回 `MEDIA_UPLOAD_RECOVERY_REQUIRED`，禁止新批次 |
| pending 损坏/超长或状态目录不可写 | 拒绝放行，需要人工恢复，不擅自删除记录 |

每次 SDK PUT 前原子记录批次 UUID、UTC 时间和已尝试对象的 key/字节/hash。成功执行所有 HEAD 并持久化 CLI manifest 后移除 pending；进程异常、PUT 超时、HEAD 失败、manifest 写入失败等均保留它。进程被终止时系统会释放文件锁，但 pending 仍会阻止后续上传。

回滚只删除已收到 PUT 成功回执的本批对象，DELETE 后还需 HEAD 确认不存在，才删对应北京副本。未知 PUT 结果、删除失败保留副本；同键冲突不删除已有远端对象。即使回滚看起来成功，仍不自动清除 pending：一次 HEAD 404 无法证明超时 PUT 之后不会完成。manifest 写入失败时保留整批对象和副本以便核对，不重复上传掩盖交接失败。

删除、权利下架和只读对账不需要上传准入，不因 pending 被阻断。它们仍会产生供应商请求，不能把此例外当成免费操作。

## 部署要求

根级与北京 Compose 的 `media-python` 都挂载 `media-state:/var/lib/self-deepsearch/media-state`，固定环境值为：

```text
MEDIA_UPLOAD_LOCK_FILE=/var/lib/self-deepsearch/media-state/upload.lock
```

完整环境校验器要求该值，避免误配到容器临时目录或另一个锁文件。所有 `compose run`、维护命令、手动/将来自动脚本必须使用**同一 Compose 项目、宿主机、卷、文件路径**。不得改项目名另起一套上传，也不得从日本、控制台或其他脚本绕过准入写入这两个专用桶；双区域采集规划不等于双区域直接上传。此实现不是跨主机分布式锁。

镜像创建 UID 10001 的私有 `media` 和 `media-state` 目录，新空命名卷可继承目录所有者。已有卷和 bind mount 不会因升级镜像自动变更所有者；部署者必须先停用上传、核对真实卷路径并备份，确保这些专用目录由该 UID 可写且无其他写入者。不可用 `chmod 777` 或删除锁文件解决权限问题。没有启动容器验证新卷初始化或 Linux 文件系统语义，仍需目标环境验证。

升级时先停止全部旧上传器，排查旧版未确认 PUT，核对两桶及备份，再挂载共享状态卷并替换上传镜像；旧版不能与新版混跑。直接回退旧版会失去准入保护，必要回退期间必须保持上传关闭。不要使用 `compose down -v` 清理运行状态。状态卷、manifest 和复核日志一起纳入北京私有备份，不上传到公开图片桶。

## 人工核对与恢复

以下命令供后续部署执行，本轮没有运行 Docker 或访问真实 S3。使用与上传完全相同的环境和持久卷：

```bash
python -m media_worker.cli upload-status \
  --upload-lock-file /var/lib/self-deepsearch/media-state/upload.lock
```

返回 `review_required` 时：

1. 暂停所有上传入口，确认旧进程、SDK 重试和远端在途写入已终止/稳定；不能仅等待几秒或一次 HEAD 就当作确认。
2. 保存原 pending、已有 manifest、北京副本及运行记录；按 scope/key/字节/hash 核对两桶当前对象和对应副本。处理冲突、孤儿、缺失和不完整交接，不覆盖已有同键对象。manifest 缺失不代表远端没图；先恢复证据和决定保留/清理，再做新批次。
3. 确认无新写入且存储已核对后，使用刚读取的精确批次 UUID 和明确复核原因执行：

```bash
python -m media_worker.cli acknowledge-upload \
  --upload-lock-file /var/lib/self-deepsearch/media-state/upload.lock \
  --run-id <pending-run-id> \
  --reason "Stopped old uploader; reconciled both buckets and matching backups" \
  --confirm-storage-reconciled
```

这只是**人工复核确认**，命令不访问、不核验、不修改 S3。必须显式确认、提供 2–1000 字原因，且 run-id 精确匹配；旧请求不能清除新批次，活跃上传时不能确认。它先持久化 `<lock>.reviews.jsonl` 再移除 pending，返回 `manual_review_acknowledged` 和需要重新核算容量的标志，不返回“存储验证成功”。

复核日志到 1 MiB 后拒绝继续追加，操作者须在停用上传/维护命令时保存并校验私有归档后再轮转，不能直接丢弃证据。损坏 pending 不提供一键删除：先保留原文件并线下重建证据。永久 `upload.lock` 文件不能删除或替换；重建锁必须在确认所有相关进程已停止的完整维护流程中进行。

## 仍需开发，不是只差凭据

- A 类 100 万/B 类 1000 万的月度额度监测与执行、统计新鲜度/不可用策略、共享账户占用和重试开销尚未形成账单硬上限。全桶和 multipart Listing 本身也消耗 A 类次数；本轮以单次 256 请求预算阻止无限放大，但每批全量扫描仍不适合无限增长的图库。
- 70/85/95% 的现有监控提示不代表供应商硬限额。Release A 当前唯一非必要自动对象任务是全量对账，已接双阶段用量准入；受控上传另有逐操作准入。未来新增对象任务必须先登记到[任务策略](./MEDIA_STORAGE_TASK_POLICY.md)，不能复用提示值冒充账单保护。
- 手动 [MEDIA_DELIVERY_MODE 主动降级](./MEDIA_DELIVERY_MODE.md)已实现：停止新准入、公开/用户目录去图、新渲染去图、旧 Next HTML 的 CSP 和默认图。默认 off 的[Cloudflare 私有 R2 网关](./MEDIA_EDGE_GATEWAY.md)也已开发，在对象缓存前执行同一策略；真实 bucket 私有化/route/binding/purge 尚未验收，已打开标签页中已下载的字节也无法撤回。账户额度仍未形成可信账单硬限制。
- 后续需结合实际供应商的计量延迟、月周期和图片入口控制实现完整降级，不能用数据库登记字节或后台请求次数冒充账户真实账单。接近阈值应预留额度，保留删除/下架所需请求预算。

真实双桶对象与 multipart 只读权限、一致性、上传完成/中止竞态、SDK 重试、Linux 锁/卷权限及中断恢复仍待目标环境验收。M5、Release A 与公网放行不因本地测试通过而完成。原容量实现证据见[历史验证](./evidence/release-a-media-upload-capacity-2026-09-11.md)，multipart 补充见[本轮验证](./evidence/release-a-media-multipart-capacity-2026-09-11.md)。
