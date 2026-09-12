# Compose 环境

- 根目录 `compose.yaml`：本地开发，只暴露到 `127.0.0.1`；
- `japan/compose.yaml`：日本生产模板，PostgreSQL、Go API、日本 Worker、公开站和运营后台；公开站/后台只 `expose` 给同一 Compose 网络；可选 `edge` profile 使用仓库内 Nginx 模板提供 TLS 外部入口；
- `beijing/compose.yaml`：北京生产模板，北京 Worker 通过 HTTPS/HMAC 批量提交，不持有数据库凭证。

生产模板要求部署系统传入不可变镜像标签和服务器本地 `.env`。日本模板可从 `japan/.env.example` 复制变量清单后逐项替换；示例值不能直接用于部署。模板不会启用自动采集；`COLLECTION_ENABLED=false` 必须保持到 Release B 单独验收完成。

仓库根目录 `docker-bake.hcl` 是 Release A 的统一镜像入口，覆盖 platform-api、platform-worker、display-web、ops-web、media-python。构建时必须显式设置 `BUILD_VERSION`、`BUILD_REVISION` 和 registry `IMAGE_PREFIX`；Go 二进制会内置版本兜底，全部镜像写入 OCI version/revision 标签。CI 在 Go、Python、迁移、前端和 Compose 门槛全部通过后构建 5 个镜像，验证标签并上传 14 天保留的 image inspect 清单，但不会自动登录或推送任何 registry。

```sh
BUILD_VERSION='2026.09.09-a1' \
BUILD_REVISION='<git-commit-sha>' \
IMAGE_PREFIX='registry.example.com/self-deepsearch' \
docker buildx bake release-a --load
```

构建网络无法访问 Go 官方代理时，可显式附加 `--set 'platform-*.args.GOPROXY=https://goproxy.cn'`；Python 依赖源可用 `--set 'media-python.args.PIP_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/'` 显式覆盖。这些参数只影响构建阶段，Dockerfile 默认仍使用官方源。

只有部署账号已登录私有 registry 时才把 `--load` 改成 `--push`。推送后将生成的五个不可变标签分别写入日本/北京私有 env，并再次执行环境检查。`.dockerignore` 排除所有 env、证书/私钥、开发 metrics token、依赖缓存、测试产物和运行数据；不要在 Dockerfile 中临时 `COPY .env` 绕过该边界。

生产命令使用 `docker compose --env-file <服务器私有 .env> ...` 为 Compose 提供变量插值。该选项不会自动把整份文件注入容器；两套生产模板禁止使用服务级 `env_file`，每个服务只通过 `environment` 接收运行所需的最小变量集合。修改模板时必须同步运行安全合同测试，避免公开前端、北京 Worker 或媒体服务获得无关凭证。

数据库 owner 连接只用于迁移、备份和恢复。完成迁移后，以 `ADMIN_DATABASE_URL` 运行 `../database/provision_runtime_logins.sh`，幂等创建非超级用户 `platform_api_login` 和 `platform_worker_login`。API 与 Worker Compose 服务分别强制使用 `PLATFORM_API_DATABASE_URL` 和 `PLATFORM_WORKER_DATABASE_URL`，两套密码必须不同且至少 16 字符。不得把 `ADMIN_DATABASE_URL` 注入任何对外服务或 Worker。

本地与日本 PostgreSQL 模板记录执行超过 500ms 的语句，但设置 `log_parameter_max_length=0`，不把邮箱、标题、搜索词等绑定参数值写入数据库日志。运行账号还分别设置 API 15 秒、Worker 30 秒 `statement_timeout` 和 5 秒 `lock_timeout`；公开 API handler 使用更短的请求上下文作为首层超时。排查慢语句时使用规范 SQL、角色、数据库和时间关联，不临时开启参数值日志。

日本模板提供一次性 `tools` profile。部署时先执行 `docker compose ... --profile tools run --rm database-migrate`，它持有 advisory lock、校验连续版本并把每个 migration 放在独立事务中；重复运行是 no-op。随后执行 `docker compose ... --profile tools run --rm database-logins`。全新 Release A 测试库若需要合成目录，可临时设置 `ALLOW_SYNTHETIC_FIXTURES=true` 和 `CONFIRM_SYNTHETIC_FIXTURES=release-a-test-only` 后运行 `database-fixtures`；Shell、SQL 和数据范围三层守卫会拒绝误用到真实内容库。之后通过受控终端临时设置 `BOOTSTRAP_OWNER_EMAIL` 和 `BOOTSTRAP_OWNER_PASSWORD`，执行 `docker compose ... --profile tools run --rm owner-bootstrap`；API 镜像内置该静态工具，宿主机不需要 Go 或源码，已有不同活动 owner 时会拒绝。完成后立即取消一次性环境变量，最后才启动 API、Worker、前端和 edge。迁移失败时不要启动新镜像，也不要用 bootstrap 脚本覆盖已有数据库。

