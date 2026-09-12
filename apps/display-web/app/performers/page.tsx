import { UsersRound } from "lucide-react";
import type { Metadata } from "next";
import { AdSlot } from "../../components/ad-slot";
import { PerformerCard } from "../../components/performer-card";
import { getPerformers } from "../../lib/api";

export const metadata: Metadata = { title: "人物资料", description: "浏览已发布的人物公开资料与关联作品。" };

export default async function PerformersPage({ searchParams }: { searchParams: Promise<{ cursor?: string }> }) {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getPerformers(cursor);
  const items = result.data?.items ?? [];
  return <main>
    <section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow">仅展示已审核公开资料</p><h1>人物资料</h1></div><p className="page-band__copy">按最近发布顺序浏览人物与关联作品。</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
      <div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><section className="section-block">
        {result.degraded ? <p className="state-note state-note--warning">人物资料暂时无法加载。</p> : null}
        {!result.degraded && items.length === 0 ? <div className="empty-state"><UsersRound size={30} /><strong>暂无已发布人物资料</strong></div> : null}
        {items.length ? <div className="performer-grid">{items.map((item) => <PerformerCard item={item} key={item.id} />)}</div> : null}
        {result.data?.next_cursor ? <a className="button pagination-next" href={`/performers?cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}
      </section></div><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
