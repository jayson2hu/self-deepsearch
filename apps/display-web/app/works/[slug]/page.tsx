import { CalendarDays, UserRound } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { AdSlot } from "../../../components/ad-slot";
import { JsonLD } from "../../../components/json-ld";
import { PageViewReporter } from "../../../components/page-view-reporter";
import { PersonalizedWorkGrid } from "../../../components/personalized-work-grid";
import { WorkActions } from "../../../components/work-actions";
import { SafeImage } from "../../../components/safe-image";
import { getWork } from "../../../lib/api";
import { siteURL } from "../../../lib/site";

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
	const slug = (await params).slug;
	const result = await getWork(slug);
  if (result.status !== "ok") return { title: "作品资料", robots: { index: false, follow: false } };
  const primaryImage = result.data.images[0];
  const socialImage = primaryImage?.renditions?.find((rendition) => rendition.rendition === "w960") ?? primaryImage;
  return {
		title: `${result.data.code} ${result.data.title}`,
		description: `${result.data.code} 的公开发行与人物资料。`,
		alternates: { canonical: `/works/${slug}` },
		openGraph: {
			type: "article", url: `/works/${slug}`, title: `${result.data.code} ${result.data.title}`,
			description: `${result.data.code} 的公开发行与人物资料。`,
			images: socialImage ? [{ url: socialImage.url, width: socialImage.width, height: socialImage.height }] : undefined,
		},
	};
}

export default async function WorkDetailPage({ params }: { params: Promise<{ slug: string }> }) {
  const slug = (await params).slug;
  const result = await getWork(slug);
  if (result.status === "not_found") notFound();

  if (result.status === "degraded") {
    return <main className="shell service-state"><h1>资料暂时无法加载</h1><p>服务暂时不可用，请稍后重试。</p><Link className="button button--quiet" href="/">返回发现页</Link></main>;
  }

  const work = result.data;
  const images = work.images.slice(0, 4);
  const primaryImage = images[0];
  const canonicalURL = new URL(`/works/${slug}`, siteURL()).toString();
  return (
    <main>
	  <JsonLD data={{
		"@context": "https://schema.org",
		"@type": "CreativeWork",
		"@id": canonicalURL,
		url: canonicalURL,
		identifier: work.code,
		name: work.title,
		alternateName: work.title_original ?? undefined,
		description: work.summary ?? `${work.code} 的公开发行与人物资料。`,
		datePublished: work.release_date ?? undefined,
		image: images.map((image) => image.url),
		publisher: work.studio_name ? { "@type": "Organization", name: work.studio_name } : undefined,
		contributor: work.performers.map((performer) => ({ "@type": "Person", name: performer.name })),
		isFamilyFriendly: false,
		audience: { "@type": "PeopleAudience", suggestedMinAge: 18 },
		mainEntityOfPage: canonicalURL,
	  }} />
	  <PageViewReporter contentType="work" contentID={work.id} />
      <div className="shell content-with-rails detail-layout">
        <div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
        <article className="content-column work-detail">
          <AdSlot label="顶部横幅" placement="banner" />
          <div className="work-detail__header">
            <div className="work-detail__visual">
              <SafeImage
                src={primaryImage?.url || "/default-work.svg"}
                renditions={primaryImage?.renditions}
                fallbackSrc="/default-work.svg"
                sizes="(max-width: 760px) calc(100vw - 24px), (max-width: 1050px) min(70vw, 640px), 540px"
                alt={`${work.code} 作品图`}
                loading="eager"
                fetchPriority="high"
                width={960}
                height={600}
              />
            </div>
            <div className="work-detail__identity">
              <p className="work-card__code">{work.code}</p>
              <h1>{work.title}</h1>
              {work.title_original && <p className="original-title">{work.title_original}</p>}
              <dl className="fact-list">
                <div><dt><CalendarDays size={16} />发行日期</dt><dd>{work.release_date ?? "待补充"}</dd></div>
                <div><dt>厂牌 / 制作方</dt><dd>{work.studio_name || "待补充"}</dd></div>
                <div><dt><UserRound size={16} />人物</dt><dd>{work.performers.length ? work.performers.map((person) => <Link key={person.id} href={person.href}>{person.name}</Link>) : "待补充"}</dd></div>
              </dl>
			  <div className="detail-actions"><WorkActions workID={work.id} /></div>
            </div>
          </div>
          {work.summary && <section className="detail-section"><h2>作品资料</h2><p>{work.summary}</p></section>}
          {images.length > 1 && <section className="detail-section"><h2>展示图片</h2><div className="detail-gallery">{images.slice(1).map((image) => <SafeImage key={`${image.url}-${image.rendition}`} src={image.url} renditions={image.renditions} fallbackSrc="/default-work.svg" alt="作品精选图" sizes="(max-width: 760px) calc(100vw - 24px), 420px" width={image.width} height={image.height} />)}</div></section>}
          <AdSlot label="详情页信息流展示位" placement="infeed" />
          {work.related_works.length > 0 && <section className="detail-section" aria-labelledby="related-works-title"><h2 id="related-works-title">相关作品</h2><PersonalizedWorkGrid items={work.related_works} emptyText="暂无可展示的相关作品" /></section>}
        </article>
        <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div>
      </div>
    </main>
  );
}
