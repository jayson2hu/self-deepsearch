# Release A Ubuntu 验收记录（2026-09-12）

日期：2026-09-12（Asia/Shanghai）
状态：Ubuntu 本地隔离验收 passed；完整 Release A / M5 与公网仍为 no-go。
范围：隔离合成资料测试；未部署目标两机、未连接真实 R2/Cloudflare 或向公网开放。

## 环境与隔离

- Ubuntu 22.04.3 LTS、x86_64；Docker 29.1.3。
- PostgreSQL 服务端 16.15，隔离容器；迁移/仓储、核心 E2E、恢复分别使用专用可丢弃数据库。
- API 合同使用 platform_api_login，Worker 合同使用 platform_worker_login；迁移/建库独立使用管理员连接。
- Mailpit 捕获本地测试邮件；账号、密码、作品和事件均为合成数据，不发送真实用户邮件。
- Go 1.22.2；Node.js 22.22.2 / npm 10.9.7；Python 3.12.13 的项目隔离 .venv。
- Python 测试依赖按 requirements-test.txt 安装；官方源下载缓慢后使用公开 PyPI 镜像，依赖声明未改变。
- 原始日志和测试配置曾位于 .cache/acceptance，汇总后已清理；连接串、密码、运行配置和备份文件不写入本记录。

宿主默认 psql 客户端版本不作为服务端版本证据；备份需使用与 PostgreSQL 16 服务兼容的 pg_dump/pg_restore。
本次已解除旧的 PostgreSQL / Mailpit 环境阻塞，历史文档中的未执行状态不代表当前结果。

## 已确认结果

| 验收 | 结果 | 适用范围 |
| --- | --- | --- |
| 21 个迁移 up/down、权限和增强 seed 负向 | 最终 passed，Schema v21 | 独立 PostgreSQL 16.15；拒绝无效确认及已存在手动来源数据 |
| API 仓储整包 | 59 顶层 + 109 子测试，0 fail / 0 skip | 受限运行账号、真实事务和 SQL |
| Worker 仓储 | 7 顶层 + 16 子场景，0 fail / 0 skip | outbox、对账、巡检、用量、上传控制 |
| 核心真实业务 | 8 检查 passed | Go API + PostgreSQL + Mailpit |
| 本地备份恢复 | passed | dump、SHA-256、本地副本、空库恢复与非空目标拒绝 |
| Python 全量 | 300 passed + 303 subtests passed，0 skip | 最终 116.59 秒；含 Linux symlink、回环 HTTP 与进程锁 |
| Ruff / mypy / OpenAPI | passed；mypy 18 源文件 | Nginx 新改动后全量 300 项及 303 子测试再次通过 |
| Go test/vet/race | 最终 passed | race 使用 -p 1 串行包执行，Go→Python HTTP 启用；真实 SQL 另行执行 |
| 前端与工具 | 40 单元、62 工具，类型/lint/两站 build/HTTP 冒烟通过 | 布局修复后公开站重建及 HTTP 冒烟再次通过 |
| Go → Next 缓存 | 9/9 passed | 真实 Go/Next 进程，合成目录 API |
| Compose / Nginx | 根级、日本、北京解析；nginx -t passed | 语法/模板验收 |
| Playwright | 最终完整一轮 82/82；桌面/移动各 41 项，0 失败/跳过/重试/flaky | 375.75 秒，已逐条核对原始结果；合成 API |
| 视觉 / 性能 | 最终 12 图复核、SHA-256、LCP/CLS 与 HTTP P95 均通过 | 本地 production standalone、无网络限速 |
| npm 依赖审计 | 生产与全量依赖均 0 漏洞 | js-yaml 4.3.1 → 4.3.2，锁文件补丁更新 |
| 五个 Docker 运行镜像 | 全部构建、非 root HEALTHCHECK passed；两站缓存实际可写、日志无 EACCES，同源首页/搜索均 200 | Go API + PG 空测试库；不代表目标部署或完整浏览器真后端业务 |
| 最后部署回归 | Docker / Nginx / 资源合同 30/30，Ruff 及三 Compose 解析通过 | 强化既有测试断言，未新增顶层测试；全量 Python 300 + 303 结果不重复累加 |

