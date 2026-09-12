# Release A 主图替换验证

日期：2026-09-10（Asia/Shanghai）。范围：主图查询、并发校验替换、后台确认与旧图生命周期接入。保持 Schema v17；自动采集、广告联盟、头像识别关闭。

## 已实现

- `GET /admin/v1/media/primary`：admin/owner 单快照查询父状态与可空主图关联，不返回私有存储路径。支持未发布/普通隐藏资料的后台核对。
- `POST /admin/v1/media/primary/replace`：admin/owner + 近期密码；请求包含 `expected_asset_id` 与未经手改的处理器 manifest，新资产不可等于旧资产，必须为主图。
- 父资料锁 → 读取预期主图 → 旧资产锁 → 重读关联 → 隐藏旧关联 → 插入新 manifest → 检查共享 → 必要时退出旧对象 → 替换审计 → 提交。新资产/对象/关联、缓存事件、删除事件和审计同事务；失败回滚。普通登记复用同一插入函数，不隐式替换。
- 共享旧图保守保留：只要还有其他 published 关联，就不退出旧资产/对象，即便其他父资料暂未公开。无其他关联时，旧公开图立即到期、私有母版保留 30×24 小时后到期；权利下架可提前，不能重置已有 dead/running 任务。
- 后台区分首次登记和替换，替换额外确认。文件/查询异步状态使用序号隔离；提交期间禁止重新选文件/移除，禁止重复提交；失败或不完整成功响应保留清单，取消确认，刷新后再核对。新资产已是当前主图时阻止重复写入。
- 客户端核对 201、资产 UUID、对象数/版本/状态/时间和旧资产/保留信息，不能把任意 2xx 当作成功。公开与私有删除状态提示分开，201 不等于物理删除，也不等于所有缓存已刷新。

涉及代码：

- [事务与共享保护](../../services/platform-api/internal/database/media_primary.go)
- [HTTP 校验与权限](../../services/platform-api/internal/httpapi/media_handlers.go)
- [后台确认页面](../../apps/ops-web/components/media-manifest-form.tsx)
- [客户端响应校验](../../apps/ops-web/lib/media-client.ts)
- [OpenAPI](../../packages/api-contracts/openapi.yaml)

## 验证记录

- Go API/Worker 全量 `go vet`、`go test -count=1` 通过；设置 `MEDIA_CONTRACT_PYTHON` 执行实际 Go→Python 本机 HTTP 合同。新 API 已编译，未启动。
- 新事务替身测试覆盖锁/重验顺序、允许/拒绝父状态、共享/非共享、30 天期限、缓存/删除/审计失败回滚及无效替换意图；HTTP 测试覆盖角色、参数、可空主图、409/503、精确保留响应和不泄漏私有诊断。
- 新增三组真实 PostgreSQL 合同：作品/人物 CAS 与期限及权利提前；共享保留/新资产冲突回滚；并发两写仅一方成功。本机未设置专用测试库，全部明确 **SKIP**。测试替身不证明真实行锁和 SQL 事务已验收。
- 两站及合同包 TypeScript、两站 ESLint、运营站 production build、OpenAPI 漂移和 13 项前端单元烟测通过。公开站本轮未重新构建，沿用已验证产物。
- 首轮 Playwright 桌面/手机 **42/42** 通过。新增五场景、两个视口，共十项：独立替换确认/删除提示、首次登记/共享保留、冲突/503 保留、GET 失败/不完整 201 后防重复、editor 禁止访问。
- 截图复核发现手机端通用表格规则隐藏字段并挤压行高，已将媒体对象表格改为可键盘聚焦的横向滚动区，保留六列，并增加六列表头可见/行高小于 100px 的测试。最终构建回归结果记录在下方。
- 首轮 Python 完整回归为 181 passed、1 failed、88 subtests：新增 OpenAPI 合同发现 YAML flow 描述中的逗号被解析为额外键。已给普通登记 409、替换 400/409 描述加引号，并检查描述对象键集合；专项 **44 passed + 7 subtests**、相关 ruff 与类型漂移复验通过。最终完整回归记录在下方。

### 最终复验

- 运营站在手机表格修复后重新 production build，通过；随后两站/合同包 TypeScript 与两站 ESLint 再次通过。
- 当前构建 Playwright **42 passed（2.6 分钟）**，含桌面/手机六列完整显示、行高与页面无横向溢出的新增断言。新截图均经人工视觉复核，替换保留说明、确认按钮与数据表格可用。
- 修正 YAML 后再次串行完整 Python：**182 passed、88 subtests passed，82.86 秒**。未与 Next build/Playwright 并行，避免备份子进程时序受重负载干扰。
- OpenAPI 类型仍无漂移；局部产品合同 **44 passed + 7 subtests**、相关 ruff 通过。已更新的八份 Markdown 共 **77 个本地链接**检查无断链。
- 最终 standalone HTTP 冒烟 **passed**：首页、登录/注册入口、页脚/18+、广告占位与默认图、三类详情 JSON-LD/转义、两站安全头/CSS、API proxy 503/no-store。临时测试 HTTP 进程已由运行器退出，不是部署服务。

## 视觉证据

[桌面确认页](./release-a-media-primary-desktop-2026-09-10.png) · [手机确认页](./release-a-media-primary-mobile-2026-09-10.png)

截图只有合成图片元数据、保留说明和测试账号，无真实媒体、私有凭据或外部存储调用。两张截图已替换为视觉修复后最终构建的输出。

## 验收边界

本轮未启动 Docker、PostgreSQL、SMTP/Mailpit、真实 S3/R2 或 Cloudflare，未触发远程 CI，未提交代码。浏览器技能所需连接工具在两次发现中均不可用；使用仓库既有 Playwright 回归运行器，未接管用户当前标签页。

对象上传在数据库事务之外。新图上传成功但登记/替换失败时可能产生未登记孤儿，保留不可变清单，交由现有对账流程核对，不自动猜测并删除。公开桶对象可能在登记前即经已知 URL 可访问；这里只接收已经核验可公开的图片。

M5 仍未完成整体验收，公网 **no-go**。下一步在授权的隔离环境运行 v17/登记/替换/退出 SQL，接真实 API/Worker 验证发布、缓存刷新、旧公开地址失效、私有母版期限/权利提前、北京副本删除及容量回收。不得通过手改 outbox completed 或对象 deleted 代替真实回执。