发布、隐藏、权利下架和编辑推荐排期变更事件由日本 Worker 调用 `display-web` 的内部缓存失效端点。两端必须配置相同的 `CACHE_HMAC_SECRET`，并且该值必须与认证、采集和监控密钥分离。端点流式限制请求体，校验 HMAC、事件 UUID、载荷和 5 分钟时间窗；Worker 不跟随重定向，只有 HTTP 200 JSON 的 `revalidated=true` 和匹配事件 ID 才允许完成，不能把任意 2xx 当作成功。完成/失败落库还必须匹配原始领取的 `attempt_count`、Worker 和未过期租约；批内执行受剩余租约限制，重领时已耗尽 10 次的事件进入 dead 而不阻塞其他事件。反向代理不得把 `/api/internal/` 路由暴露到公网。Cloudflare 可缓存公开目录 HTML 5 分钟、`/_next/static/` 和经过确认的公开图片；账号、搜索、API 和后台必须 bypass。

租约加固无需新增迁移，但须在上线前通过独立 PostgreSQL Worker 合同，并替换全部日本 outbox 消费者，避免新旧逻辑并跑。允许重试不等于 exactly-once：远端可能已执行但回执丢失，缓存和媒体接收端仍须保持幂等。详见 `docs/RELEASE_A_RUNBOOK.md` 的租约与重试说明。

`edge` profile 分别绑定 `DISPLAY_HOST`、`API_HOST` 和 `OPS_HOST`，使用服务器私有的 Cloudflare Origin Certificate。公开目录只有成功的 GET/HEAD HTML 默认缓存 5 分钟，POST/PUT/PATCH/DELETE、4xx/5xx、账号、搜索、API 和后台响应强制 `no-store`；只给 `/_next/static/` 添加 immutable 长缓存，拒绝公开访问 `/api/internal/`、API `/metrics` 和未知 TLS Host。Nginx 访问日志不记录客户端 IP、URL 路径或查询参数。

CI 在无后端服务的 `nginx -t` 检查中额外加载 `japan/compose.nginx-check.yaml`，为三个 upstream 提供仅检查用的回环地址。该覆层不能用于实际启动 edge；生产始终由 Compose 网络解析真实服务地址。

生产源站防火墙必须只允许 Cloudflare 公布的出口网段访问 80/443；只有满足该前提，`CF-Connecting-IP` 才能作为真实客户端 IP 传给 API。`OPS_HOST` 在测试期可以登录访问，正式开放前必须再由 Cloudflare Access、VPN 或固定 IP 白名单限制，不能只依赖后台账号密码。TLS 证书和私钥文件只保存在服务器，不能提交到仓库。

`../backup/` 提供测试期每周 PostgreSQL 自定义格式备份、北京副本校验、复制证据登记和空库恢复校验脚本。生成后状态只是 `completed`；北京侧不持有数据库凭据，只输出哈希/字节证据；日本登记副本并完成空库恢复后才会写 `verified`。Schema v8 在数据库层禁止缺少文件、复制或恢复证据的 verified 状态；当前整体 Schema 版本为 v21。迁移 13 增加单次、48 小时有效的管理员邀请记录，迁移 14-16 分别增加 revision 来源证据、邮件额度/熔断和搜索小时聚合，验证码只存哈希。迁移 17 收紧公开图片所属资料的发布边界；迁移 18 新增每日图片检查审计，部署须启用 MEDIA_INSPECT_ENABLED 并配置固定展示站 origin。迁移 19 增加账户用量持久观察与审计；仅日本 Worker 预留 Analytics 配置，`MEDIA_USAGE_MONITOR_MODE=off` 为默认，不查询供应商也不自动停图。迁移 20 增加管理员复核请求、Worker 账期切换与不可变回执；后台处理结果不等于恢复图片，详见[复核升级](../../docs/MEDIA_USAGE_REVIEWS.md)。启用条件与升级见[持久观察](../../docs/MEDIA_USAGE_STATE.md)。

日本 Worker 的 `history-archives` 卷保存已清除浏览历史的 gzip JSONL 归档。生产示例默认关闭归档；只有该卷已纳入跨区备份、恢复和 manifest 对账后，才设置 `HISTORY_ARCHIVE_ENABLED=true`。归档不会删除数据库原行。

