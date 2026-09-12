# Release A 图片展示槽位与详情重入验证

日期：2026-09-11（Asia/Shanghai）

## 结论

- 作品主图只接受 `cover / is_primary=true / position=0`。
- 作品精选图只接受 `gallery / is_primary=false / position=1..3`；要求已有 published 主图，最多三张，同一位置不可重复。
- 人物头像只接受 `avatar / is_primary=true / position=0`。
- 公开详情只选择上述有效组合；没有有效主图时不提升精选图，返回空图片并由公开站使用实体专用默认图。
- 详情页每次真实进入会上报一次；React Strict Mode 对同一挂载实例的 effect 重放不会重复上报，快速离开再返回同一详情不会被时间窗口吞掉。

处理器、HTTP、仓储、公开读取和运营后台均执行自己的边界校验。仓储在父资料行锁内检查主图、精选图数量和目标位置后才插入资产，拒绝路径不会留下媒体资产。当前没有新增数据库迁移，整体 Schema 仍为 v21。

## 本地验证

```text
go test ./services/platform-api/internal/httpapi ./services/platform-api/internal/database
ok self-deepsearch/services/platform-api/internal/httpapi
ok self-deepsearch/services/platform-api/internal/database

python -m pytest -q workers/media-python/tests/test_pipeline.py
9 passed, 7 subtests passed

ruff check workers/media-python/media_worker/pipeline.py workers/media-python/tests/test_pipeline.py
All checks passed!

npm run typecheck --workspace @self-deepsearch/display-web
npm run typecheck --workspace @self-deepsearch/ops-web
npm run lint --workspace @self-deepsearch/display-web
npm run lint --workspace @self-deepsearch/ops-web
npm run check:api-types
passed

npm run build --workspace @self-deepsearch/display-web
npm run build --workspace @self-deepsearch/ops-web
passed

npx playwright test tests/e2e/release-a.spec.mjs -g "每次进入详情页|后台在提交前拒绝" --project=desktop --project=mobile
4 passed

npx playwright test tests/e2e/release-a.spec.mjs --project=desktop --project=mobile
78 passed（39 个场景 × 2 个视口）

node scripts/release_a_preflight.mjs --python .venv/Scripts/python.exe --skip-build
passed：Go test/vet、OpenAPI、64 份 Markdown/403 个本地链接、TypeScript、ESLint、production audit 0 vulnerabilities、Ruff、mypy 18 文件、Python 299 passed/303 subtests/1 skipped、工具合同 62/62、前端单元 40/40、standalone HTTP、Go→Next 缓存合同 9/9
```

Python 测试在本机关闭了无关的第三方 pytest 插件自动发现，以避免全局 `langsmith` 插件缺少依赖影响项目测试；未改变测试集合。

## 未证明事项

- 没有启动 Docker，也没有连接真实 PostgreSQL、SMTP、S3/R2 或 Cloudflare。
- 真实 PostgreSQL 16 上的父行锁并发、重复精选图位置、第四张精选图和只有精选图没有主图的合同尚未执行；测试库 SKIP 不算通过。
- 未验证真实图片对象、北京副本、公开 URL、缓存刷新或物理删除。
- 因此本地交付包仍为 `conditional-go`，真实环境与公网发布仍为 `no-go`。
