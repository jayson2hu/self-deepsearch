# 资源整理与 SEO／代码优化

日期：2026-10-17（Asia/Shanghai）。范围为现有资源盘点、离线整理及公开站技术 SEO；没有重新爬取第三方内容站，没有把待审核数据发布，也未修改在线容器、生产配置或正式索引开关。

## 资源取得与整理结果

| 资源 | 实际结果 | 边界 |
| --- | --- | --- |
| 已有固定人物归档 | 4,196 条人物、130 条头像候选，804 个文件完整性检查通过 | 从同级采集项目只读获取；不是本轮新抓取，也不是已授权公开数据 |
| 派生资源清单 | 4,196 条；无头像 4,066 条，无独立别名 4,196 条；规范姓名碰撞组 0 | 不自动补图、翻译艺名或按姓名合并；不是人物身份唯一性的证明 |
| 官方技术资料 | 实际取得 Google 成人内容、canonical 和 Sitemaps 协议三份文档，均为 HTTP 200 | 仅技术资料，不代表内容使用许可、合规认证或排名保证 |

固定归档 manifest SHA-256：`ddc94eaf953efe1bfbb42cbd550d09b7e97de0c3a1d01253e9246cc100fa109d`。
归档日期为 2026-09-19，不将本轮读取时间当作新的来源观测时间。

新增 [organize_resources.py](../scripts/organize_resources.py)，通过既有固定归档验证器读取来源：

- 保留原姓名、别名、来源 URL、观测时间及原内容 hash；NFKC、空白及别名去重只写入派生字段。
- 来源 ID 和 external_id 决定来源身份；同名只标记，不合并实体。
- 头像只登记候选 hash、尺寸和 ID，不复制图片，不授予展示或社交预览权限。
- 标记缺头像、无独立别名、过期／未来观测及同名复核；这些是补全提示，不冒充已经完成的资料。
- 所有记录仍为 staging、成年未确认、rights needs_review、indexable=false；形成权利、成年、编审、SEO 分类和 SEO 质量五项待办。
- 输出目录独占创建且权限为 0700，禁止覆盖原归档或已有审核结果；不访问外网、不写数据库。

本机输出位于 `.cache/resource-audit/2026-10-17/`，包含 `inventory.jsonl`、`summary.json` 和使用说明；不公开服务、不进入 Git。使用相同日期重复整理，两份清单及报告逐字节一致。
清单 SHA-256：`987537449ec6474fb2d209fdd3f2d22d4d8172a003944054718ce380cc1181c9`。

复现时必须提供一个**尚不存在**的输出目录：

```bash
npm run organize:resources -- --output .cache/resource-audit/my-new-review --as-of 2026-10-17
```

## SEO 和代码变更

1. **作品页描述**：改为根据实际番号、标题、发行日期、厂牌和人物生成独立描述，HTML 与 Open Graph 共用；缺失字段不编造，最多 180 个 Unicode 码点，避免截断 emoji。180 是本项目的编辑上限，不是 Google 的固定字符要求。
2. **图片预览一致性**：作品／人物 Open Graph、JSON-LD 同时检查环境、页面审核策略与单图 external_preview_allowed。即使上游图标记与页面策略冲突，成人、未知、不适格和测试环境均不输出外部预览图片；站内已获权图片仍按既有显示规则展示。禁止外部预览是项目自己的更保守政策，不声称是 Google 统一要求。
3. **索引入口**：保留 production 与两个显式开关，同时拒绝所有 IP 字面量（含简写、整数、十六进制、IPv6）、内部／保留测试后缀、凭据和非 origin URL。该检查只验证配置语法，不代替真实 DNS、TLS、域名归属和生产放行。
4. **sitemap 限额**：在分配分片前拒绝非有限数、负数、小数、非安全整数及超过 50,000 个索引项的计数；同时检查索引 XML 的非压缩字节数。异常返回 503/no-store/Retry-After，不生成无限数组或静默截断。
5. **sitemap 路径**：只接收与 Go API 一致的 slug 格式，拒绝路径、查询和片段注入；每批复用同一 origin，不再每条重复解析。异常源数据不输出损坏的 sitemap。
6. **归档校验**：媒体关系校验由逐项线性查找改成 ID 字典查找，整体由 O(M²) 改为 O(M)；同时拒绝重复导出条目。未测量端到端加速比例。
7. **错误响应复用**：索引／分片共享 sitemap 源不可用响应，统一重试与禁止缓存语义。

