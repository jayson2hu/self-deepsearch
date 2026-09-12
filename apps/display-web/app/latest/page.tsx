import { Clock3 } from "lucide-react";
import type { Metadata } from "next";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { getLatestWorks } from "../../lib/api";

export const metadata: Metadata = { title: "最新发行", description: "按作品实际发行日期浏览最近发布的作品资料。" };

export default async function LatestPage({ searchParams }: { searchParams: Promise<{ cursor?: string }> }) {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getLatestWorks(cursor);
  const items = result.data?.items ?? [];
  return <main>
    <section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow">按实际发行日期排序</p><h1>最新发行</h1></div><p className="page-band__copy">只展示已经审核发布且有实际发行日期的作品资料。</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
      <div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><section className="section-block">
        {result.degraded ? <p className="state-note state-note--warning">资料服务暂时不可用，当前显示测试内容。</p> : null}
        {items.length ? <PersonalizedWorkGrid items={items} emptyText="所有作品均已被你隐藏" /> : <div className="empty-state"><Clock3 size={30} /><strong>暂无已发布作品</strong></div>}
        {result.data?.next_cursor ? <a className="button pagination-next" href={`/latest?cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}
      </section></div><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
