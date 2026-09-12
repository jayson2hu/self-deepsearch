# Release A 后台图片上传控制页面验证

日期：2026-09-11（Asia/Shanghai）。范围：运营后台 `/media/upload-control`、客户端响应合同、共享近期密码请求截止信号、有状态合成 API、桌面/手机回归和当前架构/运维文档。

不新增服务、依赖或数据库迁移；当前 Schema v21、62 表/5 视图，OpenAPI 87 操作/76 路径/106 Schema，20 个高风险操作要求近期密码。没有启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 或 Analytics，没有部署、触发远程 CI、暂存或提交。Release A/M5 和完整目标仍未完成，公网 no-go。

## 已执行和最终结果

| 检查 | 结果与边界 |
| --- | --- |
| 运营后台 production build | 通过；包含 `/media/upload-control`，最终时序修正后重新构建。公开站沿用已有制品，未重新构建 |
| 两站/合同 TypeScript、两站 ESLint | 最终时序修正后通过 |
| 前端单元合同 | 22/22 通过；新增上传控制 7 项，包含 21 种异常概览、错误/超大/非法 UTF-8 成功响应、原始微秒和同键重试、错误关联、权限错误及近期认证截止信号 |
| 工具合同 | 56/56 通过；没有调用整套联网预检、依赖审计或 Docker |
| DB/安全/环境/OpenAPI 离线合同 | 136 项、108 subtests 通过；文档更新后再跑同样全部通过（30.66 秒），是静态/替身合同，不是 SQL 实际执行 |
| OpenAPI 类型漂移 | `--check` 通过，未改变接口合同 |
| 浏览器 | 最终 **70/70 通过（5.0 分钟）**，含 8 个新增上传控制场景 × 两个视口；无 skip。旧 16 项定向成功和首轮全站 69/70 不冒充最终结果 |
| 两站 standalone HTTP | 最终浏览器退出后独立执行并通过：首页、账号入口、页脚/18+、广告占位/默认图、三类 JSON-LD/XSS、安全头/CSS 与上游不可用 503/no-store |
| 视觉 | 四张最终截图已保存并逐一复核；桌面 1366×900、手机 390×844，全页截图；布局无横向溢出，确认、待处理、未知结果清楚区分 |
| 文档链接 | 本轮更新的 12 份文档共 185 个本地文件链接全部存在；不验证页内锚点或外部网址 |

## 关键覆盖

- 发送中、202 受理、最近远端观察与有效执行回执分开：延迟 POST 期间不显示“结果未知”，受理后不把当前状态改成目标值；只有刷新得到有效回执才显示已应用。
- 暂停/恢复独立确认；修改原因/操作后确认失效，恢复用键盘可勾选，generation 0 的恢复选项原生 disabled。恢复被用量守卫拒绝时，页面仍显示原暂停观察。
- HTTP 响应真正丢失、不完整 202、刷新失败后原请求仍在；重试的 JSON、UUIDv4 幂等键和微秒快照完全不变，服务器替身只创建一条命令；数据库指令 ID 与客户端幂等键不同。
- 409 要刷新和重新确认；未连接、部分回执、过去超过两分钟、未来时间、页面停留后自然过期均禁止新操作。pending/running/uncertain 不允许创建另一条命令。
- uncertain、superseded 与 applied 明确区分；伪造无回执 applied 不会显示完成。React 将攻击片段按普通文本展示，没有创建图片或执行脚本。
- owner/admin 可操作，匿名被引导登录，editor 无导航、直接页面跳回后台首页且 API 替身拒绝。真实权限由 Go 强制执行，浏览器替身不证明 PostgreSQL 权限。
- 完整套件同时回归番号搜索、注册/重置/关闭、退出错误恢复、收藏/关注/反馈、录入/异人审核/发布、邀请/角色、主图替换、权利下架、用量复核和缓存 HTML 停图。

## 发现并修正的问题

