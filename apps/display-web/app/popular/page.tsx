import { BarChart3 } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { getPopularWorks } from "../../lib/api";

export const metadata: Metadata = { title: "热门浏览", description: "按公开详情页聚合浏览次数浏览作品资料。", robots: { index: false, follow: true } };

const ranges = [
  { key: "7d", label: "近 7 天" },
  { key: "30d", label: "近 30 天" },
  { key: "all", label: "历史累计" },
] as const;

export default async function PopularPage({ searchParams }: { searchParams: Promise<{ range?: string; cursor?: string }> }) {
  const query = await searchParams;
  const selected = ranges.some((range) => range.key === query.range) ? query.range as "7d" | "30d" | "all" : "30d";
  const cursor = query.cursor?.trim() ?? "";
  const result = await getPopularWorks(selected, cursor);
  return <main>
    <section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow"><BarChart3 size={14} /> 按小时聚合浏览次数</p><h1>热门浏览</h1></div><p className="page-band__copy">只展示已审核发布的作品。匿名浏览只进入小时聚合，不保存用户标识。</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
      <div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><nav className="filter-tabs" aria-label="排行时间范围">{ranges.map((range) => <Link className={selected === range.key ? "active" : ""} href={`/popular?range=${range.key}`} key={range.key}>{range.label}</Link>)}</nav>
        <section className="section-block">{result.degraded ? <p className="state-note state-note--warning">排行服务暂时不可用。</p> : null}{result.data?.items.length ? <PersonalizedWorkGrid items={result.data.items} emptyText="所有作品均已被你隐藏" /> : <div className="empty-state"><strong>暂无足够的浏览数据</strong></div>}{result.data?.next_cursor ? <a className="button pagination-next" href={`/popular?range=${selected}&cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}</section>
      </div><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