真实核心检查为：数据库/API ready、注册、密码重置、账号关闭、旧会话撤销、邀请、editor 邀请、
以及创建 → 异人审核 → 发布 → 搜索/站点地图 → 隐藏的业务闭环。
[核心 JSON](./release-a-core-e2e-ubuntu-2026-09-12.json)只保存检查结果、时间和未覆盖范围。
[后端汇总 JSON](./release-a-backend-ubuntu-2026-09-12.json)保留最终计数与执行时间：Go race 为 21 个包、330 顶层和 781 子测试通过；47 个 opt-in 跳过项由真实数据库与缓存专项另测，不能声称这些 SQL 已在 race 模式通过。

备份验证只使用本地复制替代远端传输；这不构成日本到北京 SFTP、目标调度或告警通过证据。

## 发现的问题与修复

| 问题 | 实际触发/影响 | 修复与验证 |
| --- | --- | --- |
| seed 拒绝仍返回成功 | psql 16 忽略 quit 的退出码参数 | 使用 ON_ERROR_STOP 与 SQL 异常；检查 0、true、缺失确认均拒绝 |
| 空库 seed 被误判为真实来源 | 迁移 6 已创建 Manual CSV import 内建来源 | 仅允许精确内建空来源，已有批次/来源记录仍拒绝；保护表一并加锁 |
| API 时间参数推断 | 频率桶、验证码重发、邀请 session、邮件熔断和 SystemHealth 在真实 SQL 失败 | 显式 timestamptz/integer/bigint；真实仓储与核心 E2E 覆盖 |
| API JSON 参数推断 | 发布/隐藏/下架、媒体派生、发现规则和编辑推荐事件失败 | JSON 参数显式 text/uuid/integer；核对结果、审计及 outbox |
| Worker 调度时间参数 | 对账/巡检查询无法推断时间类型 | 显式 timestamptz；真实调度与并发合同通过 |
| 测试夹具身份失效 | 改派合同依赖已关闭的 seed 运营账号 | 使用独立活动合成账号和唯一 request ID |
| Argon2 race 测试等待过短 | 两核主机同时验收时真实 Argon2 超过假工作器的 3 秒等待 | 该真实计算测试使用总计 30 秒上限；生产 1 个计算、2 个等待、500ms 排队参数及并发断言不变 |
| Go 独立镜像构建失败 | API 缺少 x/sys 校验和，Worker 缺少独立模块间接依赖 | 两模块分别 GOWORK=off go mod tidy 与 go build 通过；Docker 支持显式 GOPROXY build arg，默认仍官方源 |
| Python Linux 类型检查 | Windows 专有 CREATE_NO_WINDOW 导致 mypy 错误 | getattr 默认 0；18 源文件类型检查与全量 pytest 通过 |
| 前端路径测试假设 Windows | Linux 路径与硬编码反斜线不匹配 | 使用平台原生 path.join 和临时根路径 |
| 桌面广告文字重叠 | Ubuntu Chromium 中文纵排字形高度为零，侧栏文字堆叠 | 改为横排文字纵向布局；现有浏览器场景检查文本框非零且不重叠，最终 build/视觉/E2E 通过 |
| 浏览器截图覆盖历史证据 | media-delivery 场景硬编码 2026-09-11 输出路径 | 改用 Playwright outputPath；恢复本轮覆盖的历史桌面图，最终 12 张视觉证据使用新日期 |
| Nginx 主机模板未正确替换 | ENVSUBST_FILTER 需要变量名正则 | 仅匹配三个主机变量；CI 使用独立 DNS override 执行 nginx -t |
| Next Docker 健康检查连接失败 | Docker 自动 HOSTNAME 使 standalone 只监听容器主机名，回环 wget 被拒绝 | 两站显式 HOSTNAME=0.0.0.0；健康检查及真实同源读取复测通过 |
| Next Docker 缓存 EACCES | 非 root node 用户不能创建 root 拥有的 .next/cache | 仅预创建并授权缓存目录；实际写入/删除探针通过，日志无权限错误，代码和静态资源权限不变 |
| 测试依赖与临时产物 | js-yaml 4.3.1 存在高危公告、Python 缓存进入未跟踪列表 | js-yaml 更新至 4.3.2，生产与全量 npm 审计均 0 漏洞；忽略 mypy/egg-info |

