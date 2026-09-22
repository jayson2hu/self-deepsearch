# 双项目数据接收与后台验收

日期：2026-09-22。结论：**数据接收与后台验收通过**（用户确认的本轮范围）。范围：采集端 SQLite 当前快照 → 采集端版本化导出与投递 → 实际平台 API 进程 → PostgreSQL 暂存 → 运营后台验收。正式人物发布、头像入库、生产部署不属于此数据接收验收结论。

## 可验收内容

| 项目 | 验收标准 |
| --- | --- |
| 原归档完整性 | 804 个文件及 SQLite 完整性通过，源归档保持只读 |
| 采集端交付 | 从采集项目自己的 SQLite 导出 4,196 条人物，生成 9 个受 hash 保护的版本化批次 |
| 实际 HTTP 入库 | 采集项目客户端投递到主平台真实 API 进程，使用来源绑定独立凭据 |
| 重试与恢复 | 再次执行跳过已确认批次；显式重放 9 批返回原回执；丢失本地回执后恢复不重复入库 |
| 错误请求 | 错误凭据 401；同键改内容 409；批次/来源/字段等异常整批拒收 |
| 后台数据核对 | 全量分页比对 4,196 条人物的源编号、姓名、别名、来源 URL、上游 hash、采集器版本和采集时间 |
| 权限 | owner/admin/editor 可查看；匿名和真实普通用户在列表、详情均被拒绝；注销后失效 |
| 页面 | 真实后台登录、批次/候选列表、查询筛选、详情、桌面与手机，无外站请求或运行时错误 |
| 公开隔离 | 候选全部 pending / needs_review / staging / unknown；正式人物、公开发布、媒体资产增量为零 |

## 功能入口

运营后台导航新增“采集验收”，地址 `/collector`。这里可核对各批次的输入/新增/重复数量，按人物姓名、别名、来源编号和候选状态筛选，打开详情核对来源与 hash。资料为文字候选，页面明确显示头像尚未导入、成年状态尚未核验；没有自动发布按钮。

平台只读接口：

- `GET /admin/v1/collector/batches`
- `GET /admin/v1/collector/candidates`
- `GET /admin/v1/collector/candidates/{candidateID}`

都复用现有后台会话权限。接收合同、错误状态及环境配置见[人物批次 v1](COLLECTOR_INGEST_V1.md)；采集端使用说明位于同级 `self-deepsearch-collector/docs/COLLECTOR_DELIVERY_V1.md`。

## 一次完整验收

需要两个同级仓库、Go 1.22、Node 22+、采集端 Python 3.12 环境、Docker 本地 `postgres:16-alpine` 镜像和 Chrome。任务不联网重采、不拉取镜像，不修改现有 review 数据库。

```bash
# 输出目录必须不存在；默认完成后自动停止本轮服务
python3 scripts/run_collector_e2e.py --output .cache/collector-e2e-new

# 保留通过验收的专用环境，供人工查看
python3 scripts/run_collector_e2e.py --output .cache/collector-e2e-review --keep
```

脚本编译当前 API 和运营后台，创建临时 PostgreSQL 16，应用当前迁移，以受限账号运行 API；采集数据经客户端实际 HTTP 投递。浏览器完成权限、全量数据、界面及公开隔离检查。所有服务只监听 loopback，端口动态分配；任何失败写入失败报告并清理本轮进程/容器。

输出目录有 `report.json`、`browser-report.json`、客户端导出/投递/重放日志、回执和 3 张截图。`--keep` 成功时另有仅当前用户可读的 `access.json`，记录专用合成账号、随机密码、访问端口和本轮资源。不要把该文件提交或放进公开证据。接收 token 只通过进程环境传递，不记录到报告或回执。

如在远程机器验收，从报告取得运营后台端口 `PORT`，建立隧道：

```bash
ssh -N -L PORT:127.0.0.1:PORT SSH_USER@SERVER
```

然后访问 `http://127.0.0.1:PORT/collector`，使用 `access.json` 中的合成账号登录。停止只处理本轮带身份标记的资源：

```bash
python3 scripts/stop_collector_e2e.py .cache/collector-e2e-review
```

原始归档内的 130 张头像仍由 2026-09-19 的[固定归档联调预览](COLLECTOR_INTEGRATION_ACCEPTANCE.md)覆盖，本次新流程不会把它们登记成已获准的正式媒体。正式发布验收需要另行完成实体映射、权利与成年状态审核、媒体入库及发布闭环。

## 本轮执行记录

最终整轮通过：[完整报告](evidence/collector-e2e-2026-09-22/report.json)、[浏览器报告](evidence/collector-e2e-2026-09-22/browser-report.json)、[回归结果](evidence/collector-e2e-2026-09-22/validation.json)、[数据库边界与并发验收](evidence/collector-e2e-2026-09-22/database-contracts.json)。报告同时记录两个仓库 HEAD 与实际测试代码 hash，包含未提交实现，不将 HEAD 单独当作完整交付版本。

- 首次 9 次实际 HTTP 投递，4,196 来源记录 / 4,196 候选 / 9 批次；全部 pending。
- 再次执行 0 请求、跳过 9 批；显式重放 9 请求 / 9 原回执；删除一份本地回执后 1 请求恢复 / 1 原回执。
- 后台全量字段核对、真实 owner/admin/editor/user 权限矩阵、搜索/分页/详情、公开隔离、实际登录与退出按钮均通过。
- 正式人物、publication、media_assets、media_staging 均为 0；未公开候选。
- 390px 页面宽度为 390px；0 外站请求、0 浏览器运行时错误。截图：[桌面](evidence/collector-e2e-2026-09-22/collector-desktop.png)、[手机](evidence/collector-e2e-2026-09-22/collector-mobile.png)、[详情](evidence/collector-e2e-2026-09-22/collector-detail.png)。
- 平台 API 回归、13 项主平台 Python 回归、采集项目 51 项 Python + 8 项 JS、运营后台构建/lint、OpenAPI 类型与 Markdown 链接检查通过。

保留两次失败证据：[第一次](evidence/collector-e2e-2026-09-22/attempt-1-browser-report.json)发现手机筛选表单固定网格导致全页溢出，已用页面局部样式修复；[第二次](evidence/collector-e2e-2026-09-22/attempt-2-browser-report.json)测试用原始请求退出时未发送浏览器 Origin，被正确拒绝，已改用真实页面退出按钮验证。最终重新运行整个流程通过，未覆盖旧失败报告。

### 本次保留的人工验收环境

服务器本机后台：`http://127.0.0.1:54307/collector`；API：`http://127.0.0.1:35521`。仅 loopback。账号为 `collector-owner@example.test`，随机密码只保存在 `.cache/collector-e2e-20260922-final/access.json`（0600）。本环境是独立临时环境，不替换原有 13000/13001/13004 服务，不承诺主机重启后恢复。

远程访问需使用同端口隧道，避免 Origin 不匹配：

```bash
ssh -N -L 54307:127.0.0.1:54307 SSH_USER@SERVER
```

随后打开 `http://127.0.0.1:54307/collector`。人工验收建议依次查看批次计数、搜索一个人物、打开核对资料、翻页、退出后再访问。

结束本轮环境：

```bash
python3 scripts/stop_collector_e2e.py .cache/collector-e2e-20260922-final
```

旧的[进程内数据库验收](evidence/collector-ingest-2026-09-22/contracts.json)保留原范围。本轮不覆盖正式人物发布、头像入库、生产 TLS/代理、跨机器网络或负载压测。
