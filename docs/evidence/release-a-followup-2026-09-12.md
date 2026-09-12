# Release A 未完成项续验

日期：2026-09-12（Asia/Shanghai）。基线：`011b7f5e9a521882b286a0f3431af0be82719812`，结果来自其上的修复工作树；不把基线 SHA 或此前五个本地镜像冒充本次修改后的制品。

## 结论

本机能执行的三项遗漏已补齐：两站真实浏览器与 Worker 缓存闭环、历史归档恢复、失败的 Python CI 收集门槛。另修复真实验收发现的作品保存反馈错误，以及归档和 purge 的安全/可靠性缺陷。目标 R2/Cloudflare、最终入口和两地部署仍未验收，完整 Release A / M5 与公网继续 `no-go`。

## 实际发现与修复

1. 上一提交 [CI 运行](https://github.com/jayson2hu/self-deepsearch/actions/runs/34680692904) 的 Python `Run Python tests` 失败，五镜像作业被跳过。匿名日志不可下载；本机使用相同 console `pytest` 复现 `scripts` 导入失败，修正按文件定位脚本的导入，并增加隔离工作目录的收集回归。没有把推送成功当成 CI 成功。
2. 真实浏览器第一轮因新脚本对嵌套 label/select 的精确匹配失败；改用唯一具名 select，并细分脱敏失败阶段。第二轮实际创建作品成功后，React 异步回调读取已失效的 `event.currentTarget`，同时显示成功与连接错误；保存表单 DOM 引用后再异步提交，补充成功、无错误及字段清空断言。第三轮完整通过。
3. Linux `os.Rename` 会覆盖已有归档；新增回归先失败，改用同目录硬链接无覆盖安装，同内容需校验 SHA-256 并复用原 inode。文件与各级目录同步完成后才允许数据库归档成功；同步或 COMMIT 故障必须回滚标记，重试可安全复用孤儿文件。
4. 新增 `worker history-archive-verify`，离线只读核验可信 manifest、SHA-256、字节数、严格 gzip/JSONL、用户范围与精确时间；有大小/数量限制，不连接数据库，不使已删除历史重新可见。
5. 媒体 Cloudflare purge 的默认 HTTP 客户端此前会跟随重定向；本机用合成认证头复现跨源转发风险。现拒绝同源/跨源 301/302/303/307/308，目标端点不收到请求；失败返回可重试错误并保留备份。未使用真实 Cloudflare 凭据，不声称实际发生过凭据泄漏。
6. 新验收器限制一次性固定库名、回环地址、API/Worker 受限角色及环境白名单；浏览器只访问两站回环入口。清理独立执行，私有进程登记结合 Linux 启动身份回收自有 Node/Next/Chromium，包括组长已退出和后代忽略 TERM 的场景，不按名称批量终止进程。

## 最终实测

| 项目 | 结果 | 边界 |
| --- | --- | --- |
| 两站真实浏览器闭环 | 15 个浏览器检查及 API/Worker readiness 全通过，0 手动失效 | 真实 Go API、日本 Worker、PostgreSQL 16、Mailpit、生产 Next 和 Chromium；合成账号/作品 |
| 发布/隐藏传播 | 1480ms / 1224ms，均小于 60 秒观察窗口及 300 秒缓存 TTL | 首页/详情/sitemap 为缓存检查；搜索 `no-store`，只证明公开可见性 |
| 历史归档 | race 下 14 顶层 + 34 子测试，含 9 个真实 PG 场景，0 skip/fail | 实际软删除→归档→manifest→临时表逐字段恢复；不是目标复制或真实断电演练 |
| Python CI 原命令 | 最终 315 tests + 339 subtests，0 skip/fail | `PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv/bin/pytest -q`，32.34s；包括最终缓存证据边界 |
| Python 静态/API | Ruff、mypy 19 文件、OpenAPI 漂移检查通过 | 生产工具及新父运行器 |
| Go | API/Worker 全量默认 test、vet、race 通过 | 包含真实 Go→Python 回环 HTTP；一般运行不自动开启 SQL opt-in，归档专项单独证明 SQL/race |
| 前端 | 40 单元、76 工具、两站 lint/typecheck 通过 | 运营站重新 production build；公开站沿用未修改的前次构建 |
| 表单浏览器回归 | 桌面/移动 2/2 通过，并通过上方真实闭环 | 增强原 82 项套件中的录入审核场景；本次本机未重复其余 80 项 |

机器结果：[真实浏览器](./release-a-real-stack-ubuntu-2026-09-12.json)、[归档恢复](./release-a-history-archive-ubuntu-2026-09-12.json)。命令与专用空库要求见[运行手册 7.1–7.3](../RELEASE_A_RUNBOOK.md#71-真实核心服务-ci-验收)，归档参数见[只读工具说明](../../services/platform-worker/internal/historyarchive/README.md)。

## CI 与交付边界

新增独立 `real-stack-e2e` 作业；`migrations` 串行执行归档真实 PG 合同；五镜像 `images` 必须等待两者及原有 Go/Python/web/core/Compose 门槛。只上传脱敏结果，不上传原始日志、验证码、Cookie 或私有配置。远端是否通过必须查看修复提交对应的 Actions 运行，不能从本地测试推断。

本次不新增 Schema 迁移，不开启采集、广告联盟、真实上传删除或公网部署。真实 S3/R2、Cloudflare purge/route/binding、最终 SMTP/Turnstile、权利邮箱、后台网络限制、两机资源、跨区 SFTP 与负载仍需用户提供对应目标环境和授权。凭据应在受控环境配置，不粘贴进代码、证据或聊天。

临时资源已由本次创建方清理：两个本轮专用 PostgreSQL/Mailpit 容器及合成数据库/邮件/归档、隔离前端副本、临时二进制和原始日志均已删除。合成数据无法原样恢复，但可按手册重新生成；没有删除真实资料。保留本页及脱敏 JSON、源代码、开发依赖和可复用缓存。历史证据不覆盖，未执行全局 Docker prune。