1. 原嵌套 label 没有明确关联，浏览器无法用精确“操作类型”定位；改为 `aria-labelledby`，原因字段同步显式关联。
2. 框架的路由播报也使用 alert，原断言产生歧义；业务错误增加“上传控制错误”可访问名称。空 status 按设计隐藏，改为核对隐藏节点的空文本；原生 option 用 disabled 属性验证，不错误依赖不适用的通用状态断言。
3. 首轮全站 69 通过/1 失败暴露时序问题：发送前就设置 uncertain，手机可能在请求尚未受理时显示原请求重试区域。改为仅在失败/未知响应后保留原请求；加入拦截延迟 POST 断言，丢失响应场景等重试按钮可用才检查请求证据。修正后重建并完整复跑，不以重试偶然通过掩盖问题。
4. 成功响应运行时类型检查拒绝数组伪装的 mode/status，近期密码请求补截止信号；共享调用的回归面不局限上传页面。

## 可复跑命令

仓库根目录 PowerShell；使用已安装的本机依赖，不下载模块：

```powershell
$env:NEXT_TELEMETRY_DISABLED='1'
npm run build --workspace @self-deepsearch/ops-web
npm run typecheck
npm run lint
npm run test:frontend:smoke
npm run test:preflight
npm run test:e2e:release-a -- --max-failures=2
npm run smoke:frontend:release-a

$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
.venv/Scripts/python.exe -m pytest -q db/tests infra/security scripts/tests/test_release_a_env_check.py scripts/tests/test_generate_openapi_types.py
.venv/Scripts/python.exe scripts/generate_openapi_types.py --check
```

E2E 使用随机回环端口、真实两站 standalone 与合成 API，运行结束清理自己的临时服务。不操纵用户已打开浏览器的登录状态，不把浏览器内合成 command/receipt 当作真实 Worker 派发或供应商暂停回执。浏览器插件连接能力本轮不可用，按技能的回退路径使用仓库已有 Playwright 测试。

## 最终截图

以下均来自最终 70/70 的本机合成数据，不包含真实用户、真实图片或供应商回执；攻击文本仅用于转义验证。四张 PNG 共 452325 字节，未保存重复预览或完整视频。

| 截图 | SHA-256 |
| --- | --- |
| [桌面有效回执](./release-a-media-upload-ui-2026-09-11/desktop-upload-confirmed.png) | `2abc6081a0f0ea1ebf3ec48f6d731e065dbaaa8c5d8d53dca31f698da5ed4de6` |
| [手机请求待处理](./release-a-media-upload-ui-2026-09-11/mobile-upload-pending.png) | `4f6c1282f1dd79620fe85f6a92f4ad53547bd0d7b2b0ee38ce04f5e84a576401` |
| [手机恢复独立确认](./release-a-media-upload-ui-2026-09-11/mobile-upload-resume-confirmation.png) | `2214280eb2d8bff1a84aefcdd8e122eb87247df3210af50dab39c62d16b9ae31` |
| [桌面保留原请求](./release-a-media-upload-ui-2026-09-11/desktop-upload-unconfirmed.png) | `4e93dd4a01faa507b4fd32e563f80ef3eae79dd77f9bec5f680b97ff9a3523be` |

## 未执行与剩余工作

本轮没有重新执行 Go 全量测试/构建、完整媒体 Python 测试/ruff/mypy、真实 Go→Python HTTP、SQL fresh/upgrade/down、性能探针、联网依赖审计或 Compose 检查。后端证据保留在[上传控制队列验证](./release-a-media-upload-queue-2026-09-11.md)，本轮前端证据不扩大其结论。

仍需自动额度联动、API/Next 动态停图、账户存储/账单口径和图片源站/直连/边缘全入口限制。真实 PG、SMTP、R2/S3/Cloudflare/Analytics、北京 Linux 卷/断电、SDK 在途写入、公网传输和两台小规格服务器资源门槛仍待验收；页面不能证明零费用保护，也不会清 pending、由用量复核自动恢复或打开默认关闭的执行器。操作与升级见[队列/页面手册](../MEDIA_UPLOAD_QUEUE.md)。
