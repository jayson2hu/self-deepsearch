import { Building2 } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { AdSlot } from "../../../components/ad-slot";
import { JsonLD } from "../../../components/json-ld";
import { PageViewReporter } from "../../../components/page-view-reporter";
import { WorkCard } from "../../../components/work-card";
import { getStudio } from "../../../lib/api";
import { siteURL } from "../../../lib/site";

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
  const slug = (await params).slug;
  const result = await getStudio(slug);
  return result.status === "ok" ? { title: result.data.name, description: `${result.data.name} 的公开厂牌资料与作品列表。`, alternates: { canonical: `/studios/${slug}` }, openGraph: { url: `/studios/${slug}`, title: result.data.name, description: `${result.data.name} 的公开厂牌资料与作品列表。` } } : { title: "厂牌资料", robots: { index: false, follow: false } };
}

export default async function StudioPage({ params }: { params: Promise<{ slug: string }> }) {
  const slug = (await params).slug;
  const result = await getStudio(slug);
  if (result.status === "not_found") notFound();
  if (result.status === "degraded") return <main className="shell service-state"><h1>资料暂时无法加载</h1><p>服务暂时不可用，请稍后重试。</p><Link className="button button--quiet" href="/studios">返回厂牌列表</Link></main>;
  const canonicalURL = new URL(`/studios/${slug}`, siteURL()).toString();
  return <main><JsonLD data={{
    "@context": "https://schema.org", "@type": "Organization", "@id": canonicalURL,
    url: canonicalURL, name: result.data.name, mainEntityOfPage: canonicalURL,
  }} /><PageViewReporter contentType="studio" contentID={result.data.id} /><section className="page-band"><div className="shell page-band__inner"><div><p className="eyebrow"><Building2 size={16} />厂牌公开资料</p><h1>{result.data.name}</h1></div><p className="page-band__copy">共 {result.data.works.length} 部已发布作品</p></div></section>
    <div className="shell content-with-rails"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div><div className="content-column"><AdSlot label="顶部横幅" placement="banner" /><section className="section-block"><div className="section-heading"><div><h2>关联作品</h2></div></div>
      {result.data.works.length ? <div className="work-grid">{result.data.works.map((item, index) => <WorkCard item={item} key={item.id} position={index + 1} />)}</div> : <div className="empty-state"><strong>暂无已发布作品</strong></div>}
    </section></div><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div>
  </main>;
}
