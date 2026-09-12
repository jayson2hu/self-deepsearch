# Release A 备份加固验证记录

日期：2026-09-10（Asia/Shanghai）。本机 Windows / 仓库 `.venv` / Git for Windows POSIX Shell。

## 结果

| 检查 | 实际结果 |
| --- | --- |
| 备份执行级回归 | 14 passed, 7 subtests passed，126.38 秒 |
| 其余 Python 回归 | 139 passed, 54 subtests passed，61.34 秒 |
| 合计（上述两组无重复） | 153 项测试、61 个子测试通过 |
| 同秒重试、固定日期、时钟回拨定点复测 | 3 passed；已经包含在上述 14 项中，不另加总 |
| `ruff check infra/backup db/tests` | All checks passed |
| 四个改动 Shell 脚本 `sh -n` | 退出码 0 |

执行命令：

```powershell
.\.venv\Scripts\python.exe -m pytest -q -p no:cacheprovider infra/backup/tests/test_backup_workflow.py
.\.venv\Scripts\python.exe -m pytest -q -p no:cacheprovider --ignore=infra/backup
.\.venv\Scripts\python.exe -m ruff check infra/backup db/tests
& 'C:/Program Files/Git/bin/sh.exe' -n infra/backup/weekly_backup.sh
& 'C:/Program Files/Git/bin/sh.exe' -n infra/backup/audit_backup.sh
& 'C:/Program Files/Git/bin/sh.exe' -n db/tests/backup_retention_contracts.sh
& 'C:/Program Files/Git/bin/sh.exe' -n db/tests/postgres_roundtrip.sh
```

## 覆盖与修正

- 备份成功/失败审计、默认四份目标、sidecar 成对清理和无关文件保留。
- 非法保留数量在写入前拒绝，同秒名称冲突不覆盖、不再写审计。
- 两个真实并发 Shell 进程：首个 dump 被测试控制阻塞时，第二个返回 75；首个完成后释放锁。
- 最后完整已验证恢复点和未验证文件保留；损坏、缺失 sidecar、与数据库哈希不符时暂缓清理。
- 生成部分计划后数据库查询失败，旧文件全部保留；completed 审计失败不轮转旧备份。
- 时钟回拨保留新 dump，并正确上报额外保护份数。
- 首轮发现同秒测试未固定日期：Git for Windows 在启动时将 `/usr/bin` 放到外部 PATH 前面。测试现从 Shell 内恢复 mock 优先级，并断言精确文件名；修正后定点与整组回归均通过。
- PostgreSQL roundtrip 新增备份审计执行合同，覆盖状态转换、复制/恢复摘要、路径/哈希/字节精确匹配和区域/类型边界；离线合同确认已接入 CI 调用路径。

## 证据边界

备份用例在临时目录中执行真实 Shell、文件校验和锁操作，但 `pg_dump`、`pg_restore`、`psql` 为受控替身。没有生成真实业务备份、修改真实审计记录或访问北京目录。

新增 PostgreSQL 合同只是已编写、已接入并通过语法/静态检查，尚未在本轮真实数据库执行。没有启动 Docker、连接 PostgreSQL/SMTP/S3/Cloudflare，也没有启用采集或广告。Go、前端构建、浏览器和性能套件未受改动，沿用同日已有证据。

发布结论不变：本地代码可进入真实测试环境；目标环境联调与公网开放仍为 no-go。
