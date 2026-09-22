import { Clock3, RefreshCw } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { getLatestWorks } from "../../lib/api";
import { canonicalWithCursor, pageRobots } from "../../lib/seo-policy";
import styles from "../search/search.module.css";

export async function generateMetadata({ searchParams }: { searchParams: Promise<{ cursor?: string }> }): Promise<Metadata> {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getLatestWorks(cursor);
  const pageIndexable = !result.degraded && Boolean(result.data?.items.length);
  return { title: "最新发行", description: "按作品实际发行日期浏览最近发布的作品资料。", alternates: { canonical: canonicalWithCursor("/latest", pageIndexable ? cursor : "") }, robots: pageRobots(pageIndexable) };
}

export default async function LatestPage({ searchParams }: { searchParams: Promise<{ cursor?: string }> }) {
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getLatestWorks(cursor);
  const items = result.data?.items ?? [];
  return <main>
    <section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow">按实际发行日期排序</p><h1>最新发行</h1></div><p className="page-band__copy">只展示已经审核发布且有实际发行日期的作品资料。</p></div></section>
    <div className="shell content-with-rails">
      <div className="content-column"><section className="section-block">
        {result.degraded ? (
          <div className={`empty-state ${styles.serviceState}`} role="alert">
            <RefreshCw size={30} aria-hidden="true" />
            <strong>发行资料暂时无法加载</strong>
            <p>资料服务暂时不可用，请稍后重试。你的浏览位置已保留。</p>
            <form action="/latest" method="get">
              {cursor ? <input type="hidden" name="cursor" value={cursor} /> : null}
              <button className="button button--quiet" type="submit">重试加载</button>
            </form>
          </div>
        ) : items.length ? <><PersonalizedWorkGrid context="catalog" items={items} emptyText="暂无已发布作品" nativeAd={<AdSlot label="最新发行原生作品卡广告" placement="native" slot="latest.native_card" />} nativeAdPosition={8} /><AdSlot label="最新发行列表下方广告" placement="banner" slot="latest.after_grid" /></> : <div className="empty-state"><Clock3 size={30} aria-hidden="true" /><strong>暂无已发布作品</strong></div>}
        {!result.degraded && result.data?.next_cursor ? <Link className="button pagination-next" href={`/latest?cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</Link> : null}
      </section></div><div className="ad-rail ad-rail--right"><AdSlot label="最新目录右侧广告" placement="rail" slot="latest.desktop_right" /></div></div>
  </main>;
}