## 实际取得的技术依据

来源、HTTP 状态、页面更新时间及下载字节 hash 记录在 [reference-resources.json](../data/seo/reference-resources.json)。原始文档只缓存到 `.cache/resource-audit/references/`，不在仓库复制网页正文。

- [Google 成人内容指南](https://developers.google.cn/search/docs/specialty/explicit/guidelines?hl=zh-cn)：真实露骨内容页面可使用 `rating=adult` 标记；本项目继续按经审核的分类输出。未加入凭 User-Agent 绕过年龄或访问验证的机制，也未接入视频功能。
- [Google canonical 文档](https://developers.google.cn/search/docs/crawling-indexing/consolidate-duplicate-urls?hl=zh-cn)：保持绝对 canonical、内部链接及 sitemap 一致；不同内容的合法分页仍保留自己的 canonical，不把所有分页指向首页。
- [Sitemaps 协议](https://www.sitemaps.org/protocol.html)：URL 集和索引均有 50,000 项、50 MiB 非压缩大小限制，值需要 XML 转义，文件使用 UTF-8；本轮补齐索引端保护。

Google 国际站直连超时；既有代理请求失败（curl 56），未关闭 TLS 校验或打印代理凭据。随后通过 Google 官方中国站成功取得前两份文档。Google 中国站 build-sitemap 请求亦超时，因此数量／大小依据改用协议官网；不将失败请求记录成已取得资料。

## 验证记录

| 检查 | 实际结果与范围 |
| --- | --- |
| Node 全量回归 | 238 项通过，0 失败、0 跳过；含新增 7 项 SEO 保护测试 |
| Python 全量回归 | 349 项及 380 个子测试通过；含新增 8 项资源整理测试 |
| 资源实测 | 804 文件验证、SQLite 完整性通过；4,196 条清单、两次输出一致、原归档未改 |
| TypeScript／ESLint／Ruff | 两站及共享包类型检查、公开站 ESLint 和项目 Python Ruff 通过 |
| 公开站生产构建 | 通过；未部署 |
| 既有真实 HTTP 冒烟 | 12 组通过；含三类详情、canonical、robots、sitemap、安全头和错误状态；合成 API |
| 新增专项真实 HTTP | 9 场景通过：允许预览、成人／未知／不适格禁止预览、测试与 IP 入口阻断、负计数、超限索引、非法 slug；每个场景复制独立 Next 运行目录，结束后清理 |
| 额外 Go→Next 缓存合同 | 未完成：当前环境找不到 Go 编译器；失败证据保留，不能计为通过 |

专项结果写入 `.cache/resource-audit/seo-http-result.json`；额外缓存合同原始失败保留在 [独立失败报告](evidence/resource-seo-cache-contract-2026-10-17.json)。本轮不是数据库发布验收、完整浏览器回归、海外法律合规认证或搜索引擎收录验收。

```bash
npm run test:unit
PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 .venv/bin/python -m pytest -q
npm run typecheck
npm run lint --workspace @self-deepsearch/display-web
.venv/bin/ruff check workers/collector-python workers/media-python db/tests infra/backup infra/reverse-proxy infra/security scripts
npm run build --workspace @self-deepsearch/display-web
npm run smoke:frontend:release-a
npm run test:seo:http
```

## 继续推进需要的输入

新增外站资源需要明确来源网址或数据包、允许获取和展示的范围；已有归档不能替代授权。项目继续只处理公开作品／人物元数据与有权图片，不采集视频、音频、磁力或下载入口。

正式 SEO 仍需要目标语言／地区、真实域名及 HTTPS、可发布资料的权利／成年／编审结论和 Search Console 所有者权限。上述条件未满足前，不打开正式索引、不替用户提交站点地图。当前只优化既有中文站，不虚构英文译名、hreflang、站点评价或外链。
