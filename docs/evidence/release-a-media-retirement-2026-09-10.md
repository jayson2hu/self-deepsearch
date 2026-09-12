# 媒体退出、重复下架与删除调度

日期：2026-09-10（Asia/Shanghai）。延续 Release A，不改变自动采集/广告关闭边界。未启动 Docker、真实 PostgreSQL、SMTP、S3/R2、Cloudflare 或远程 CI，也未提交/推送代码。

## 本轮变化

- `applyTakedown` 使用统一 `retireMediaObjects`：资产/关联撤下后，公共对象立即排队，私有对象可给定保留期限；权利下架传当前时间，能够提前已有 pending/retry 的私有期限。
- 新删除幂等键为 `media-delete:{media_object_id}`，不随权利工单变化。重复操作不得覆盖原 payload、推迟时间、重置 attempts、抢占 running 或复活 dead。已有任务原公开 URL 按资产/scope/key/北京路径精确匹配恢复；当前 URL 和历史任务都缺地址时返回失败并回滚，不猜测来源地址。
- 按 outbox → media_objects 顺序加锁，与 Worker 完成路径一致；对象 hidden 时仍计费/计量，只有已有真实删除回执路径才可将其置为 deleted，保留首次 deleted_at。
- 草稿、审核中等未发布资料也能权利下架，不再强制要求主站 publication 存在，不创建伪 publication；实体缺失仍拒绝。
- OpenAPI 明确权利工单 completed 不等于远端删除完成；HTTP 失败不输出成功工单或底层诊断。无 JSON 请求字段变化、无新增迁移，整体 Schema 仍为 v17。

主图切换 API/后台尚未开发。本轮的可延期退出函数和测试夹具只是旧母版保留的基础，不能据此声称“主图替换、保留 30 天和到期清理”已对用户可用。

## 验证与证据强度

确实执行的先失败后通过用例是未发布父资料下架：新增事务替身测试在原代码返回 `operations state conflict`，移除错误的 publication 必须存在条件后通过；同时保留缺失实体拒绝。重复下架的原 URL 风险来自原 SQL 读取已清空字段的代码审查；新锁、SQL 与去重语义有静态/事务替身检查和可选真实 SQL 合同，但没有在本机实际执行 PostgreSQL，不能称已验证真实并发。

| 范围 | 本轮结果 |
| --- | --- |
| Go API/Worker vet/test 与 API 编译 | 通过；显式启用实际 Go→Python 本地 HTTP 合同，API 二进制仅编译未启动 |
| 退出专项 | 锁/游标顺序、私有期限/公共立即、错误传播、位置校验、草稿下架与缺实体拒绝通过 |
| HTTP | 删除队列/身份错误返回 503/no-store，不返回 completed 或诊断文本 |
| PostgreSQL | 五组合同明确 SKIP：重复工单与租约/失败次数保留；私有期限提前；旧格式 URL 恢复；缺失身份回滚；作品/人物草稿图片下架 |
| Python 回归 | 181 项 + 88 个 subtests 通过，81.49 秒 |
| OpenAPI TypeScript 生成/漂移 | 通过，响应说明更新、不改变数据字段 |
| 前端与相关质量检查 | 两站/合同包 TypeScript、两站 ESLint、13 项单元烟测、standalone HTTP 冒烟和相关 ruff 通过；沿用已有 production 产物，没有重新 build/Playwright |

补充验证：旧任务按批次解析一次，再按完整位置映射恢复地址，避免对每个对象重复查询历史 outbox。实际 Go 测试覆盖恢复旧版不含 `media_object_id` 的 payload，同时拒绝另一资产、范围、有效但不同的 key、副本路径和无效 URL；不会把旧任务的对象 ID 误当成当前对象 ID。Worker 签名测试使用新附加字段并验证其不被转发到 Python，既有旧格式用例仍通过。SQL 锁与事务的最终保证仍待真实库验证。

SQL 合同使用既有随机合成夹具和受限 `platform_api_login`，只允许回环的固定一次性库 / Schema v17；保留审计，不增加 DELETE 权限或停用触发器。测试留存未来任务仅用于模拟退出阶段，不启动 Worker 或对象存储；缺库 SKIP 不等于 SQL 通过。

## 兼容和运维边界

新 payload 增加 `media_object_id`；Go Worker 只向 Python 转发原有 scope/key/path/url 和事件 ID，附加字段不会改变签名接收合同。旧格式工单式事件不删除、不改写；首次新格式操作可能新增每对象一条规范任务，旧/新任务都依赖远端幂等。不能为让失败数下降而丢弃旧 dead 证据。

需要配套更换所有 API，避免旧 API 继续创建空地址任务。已有 running 的任务由原持有者完成，dead 必须排除失败原因后独立复核；重复新工单不承担重置死信的职责。全部确认前容量统计保留对象字节。

公开 URL 的恢复不是任意抓取：本轮没有从网络、来源页、manifest 外部地址或推测域名下载数据。缺少原 URL 的异常对象必须由管理员核对不可变资料修复，失败事务不冒充完成；必要时保持相关公开入口不可见。

## 重跑

```powershell
$env:GOCACHE=Join-Path (Get-Location) '.cache/go-build'
$env:GOMODCACHE=Join-Path (Get-Location) '.cache/go-mod'
$env:GOPROXY='off'
$env:MEDIA_CONTRACT_PYTHON=Join-Path (Get-Location) '.venv/Scripts/python.exe'
go vet ./services/platform-api/... ./services/platform-worker/...
go test ./services/platform-api/... ./services/platform-worker/... -count=1
go test ./services/platform-api/internal/database -run 'TestMediaRetirement|TestRetiredMedia|TestTakedownAllows' -count=1 -v
$env:PYTEST_DISABLE_PLUGIN_AUTOLOAD='1'
./.venv/Scripts/python.exe -m pytest -q -p no:cacheprovider
npm run check:api-types
```

主图替换与真实依赖验收仍有工作，Release A 保持 active；公网仍 no-go。后续按[运行手册](../RELEASE_A_RUNBOOK.md)执行受限 SQL、队列时序和真实删除链路，不缩减原图片生命周期要求。
