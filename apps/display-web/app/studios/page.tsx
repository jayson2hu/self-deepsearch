import { Building2 } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { AdSlot } from "../../components/ad-slot";
import { getStudios } from "../../lib/api";

export const metadata: Metadata = { title: "厂牌资料", description: "浏览已发布厂牌及其关联作品。", alternates: { canonical: "/studios" } };

export default async function StudiosPage({ searchParams }: { searchParams: Promise<{ cursor?: string }> }) {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getStudios(cursor);
  const items = result.data?.items ?? [];
  return <main><section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow">仅展示已审核公开资料</p><h1>厂牌 / 制作方</h1></div><p className="page-band__copy">浏览厂牌及其已发布作品。</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div><div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><section className="section-block">
      {result.degraded ? <p className="state-note state-note--warning">厂牌资料暂时无法加载。</p> : null}
      {!result.degraded && !items.length ? <div className="empty-state"><Building2 size={30} /><strong>暂无已发布厂牌资料</strong></div> : null}
      {items.length ? <div className="studio-grid">{items.map((item) => <Link className="studio-link" href={item.href} key={item.id}><Building2 size={19} /><strong>{item.name}</strong><span>查看作品</span></Link>)}</div> : null}
      {result.data?.next_cursor ? <a className="button pagination-next" href={`/studios?cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}
    </section></div><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