根 Compose 和日本模板都提供可选的 `monitoring` profile。Prometheus 只绑定 `127.0.0.1:9090`，保留 7 天并限制到 512 MiB；Alertmanager 只绑定 `127.0.0.1:9093`，按 critical 与 warning/info 分流通知，并固定以 UID/GID `65534:65534` 非 root 运行。最小化 node-exporter 只绑定 `127.0.0.1:9100`，仅启用 CPU、meminfo、filesystem collector，以 UID/GID `65534:65534`、只读宿主挂载、`cap_drop: ALL` 和 64 pids 运行；禁止改成特权容器。日本服务器必须设置 `METRICS_TOKEN_FILE`、`ALERTMANAGER_CONFIG_FILE` 和 `ALERTMANAGER_SMTP_PASSWORD_FILE` 为私有文件绝对路径；Alertmanager 两个文件应通过受限组或 ACL 允许该容器 UID 只读，不能 world-readable。模板只引用密码文件，不允许把 SMTP 密码直接写进配置或仓库；启用前必须替换 `.invalid` 收件人与 SMTP 主机并完成实际送达测试。生产 metrics 令牌不得使用仓库中的开发示例。

API 请求耗时使用 10ms～5s 的固定桶 histogram，包含 300ms/500ms 产品门槛桶；标签只使用受控 method、chi 规范 route 与 status class。公开目录与番号搜索 P95 告警分别要求 10 分钟内至少 20/10 次成功请求，并持续超标 10 分钟后才触发 warning。测试期低于该流量门槛时必须使用发布探针主动验收，不能把“没有告警”解释成性能已经通过。

API 从 pgxpool 内存统计导出连接池 acquired/idle/total/max、利用率和 acquire 计数，不产生监控 SQL；利用率达到 80% 持续 10 分钟触发 warning。API 默认最大 10 个连接，出现告警时先检查 P95、5xx、无空闲连接获取/取消计数和 500ms 慢语句日志，不应在 2C4G 上无依据扩大池。

主机告警覆盖 node-exporter 不可用、根分区 80% warning/90% critical、内存 80% 和 CPU 85% 持续高压。根分区告警只匹配 `mountpoint="/"`，上线时必须在目标 Linux 主机确认该标签存在；如果系统数据盘另挂载，需要显式增加受控 mountpoint 规则，不能用无边界的任意挂载聚合替代。

日本模板按 2C4G 主机设置服务上限：常驻服务（含 edge、不含监控）合计最多 1.85 CPU、约 2.34 GiB；同时启用 Prometheus、Alertmanager 和 node-exporter 后仍限制在 2.0 CPU、约 2.9 GiB，剩余内存留给操作系统、Docker 和页缓存。所有服务使用 `json-file`，单文件 10 MiB、保留 3 份。上线后按真实 RSS、数据库缓存命中率和延迟调整，不能直接取消上限。

北京模板按 2C8G 主机设置独立上限：Go Worker 为 0.20 CPU/192 MiB/128 pids，Python 图片服务为 1.25 CPU/2 GiB/256 pids，常驻合计最多 1.45 CPU、约 2.19 GiB。剩余资源留给操作系统、Docker、SFTP/备份及人工图片处理峰值；两项服务同样使用 10 MiB × 3 的 `json-file` 轮转。40MP 图片上限不代表 2 GiB 一定足够，目标 Linux 仍须用最大允许样本和并发删除/对账实测 RSS；不得为绕过 OOM 直接取消限制。

日本 Worker 新增 `MEDIA_RECONCILE_USAGE_GUARD=off|enforce`，默认 off。Enforce 依赖 observe 和启用对账，只控制定时全量扫描的发起，不停止必要删除或图片展示；无需新迁移/服务。更新全部日本 Worker 和监控后，按[准入手册](../../docs/MEDIA_TASK_ADMISSION.md)验证，再决定是否显式开启。回退会移除该准入，须先暂停自动对账调度，不能只将 off 当作安全恢复。该准入功能最初提交时未运行 Docker 验收；2026-09-12 已在 Ubuntu 完成三套 Compose 校验、实际 `nginx -t`、五个 Release A 镜像构建与非 root 健康检查，并验证两站同源读接口连通真实 API 和 PostgreSQL，详见[本地 Docker 验收证据](../../docs/evidence/release-a-docker-ubuntu-2026-09-12.json)。验收镜像来自修复后的未提交工作树，未推送 registry，也不代表跨区生产部署已验收。
