# Release A 主图读取防御复核

日期：2026-09-12（Asia/Shanghai）

## 结论

- 作品目录、搜索、推荐、厂牌和人物关联作品等共享摘要查询，只把 `cover / is_primary=true / position=0` 作为封面。
- 人物目录和发现流共享摘要查询，只把 `avatar / is_primary=true / position=0` 作为头像。
- 登录用户的关注列表、作品浏览历史和人物浏览历史使用相同槽位约束；没有合法主图时返回空图片，由前端默认图兜底。
- 运营后台当前主图读取及主图替换事务内的两次读取，都要求实体对应的合法用途和位置。历史异常行不会被当作当前可替换主图。
- 详情图库的 `cover/0 + gallery/1..3` 规则没有改变；本轮没有新增 API、配置或数据库迁移，Schema 保持 v21。

底层 `public_entity_media` 视图继续负责 published、权利允许和公开派生对象边界；本轮在各读取入口补充展示槽位边界。相关实体每个正常发布周期最多四个展示槽位，新增谓词不会产生 N+1 查询，也不改变分页或缓存合同。

## 本地验证

```text
go test ./services/platform-api/internal/database ./services/platform-api/internal/httpapi
ok self-deepsearch/services/platform-api/internal/database
ok self-deepsearch/services/platform-api/internal/httpapi

go test ./services/platform-api/...
passed

go vet ./services/platform-api/...
passed

python -m unittest infra.security.tests.test_release_a_product_contract
Ran 46 tests
OK

npm run check:markdown-links
Markdown link check passed: 65 files, 408 local links, 0 missing

node scripts/release_a_preflight.mjs --python .venv/Scripts/python.exe --skip-build
passed：Go test/vet、OpenAPI 类型漂移、Markdown 65/408/0、TypeScript、ESLint、production audit 0 vulnerabilities、Ruff、mypy 18 files、Python 299 passed/303 subtests/1 skipped、工具合同 62/62、前端单元 40/40、standalone HTTP、Go→Next cache contract 9/9
```

自动 `codex review` 因本机外网/DNS不可用未产生评审结论，未计作通过证据；人工按查询调用链复核后没有发现可接受的新增问题。

## 后续补充：可执行的数据库行为合同

新增 [media_read_contract_test.go](../../services/platform-api/internal/database/media_read_contract_test.go)，补充三组 PostgreSQL 仓储行为测试：

- `TestMediaReadsPostgresPrimarySlots`：先验证合法主图，再逐项改变位置、主图标识和作品用途，直接调用收藏摘要、人物详情、关注、两类历史、作品详情和后台主图查询；要求资料仍存在、公开视图仍有三个派生对象，但仓储返回空图片。恢复同一槽位后图片重新出现。
- `TestPrimaryReplacementPostgresRejectsInvalidCurrentSlot`：异常主图替换返回 conflict，新资产/对象/关联/审计/刷新事件不落库；旧资产、对象 URL/状态、关联及删除队列快照不变。恢复合法槽位后，使用相同新 manifest 替换成功。
- `TestMediaReadsPostgresGalleryOrderAndMissingCover`：按 3、1、2 顺序登记精选图，读取结果必须是封面在前、精选图按位置排序，每个逻辑资产只有三个对应尺寸；隐藏封面后即使九个精选派生对象仍在公开视图中，详情也返回空图，恢复封面后图库恢复。

所有用例复用受限回环测试库 harness，需要 Schema v21、`platform_api_login` 和独立随机合成资料。现有 CI 的 `migrations` 作业执行完整 database 包，已自动包含这三个测试，无需新增服务或改动流水线。

本地验证：`go test -count=1 ./services/platform-api/internal/database` 与该包 `go vet` 通过；定向 `-v -run 'TestMediaReadsPostgres|TestPrimaryReplacementPostgresRejectsInvalidCurrentSlot'` 确认测试被发现，但数据库场景全部因 `PLATFORM_API_TEST_DATABASE_URL` 未配置而 **SKIP**。外层测试可能显示 PASS，不代表其 SQL 子场景已执行。独立只读评审确认 fixture、返回值和失败回滚断言与 Schema 相符；此前统一预检记录发生在本次仅测试代码增量之前，没有重复运行前端和浏览器。

本次补充后的 Markdown 链接检查通过：65 份文件、410 个本地链接、0 缺失。

## 未证明事项

- 没有启动 Docker，也没有连接真实 PostgreSQL、SMTP、S3/R2 或 Cloudflare。
- 尚未在真实 PostgreSQL 16 中注入历史异常媒体行，验证列表、账号历史和替换事务的实际返回结果。
- Schema v21 仍依靠处理器、HTTP 和仓储写入校验保持槽位组合合法；读取防御不等同于新增数据库 CHECK 约束。
- 本轮显式跳过 Next.js production build、性能、Playwright 和 Compose；这些门槛沿用 9 月 11 日独立通过证据，没有伪装成本轮重跑。
- 因此本地交付仍为 `conditional-go`，真实环境和公网仍为 `no-go`。
