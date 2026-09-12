import { Archive } from "lucide-react";
import type { Metadata } from "next";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { getRecentlyAddedWorks } from "../../lib/api";

export const metadata: Metadata = { title: "最近收录", description: "按本站首次公开发布时间浏览最近收录的作品资料。" };

export default async function RecentPage({ searchParams }: { searchParams: Promise<{ cursor?: string }> }) {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getRecentlyAddedWorks(cursor);
  const items = result.data?.items ?? [];
  return <main>
    <section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow"><Archive size={14} /> 按本站首次公开时间</p><h1>最近收录</h1></div><p className="page-band__copy">发行日期缺失但已经审核发布的资料也会出现在这里。</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
      <div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><section className="section-block">{result.degraded ? <p className="state-note state-note--warning">资料服务暂时不可用。</p> : null}{items.length ? <PersonalizedWorkGrid items={items} emptyText="所有作品均已被你隐藏" /> : <div className="empty-state"><strong>暂无已发布作品</strong></div>}{result.data?.next_cursor ? <a className="button pagination-next" href={`/recent?cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}</section></div>
      <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
