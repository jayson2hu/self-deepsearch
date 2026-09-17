import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { formatDateTime } from "../../lib/date-time";
import { getOperator, getSEOInventorySummary, getSEOPages } from "../../lib/api";

type Search = { q?: string; entity_type?: string; status?: string; offset?: string };

export default async function SEOPage({ searchParams }: { searchParams: Promise<Search> }) {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const search = await searchParams;
  const params = new URLSearchParams({ limit: "25" });
  for (const key of ["q", "entity_type", "status", "offset"] as const) if (search[key]) params.set(key, search[key]!);
  const [summary, pages] = await Promise.all([getSEOInventorySummary(), getSEOPages(params.toString())]);
  return <AdminShell active="SEO 中心" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>统一查看发布资格、metadata、canonical、robots 与 sitemap 候选</p><h1>SEO 中心</h1></div><div className="heading-actions"><Link className="button" href="/seo/reports">搜索报表</Link><Link className="button" href="/seo/issues">优化问题</Link></div></div>
    {summary ? <><div className="metric-grid seo-metrics">
      <article><div><small>发布记录</small><strong>{summary.total}</strong><p>当前环境的正式 publication 清单</p></div></article>
      <article><div><small>技术可索引</small><strong>{summary.indexable}</strong><p>仅表示资格通过，不代表已被搜索引擎收录</p></div></article>
      <article><div><small>排除</small><strong>{summary.excluded}</strong><p>隐藏、下架、未知或质量不合格</p></div></article>
      <article><div><small>待分类</small><strong>{summary.unknown}</strong><p>unknown 一律 fail-closed，不放行 sitemap</p></div></article>
    </div><p className="seo-observed">统计时间 {formatDateTime(summary.observed_at)} · sitemap 候选 {summary.sitemap_candidates} · 排除原因 {Object.entries(summary.exclusion_reasons).map(([reason, count]) => `${reason} ${count}`).join("；") || "无"}</p></> : <div className="service-warning dashboard-section">无法读取 SEO 总览；不会把服务失败显示为 0。</div>}
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>页面资格清单</h2><p>标题和描述沿用公开站现行模板；内容整改通过既有修订流程完成。</p></div></div>
      <form className="seo-filters" method="get"><label>标题或 slug<input defaultValue={search.q ?? ""} name="q" /></label><label>实体<select defaultValue={search.entity_type ?? ""} name="entity_type"><option value="">全部</option><option value="work">作品</option><option value="performer">人物</option><option value="studio">厂牌</option></select></label><label>发布状态<select defaultValue={search.status ?? ""} name="status"><option value="">全部</option><option value="published">已发布</option><option value="hidden">已隐藏</option><option value="takedown">已下架</option></select></label><button type="submit">查询</button><Link className="button" href="/seo">重置</Link></form>
      {pages ? <><p className="user-result-count">共 {pages.total} 条；本页展示 {pages.items.length} 条</p><div className="table-wrap"><table className="seo-table"><thead><tr><th>页面</th><th>实际 metadata</th><th>资格</th><th>策略</th><th>更新时间</th><th>整改</th></tr></thead><tbody>
        {pages.items.map((item) => <tr key={`${item.entity_type}-${item.entity_id}`}><td><strong>{item.url_path}</strong><small>{item.entity_type} · {item.publication_status}</small></td><td><strong>{item.title}</strong><small>{item.description}</small><code>canonical {item.canonical}</code></td><td><span className={`status ${item.indexable ? "ok" : "waiting"}`}>{item.indexable ? "技术可索引" : exclusionLabel(item.exclusion_reason)}</span><small>sitemap {item.sitemap_eligible ? "候选" : "排除"}</small></td><td><code>{item.robots}</code><small>{item.content_classification} / {item.quality_status} / {item.media_policy}</small></td><td>{formatDateTime(item.updated_at)}</td><td><Link className="button compact-button" href={item.revision_href}>进入修订</Link></td></tr>)}
        {!pages.items.length ? <tr><td className="empty-state" colSpan={6}>当前筛选没有页面记录。</td></tr> : null}
      </tbody></table></div><nav className="user-pagination">{Number(search.offset ?? "0") > 0 ? <Link className="button" href={pageHref(search, Math.max(0, Number(search.offset ?? "0") - 25))}>上一页</Link> : null}{pages.next_offset !== null ? <Link className="button" href={pageHref(search, pages.next_offset)}>下一页</Link> : <span>已到最后一页</span>}</nav></> : <div className="service-warning">无法读取 SEO 页面清单。</div>}
    </section>
  </main></AdminShell>;
}

function pageHref(search: Search, offset: number) {
  const params = new URLSearchParams();
  for (const key of ["q", "entity_type", "status"] as const) if (search[key]) params.set(key, search[key]!);
  if (offset) params.set("offset", String(offset));
  return `/seo?${params.toString()}`;
}

function exclusionLabel(reason: string | null) {
  return ({ not_published: "未公开", classification_unknown: "分类未知", quality_unknown: "质量待审", quality_ineligible: "质量不合格" } as Record<string, string>)[reason ?? ""] ?? "已排除";
}
