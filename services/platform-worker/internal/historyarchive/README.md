# 历史归档与只读恢复校验

归档仅选择已伪删除、达到年龄门槛、尚未设置 `archived_at` 的浏览历史。gzip JSONL 保留 history ID、用户/内容 UUID 及精确时间；归档不会物理删除原行，也不会恢复前端可见性。文件和目录分别以 0600、0700 创建。

归档文件通过同目录硬链接原子安装，不覆盖已有文件。已有同名文件必须是普通文件且 SHA-256 相同，才可复用；损坏文件会被拒绝并保留，不能自动覆盖旧证据。归档文件系统必须支持同目录硬链接，不支持时任务失败，不降级为覆盖式重命名。

数据库写入 manifest 之前，先同步归档文件，再在完成最终链接安装和临时链接移除后，按叶目录至文件系统根的有限祖先链逐级同步目录；复用既有文件时也同步实际复用的 inode。每次重试都重复目录同步，不能把目录“已存在”当作上次已经落盘。任一同步失败都不提交 manifest 或 `archived_at`；底层文件系统必须支持文件和目录同步。

文件系统与数据库不是同一事务：数据库提交失败时，`archived_at` 和 manifest 都回滚，已安装文件可保留为孤立文件。重试会验证并复用同一文件；单独看到文件存在不等于归档成功，必须以已提交 manifest 对账。不要扫描目录后批量删除所谓孤立文件。

## 离线只读校验

先从受信任数据库导出某一个 manifest，私下传输它与归档目录副本。manifest 包含用户 UUID，必须与归档同等保护；它的 SHA-256 不是数字签名，不应把不可信来源的 manifest 当作完整性依据。

下例在仓库根目录执行。连接变量只需对 `audit.archived_history_manifests` 有 SELECT 权限，不要求数据库 owner；明确指定要校验的单个归档路径，不导出全部用户归档：

```sh
umask 077
psql "$HISTORY_ARCHIVE_READ_DATABASE_URL" -X -v ON_ERROR_STOP=1 -At \
  -v archive_path='history/2026/09/<archive-filename>.jsonl.gz' \
  > /private/offline-copy/manifest.json <<'SQL'
SELECT row_to_json(manifest)
FROM (
  SELECT storage_path, schema_version, user_scope, range_start, range_end,
         record_count, byte_size, sha256
  FROM audit.archived_history_manifests
  WHERE storage_path = :'archive_path'
) AS manifest;
SQL

go run ./services/platform-worker/cmd/worker history-archive-verify \
  --archive-dir /private/offline-copy/archives \
  --manifest /private/offline-copy/manifest.json
```

编译后的同一 Worker 可执行文件支持相同子命令。此分支在加载 Worker 配置之前执行，不连接数据库、不启动调度器、不写恢复记录。应对没有不可信并发写入者的离线副本运行；归档路径必须是目录内的规范相对路径，嵌套符号链接会被拒绝。

校验覆盖 schema v1、压缩文件 SHA-256/字节数、gzip 完整性、单一 gzip member、JSONL 字段/条数/顺序、唯一 history ID、用户 scope、精确时间范围。输入最多 10000 条，压缩及解压内容各不超过 64 MiB；超限失败，不静默截断。成功仅输出 `status=verified`、条数、字节数和 SHA 等摘要，不输出用户、内容或本地路径；失败返回非零退出码和脱敏错误码。

`verified` 表示当前文件与提供的 manifest 一致，不表示跨区副本、灾难恢复或生产保留策略已验收。Release A 不提供将已清除历史重新显示或自动写回正式表的命令；任何生产物理清理和恢复操作仍须单独批准。

## 真实 PostgreSQL 合同

需要 schema v21、无开发种子、无用户/作品/历史/媒体/outbox 的一次性 `self_deepsearch_worker_test` 数据库。两个 URL 必须使用同一个 `127.0.0.1` 显式端口；仅接受可选的 `sslmode=disable` 查询参数。运行连接必须是非超级用户 `platform_worker_login`，不能读取用户表或删除历史/修改 manifest。环境未设置时合同跳过；只设置一部分或缺少确认值会失败。

```sh
HISTORY_ARCHIVE_CONTRACT_ADMIN_DATABASE_URL='<dedicated-admin-url>' \
HISTORY_ARCHIVE_CONTRACT_DATABASE_URL='<dedicated-worker-url>' \
CONFIRM_HISTORY_ARCHIVE_CONTRACT=disposable-database \
go test ./services/platform-worker/internal/historyarchive \
  -run '^TestPostgresHistoryArchiveContract$' -count=1 -v
```

与其他 Worker 合同串行执行，共用 advisory guard。合同验证真实行锁跳过、分批/门槛/重跑、manifest 与文件对账、微秒时间及 UUID、原行保留和伪删除不变、写盘/叶目录同步/父目录同步失败回滚，以及 deferred COMMIT 失败后的回滚与孤立文件复用。恢复练习只向事务内 TEMP 表写入已验证记录，再与保留原行逐字段对账，始终保持 `is_deleted=true`。目录同步顺序和故障注入不等同于实际断电实验。

测试结束会精准清理其合成用户/历史及临时文件、TEMP 表和故障触发器；追加写 manifest 保留在一次性数据库中，交验收环境整体销毁，不绕过审计不可变保护。
