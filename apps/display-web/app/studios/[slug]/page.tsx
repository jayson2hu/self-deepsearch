import { Building2 } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { AdSlot } from "../../../components/ad-slot";
import { Breadcrumbs } from "../../../components/breadcrumbs";
import { JsonLD } from "../../../components/json-ld";
import { PageViewReporter } from "../../../components/page-view-reporter";
import { PublicUpdatedAt } from "../../../components/public-updated-at";
import { ViewStats } from "../../../components/view-stats";
import { WorkCard } from "../../../components/work-card";
import { getStudio, getStudioWorks } from "../../../lib/api";
import { canonicalWithCursor, pageRobots, resolvePublicSEOProfile } from "../../../lib/seo-policy";
import { siteURL } from "../../../lib/site";

export async function generateMetadata({ params, searchParams }: { params: Promise<{ slug: string }>; searchParams: Promise<{ cursor?: string }> }): Promise<Metadata> {
  const slug = (await params).slug;
  const cursor = (await searchParams).cursor?.trim() ?? "";
  const result = await getStudio(slug);
  const works = cursor && result.status === "ok" ? await getStudioWorks(slug, cursor) : null;
  const seo = resolvePublicSEOProfile(result.status === "ok" ? result.data.seo : null);
  const pageIndexable = seo.indexable && (!cursor || works?.status === "ok" && works.data.items.length > 0);
  const canonical = canonicalWithCursor(`/studios/${slug}`, pageIndexable ? cursor : "");
  return result.status === "ok" ? { title: result.data.name, description: `${result.data.name} 的公开厂牌资料与作品列表。`, alternates: { canonical }, robots: pageRobots(pageIndexable, seo.image_preview_mode), other: seo.adult_rating ? { rating: seo.adult_rating } : undefined, openGraph: { url: canonical, title: result.data.name, description: `${result.data.name} 的公开厂牌资料与作品列表。` } } : { title: "厂牌资料", robots: { index: false, follow: false } };
}

export default async function StudioPage({ params, searchParams }: { params: Promise<{ slug: string }>; searchParams: Promise<{ cursor?: string }> }) {
  const slug = (await params).slug;
  const cursor = (await searchParams).cursor ?? "";
  const [result, worksResult] = await Promise.all([getStudio(slug), getStudioWorks(slug, cursor)]);
  if (result.status === "not_found") notFound();
  if (result.status === "degraded") return <main className="shell service-state"><h1>资料暂时无法加载</h1><p>服务暂时不可用，请稍后重试。</p><Link className="button button--quiet" href="/studios">返回厂牌列表</Link></main>;
  if (worksResult.status === "not_found") notFound();
  const seo = resolvePublicSEOProfile(result.data.seo);
  const canonicalURL = new URL(`/studios/${slug}`, siteURL()).toString();
  return <main><JsonLD data={{
    "@context": "https://schema.org", "@type": "Organization", "@id": canonicalURL,
    url: canonicalURL, name: result.data.name, mainEntityOfPage: canonicalURL, isFamilyFriendly: seo.adult_rating ? false : undefined,
  }} /><PageViewReporter contentType="studio" contentID={result.data.id} /><section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow"><Building2 size={16} />厂牌公开资料</p><h1>{result.data.name}</h1><PublicUpdatedAt value={result.data.public_updated_at} /><ViewStats last30Days={result.data.view_count_30d} total={result.data.view_count_total} /></div><p className="page-band__copy">已发布关联作品按页展示</p></div></section>
    <div className="shell content-with-rails"><div className="content-column"><Breadcrumbs items={[{ label: "首页", href: "/" }, { label: "厂牌资料", href: "/studios" }, { label: result.data.name }]} /><AdSlot label="厂牌资料后广告" placement="infeed" slot="studio.after_summary" /><section className="section-block"><div className="section-heading"><div><h2>关联作品</h2></div></div>
      {worksResult.status === "invalid_cursor" ? <div className="empty-state"><strong>分页地址已失效</strong><p>请从关联作品第一页重新开始。</p><Link className="button button--quiet" href={`/studios/${slug}`}>返回第一页</Link></div> : null}
      {worksResult.status === "degraded" ? <div className="empty-state"><strong>关联作品暂时无法加载</strong><p>厂牌资料仍可浏览，请稍后重试作品列表。</p><Link className="button button--quiet" href={`/studios/${slug}${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`}>重试</Link></div> : null}
      {worksResult.status === "ok" && worksResult.data.items.length ? <div className="work-grid">{worksResult.data.items.map((item, index) => <WorkCard item={item} key={item.id} position={index + 1} />)}</div> : null}
      {worksResult.status === "ok" && !worksResult.data.items.length ? <div className="empty-state"><strong>{cursor ? "本页没有更多已发布作品" : "暂无已发布作品"}</strong></div> : null}
      {worksResult.status === "ok" && (cursor || worksResult.data.next_cursor) ? <nav className="pagination" aria-label="关联作品分页">{cursor ? <Link className="button button--quiet" href={`/studios/${slug}`}>第一页</Link> : null}{worksResult.data.next_cursor ? <Link className="button" href={`/studios/${slug}?cursor=${encodeURIComponent(worksResult.data.next_cursor)}`}>下一页</Link> : <span>已到最后一页</span>}</nav> : null}
    </section></div><div className="ad-rail ad-rail--right"><AdSlot label="厂牌详情右侧广告" placement="rail" slot="studio.desktop_right" /></div></div>
  </main>;
}
