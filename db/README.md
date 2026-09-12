# 数据库

MVP 的唯一 PostgreSQL 位于日本。业务对象不得创建在 `public` Schema：

- `collector`：采集事实、候选、任务和审核前数据；
- `platform`：已审核/发布目录、账号与用户能力；
- `audit`：安全、权利、归档和运维证据。

运行服务不能使用数据库 owner。迁移创建 `platform_api`、`platform_worker` 等 NOLOGIN 权限组；`infra/database/provision_runtime_logins.sh` 使用部署环境密码幂等创建 `platform_api_login` 和 `platform_worker_login`，分别继承对应权限组。owner 连接只用于迁移、备份、恢复和运行账号配置。

`bootstrap/` 只用于第一次创建本地开发容器，并且每个迁移都在独立事务中执行；生产升级使用 `migrations/`。当前 `/readyz` 使用 SQL Ping，应用查询由 `pgx` 执行，数据库角色和核心领域表已进入迁移。

本地容器首次创建后会执行 `seeds/development.sql`，写入仅供 Release A 联调的 30 个人物、100 部作品及审核、发布、搜索和来源记录。种子不访问网络、不包含外部图片、不代表真实人物或作品，也不会在生产迁移中执行。文件必须通过 psql 显式设置 `release_a_fixture=1` 才能运行，并要求当前 Schema v21；其他值会被拒绝，两个固定审计账号处于关闭状态且不可登录。

全新日本测试库需要同一目录时，使用 `infra/database/load_release_a_fixtures.sh` 或 Japan Compose 的 `database-fixtures` tools 服务。外层必须同时设置 `ALLOW_SYNTHETIC_FIXTURES=true`、`CONFIRM_SYNTHETIC_FIXTURES=release-a-test-only`，内层 SQL 还会拒绝存在非 fixture 用户或目录数据的数据库。加载器忽略用户 `psqlrc`，并在单事务中短时锁住会写入的 13 张表；应在常驻服务启动前执行。加载完成或创建真实 owner 后立即恢复关闭状态；生产迁移永远不会自动调用该工具。

迁移的本地静态校验：

```sh
python -m unittest discover -s db/tests -p "test_*.py"
```

有 PostgreSQL 16 可用时，可通过 `DATABASE_URL` 执行全量安装和逆序回滚验证：

```sh
DATABASE_URL=postgres://platform:password@127.0.0.1:5432/self_deepsearch_test sh db/tests/postgres_roundtrip.sh
```

已安装迁移和开发种子的数据库可单独执行 Release A 数据库合同验收。脚本在事务中验证角色隔离、公开番号搜索、审计追加写、媒体双桶约束和媒体对账状态机，结束时回滚测试数据：

```sh
psql "$DATABASE_URL" --file db/tests/postgres_contracts.sql
```

生产部署迁移完成后配置运行账号：

```sh
ADMIN_DATABASE_URL="$ADMIN_DATABASE_URL" \
PLATFORM_API_DB_PASSWORD="$PLATFORM_API_DB_PASSWORD" \
PLATFORM_WORKER_DB_PASSWORD="$PLATFORM_WORKER_DB_PASSWORD" \
sh infra/database/provision_runtime_logins.sh
```

本地重建数据库会删除开发卷，不能用于有价值的数据。生产禁止把 `.env.example` 中的本地密码直接使用。