新增 operational_sql_contract_test.go 的四项真实合同覆盖邮件失败/抑制/冷却恢复/幂等、
发现规则的数字 JSON 与审计/缓存事件、编辑推荐创建/修改/移除，以及后台 SystemHealth。
邮件单例与发现配置在 cleanup 中恢复；合成资料及追加审计仅保留于可丢弃测试库。

独立只读复核未发现新的代码功能或安全问题；疑似 outbox 重试参数风险用真实 PG16 PREPARE 排除，
没有执行 UPDATE。历史截图必须保留原日期，本轮新证据应使用新文件。

## 复跑入口

以下命令从仓库根目录执行。依赖须先安装，数据库必须是隔离测试库；
所需 URL、确认变量和测试角色在私有终端环境配置，不在文档中提供值。
完整环境变量及建库顺序沿用 [CI 工作流](../../.github/workflows/ci.yml)和 [运行手册](../RELEASE_A_RUNBOOK.md)。

### Python

```sh
uv venv --python 3.12 .venv
uv pip install --python .venv/bin/python -r requirements-test.txt
.venv/bin/python -m ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
.venv/bin/python -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/generate_openapi_types.py scripts/release_a_deployment_probe.py scripts/release_a_env_check.py scripts/release_a_email_e2e.py scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py
.venv/bin/python scripts/generate_openapi_types.py --check
PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv/bin/python -m pytest -q -ra
```

HTTP 合同需要允许绑定本机回环端口；禁止 socket 的沙箱失败不能视为产品失败或直接计作跳过。

### Go 与真实数据库

```sh
go test ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
MEDIA_CONTRACT_PYTHON="$PWD/.venv/bin/python" go test -p 1 -count=1 -race ./services/platform-api/... ./services/platform-worker/...
sh db/tests/postgres_roundtrip.sh
go test -count=1 ./services/platform-api/internal/database
go test -count=1 ./services/platform-api/internal/database -run '^TestPostgresOperational' -v
go test -count=1 ./services/platform-worker/internal/outbox -run '^TestPostgresOutboxLeaseContract$'
go test -count=1 ./services/platform-worker/internal/mediareconcile -run '^TestPostgresReconciliationScheduleContract$'
go test -count=1 ./services/platform-worker/internal/mediainspect -run '^TestPostgresInspection.*Contract$'
go test -count=1 ./services/platform-worker/internal/mediausage -run '^TestPostgresMediaUsage.*Contract$'
go test -count=1 ./services/platform-worker/internal/mediauploadcontrol -run '^TestPostgresMediaUploadQueueContract$'
.venv/bin/python scripts/release_a_core_e2e.py --output docs/evidence/release-a-core-e2e-ubuntu-2026-09-12.json
```

数据库命令需要先配置工作流中的受限测试连接与 disposable-database 确认变量。
roundtrip 最后会执行 down；API 仓储及核心 E2E 必须使用各自重新迁移/初始化的专用库，
不能将上述命令理解为在同一个数据库连续运行。Down 会删除集群级角色，因此 roundtrip 必须使用独立 PostgreSQL 实例。本轮增强负向测试曾在共享实例回滚时被依赖保护拒绝，随后改用无外网、无端口映射的独立容器完整执行成功并清理。Go → Python 合同需将 MEDIA_CONTRACT_PYTHON 指向 .venv/bin/python。
核心 E2E 前需按运行手册构建 platform-api 与 create-owner 二进制。

本地备份依次使用 weekly_backup.sh、verify_copy.sh、record_copy.sh、verify_restore.sh；
前提是独立 BACKUP_DIR、校验信息、审计连接和新建空恢复库均已配置。
脚本位于 [infra/backup](../../infra/backup)，不把本地复制命令包装为跨区传输证据。

### 前端与部署

