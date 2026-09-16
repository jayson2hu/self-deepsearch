# RD v1 验收执行标准

版本：1.0 / 2026-09-17。适用于幕鉴重设计开发完成后的本地、隔离集成、制品和目标环境验收。本标准补充[验收计划](./ACCEPTANCE_PLAN.md)，不降低其中 AC-01～AC-30 的产品要求。

## 1. 结论状态

| 状态 | 定义 | 是否计入通过 |
| --- | --- | --- |
| PASS | 前置、步骤、断言和证据全部满足 | 是 |
| PARTIAL | 可执行部分已通过，但同一用例仍缺浏览器、目标环境或外部依赖 | 否 |
| BLOCKED | 当前环境缺少执行所需能力；必须记录尝试、原因和解锁条件 | 否 |
| FAIL | 已具备前置且断言不满足 | 否，必须修复后重跑 |
| NOT_APPLICABLE | 经交付范围确认确实不适用 | 否；不能用来排除失败项 |

开发者本地 PASS 不等于生产放行。只有适用门禁全部 PASS、负责人签收且没有 FAIL/BLOCKED，才能写“可上线”。

## 2. 六层验收门禁

| 层级 | 必须验证 | 最低证据 |
| --- | --- | --- |
| A 文档与静态 | 审核追踪、Markdown 链接、OpenAPI 生成、类型、lint、依赖高危漏洞 | 命令、数量、退出码 |
| B 单元与合同 | Go/Node/Python 单元、负例、权限、游标、SEO、缓存签名 | 测试数量、失败/skip 说明 |
| C 构建与制品 | 两站 production build、五个容器镜像、版本/修订标签、非 root、健康检查 | 镜像 ID、标签、运行用户、健康结果 |
| D 隔离真实栈 | PostgreSQL 迁移/权限、API、邮件、发布、搜索、sitemap、隐藏、缓存刷新 | 专用库、合成数据、链路结果、清理记录 |
| E 真实浏览器 | 320/390/768/1440、键盘、账号闭环、截图、LCP/CLS/INP 实验室样本 | 浏览器版本、截图、trace/report |
| F 目标与外部 | 最终 HTTPS、Nginx/CDN、S3/R2、Cloudflare、Turnstile、真实 SMTP、Search Console、容量与回退 | 目标域名、制品 digest、供应商/监控证据、负责人签字 |

前一层通过不能替代后一层。浏览器不可用时，HTTP 正文、Playwright 用例发现和静态 DOM 检查只能计入 A～D，不能把 E 写成 PASS。

## 3. 标准执行顺序

1. 固定代码 SHA，记录 dirty worktree；在干净快照执行，不纳入无关工作区改动。
2. 执行 A/B：Markdown、OpenAPI、TypeScript、ESLint、Go test/vet、Ruff、MyPy、pytest、Node 合同与依赖审计。
3. 执行 C：production build；构建五个镜像；核对 `org.opencontainers.image.version/revision`、非 root 用户和健康检查。
4. 执行 D：只使用明确命名的一次性 PostgreSQL/Mailpit；Schema v22 从空库迁移，加载受保护合成夹具，使用受限运行账号；执行 API 数据库合同、核心 E2E、缓存合同和 HTTP smoke。
5. 执行 E：使用仓库锁定的 Playwright 版本和浏览器；按 desktop/mobile 项目执行，不用“用例发现成功”代替执行。
6. 执行 F：只对已授权目标执行；先保持 SEO、广告和历史策略外部开关关闭，再完成域名、边缘、媒体、邮件、监控、备份/恢复和负责人签收。
7. 汇总 AC-01～AC-30；任何 PARTIAL/BLOCKED/FAIL 都必须写剩余条件。清理仅限本批明确创建的容器、网络、卷和临时文件。

## 4. 必跑命令基线

```bash
npm run check:markdown-links
npm run check:api-types
npm run typecheck
npm run lint
npm run audit:production
npm run test:preflight
npm run test:frontend:smoke
go test -count=1 ./services/platform-api/... ./services/platform-worker/...
go vet ./services/platform-api/... ./services/platform-worker/...
.venv/bin/python -m ruff check .
.venv/bin/python -m mypy workers/collector-python/collector workers/media-python/media_worker scripts/release_a_catalog_e2e.py scripts/release_a_core_e2e.py scripts/release_a_real_stack_e2e.py
.venv/bin/python -m pytest -q
npm run build
npm run smoke:frontend:release-a
npm run test:cache:release-a
npx playwright test --list
npx playwright test
```

真实数据库合同必须显式设置专用回环数据库 URL 和确认变量。数据库必须在每次确定性搜索/游标全量回归前重建并加载规定夹具；复用已被测试写入的库会污染期望集合，不得把脏库失败记为产品失败或通过。

## 5. 定量门槛

- sitemap：单片不超过 50,000 URL、未压缩不超过 50 MB；50,001 边界无重漏。
- 本地 HTTP 探针：公开首页 API P95 < 300 ms；搜索 API P95 < 500 ms；并发 1，预热 3 次，每端点至少 20 个样本。
- 正式性能目标：真实用户第 75 百分位 LCP ≤ 2.5 s、INP ≤ 200 ms、CLS ≤ 0.1；没有真实流量时写“数据不足”。
- 镜像：五个发布镜像全部有固定代码 revision、版本标签、非 root 用户和有效 HEALTHCHECK；运行态必须实际达到 healthy/HTTP 200。
- 安全：生产依赖 high 及以上漏洞为 0；私有响应不进入公共缓存；SEO 和媒体资格 fail-closed。

## 6. SEO 正式验收标准

- 只有正式环境、最终 HTTPS origin、显式索引开关和内容政策开关同时满足时允许 index/sitemap。
- 已发布且有独立价值的作品、人物、厂牌主实体页才可索引；搜索、登录、账号、隐私、测试、私有、错误和低质量页排除。
- canonical、robots、HTML、XML、结构化数据和图片预览使用同一资格判断；unknown 默认不索引、不外部预览。
- 普通内容可使用标准图片预览；成人或无许可媒体不得进入 OG/JSON-LD/搜索图片预览；私有母版永不公开。
- 重定向、404、400、503 必须与正文一致；故障响应 `no-store`，禁止软 200。
- 正式域名由已验证所有者在放行后提交 sitemap；不提交测试域，不伪造 hreflang、地区跳转或排名结论。

## 7. 验收包内容与签收

每批目录必须至少包含：人类可读报告、机器可读 manifest、原始关键 JSON、代码 SHA、镜像 ID、命令结果、环境边界、失败尝试、阻塞原因、解锁条件和清理记录。凭据、真实邮箱、浏览历史、未公开审核内容不得写入证据。

签收顺序：开发者自测 → 产品负责人按 AC 确认 → 运维/安全确认目标环境 → 有权限的所有者执行外部平台操作。缺任一角色时如实写“待签收”，不得代签。