```sh
export PLAYWRIGHT_BROWSERS_PATH="$PWD/.cache/ms-playwright"
npx playwright install --with-deps chromium --only-shell
npm run typecheck
npm run lint
npm run test:frontend:smoke
npm run test:preflight
npm run build
npm run smoke:frontend:release-a
npm run test:cache:release-a -- --output docs/evidence/release-a-cache-contract-ubuntu-2026-09-12.json
CI=1 PLAYWRIGHT_JSON_OUTPUT_NAME="$PWD/.cache/acceptance/frontend/playwright.json" node node_modules/@playwright/test/cli.js test --reporter=line,json --output=.cache/acceptance/frontend/results
npm run capture:visual:release-a -- --browser-channel chromium --output-dir docs/evidence/release-a-visual-2026-09-12
npm run probe:performance:release-a -- --browser-channel chromium --output docs/evidence/release-a-performance-2026-09-12.json
npm run probe:http-latency:release-a -- --output docs/evidence/release-a-http-latency-2026-09-12.json
npm run audit:production
npm audit --audit-level=high
npm run check:markdown-links
docker compose config --quiet
docker buildx bake release-a --load
```

日本/北京 Compose 与 nginx -t 的完整命令见 CI compose 作业；测试 DNS override 只用于语法检查。
五镜像真实构建与运行结果见 [Docker 汇总](./release-a-docker-ubuntu-2026-09-12.json)，不是仅 bake 图解析或 nginx -t。构建因网络限制显式使用公开 Go/PyPI 镜像源；Dockerfile 默认仍为官方源，未向镜像代理提交凭据。
本轮镜像标记 source_state=modified_worktree、base_revision=7a932be95fbcad0f5e394a414101f7e89a36d786。OCI revision 使用的是测试时提供的基线标签，不能据此宣称镜像来自未修改的旧提交。镜像仅本地留存；正式部署要使用提交后的新 SHA 重建并固定版本。

## 前端机器证据与未覆盖项

- [前端最终验收汇总](./release-a-frontend-ubuntu-2026-09-12.json)：40 单元、62 工具、82 浏览器、构建/HTTP、依赖审计及执行边界。
- [缓存合同](./release-a-cache-contract-ubuntu-2026-09-12.json)。
- [视觉 manifest](./release-a-visual-2026-09-12/manifest.json)。
- [LCP/CLS 原始样本](./release-a-performance-2026-09-12.json)。
- [HTTP 延迟原始样本](./release-a-http-latency-2026-09-12.json)。

上述前端证据使用本地 standalone 与内存合成 API；无真实两地网络、对象供应商或公网负载。
最终布局版本的首页/详情桌面 LCP 中位数为 332/196ms，移动为 184/148ms，移动 CLS 均为 0；首页 API/搜索 P95 为 29.79/9.10ms，均低于产品门槛。
此前两次 npm 包装入口在不同位置收到 SIGTERM，未计为通过；最终直接调用已安装的 Playwright CLI 完整通过 82 项，signal-only 追踪未再观察到终止信号，此前发送来源仍未确认。
真实 R2、Cloudflare、账单与共享应用完整性、目标两机资源、SFTP、SMTP、域名/TLS/Turnstile、
后台网络限制和目标视觉/性能均未放行。完整 Release A / M5 与公网结论仍为 no-go。

## 清理与交付

- 已删除本轮 PostgreSQL、Mailpit、镜像冒烟和 Nginx 检查容器、隔离网络、测试空卷及合成 TLS 文件；复核无遗留验收服务。
- 合成数据库、测试邮件和本地测试备份随容器清理；可通过本文入口重新生成，不保留原始数据副本。
- 已删除 .cache/acceptance 的原始日志/临时配置和 .cache/core-e2e 二进制，共约 31 MiB；此前前端另清理约 163 MiB 中断下载、运行副本与冗余截图。
- 保留开发依赖、可复用构建缓存和五个通过验收的本地镜像；保留脱敏 JSON、新日期视觉证据、历史设计与验收记录。
- 当前进度和就绪审计已精简重复过期段落。提交包含修复、回归断言、依赖锁与验收证据，不包含凭据、原始服务日志、数据库备份或临时产物。
