import { Activity, Building2, CalendarDays, Ruler, UserRound } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { AdSlot } from "../../../components/ad-slot";
import { JsonLD } from "../../../components/json-ld";
import { PageViewReporter } from "../../../components/page-view-reporter";
import { PerformerActions } from "../../../components/performer-actions";
import { WorkCard } from "../../../components/work-card";
import { SafeImage } from "../../../components/safe-image";
import { getPerformer } from "../../../lib/api";
import { siteURL } from "../../../lib/site";

export async function generateMetadata({ params }: { params: Promise<{ slug: string }> }): Promise<Metadata> {
	const slug = (await params).slug;
	const result = await getPerformer(slug);
	const primaryImage = result.status === "ok" ? result.data.images[0] : undefined;
	const socialImage = primaryImage?.renditions?.find((rendition) => rendition.rendition === "w960") ?? primaryImage;
	const image = result.status === "ok" ? socialImage?.url ?? result.data.image_url : null;
	return result.status === "ok" ? {
		title: result.data.name, description: `${result.data.name} 的公开资料与作品列表。`,
		alternates: { canonical: `/performers/${slug}` },
		openGraph: { type: "profile", url: `/performers/${slug}`, title: result.data.name, description: `${result.data.name} 的公开资料与作品列表。`, images: image ? [{ url: image }] : undefined },
	} : { title: "人物资料", robots: { index: false, follow: false } };
}

export default async function PerformerDetailPage({ params }: { params: Promise<{ slug: string }> }) {
  const slug = (await params).slug;
  const result = await getPerformer(slug);
  if (result.status === "not_found") notFound();
  if (result.status === "degraded") return <main className="shell service-state"><h1>资料暂时无法加载</h1><p>服务暂时不可用，请稍后重试。</p><Link className="button button--quiet" href="/performers">返回人物列表</Link></main>;
  const performer = result.data;
  const primaryImage = performer.images[0];
  const primary = primaryImage?.url ?? performer.image_url;
  const canonicalURL = new URL(`/performers/${slug}`, siteURL()).toString();
  return <main><JsonLD data={{
    "@context": "https://schema.org", "@type": "Person", "@id": canonicalURL, url: canonicalURL,
    name: performer.name, alternateName: [performer.name_original, performer.romanized_name, ...performer.aliases].filter((value): value is string => Boolean(value)),
    image: primary ?? undefined,
    height: performer.height_cm ? { "@type": "QuantitativeValue", value: performer.height_cm, unitCode: "CMT" } : undefined,
    memberOf: performer.agency ? { "@type": "Organization", name: performer.agency } : undefined,
    mainEntityOfPage: canonicalURL,
  }} /><PageViewReporter contentType="performer" contentID={performer.id} /><div className="shell content-with-rails detail-layout"><div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
    <article className="content-column work-detail"><AdSlot label="顶部横幅" placement="banner" />
      <div className="work-detail__header performer-detail__header"><div className="work-detail__visual performer-detail__visual">
        <SafeImage src={primary || "/default-performer.svg"} renditions={primaryImage?.renditions} fallbackSrc="/default-performer.svg" sizes="(max-width: 760px) calc(100vw - 24px), 420px" alt={`${performer.name} 人物图`} loading="eager" fetchPriority="high" width={640} height={800} />
      </div><div className="work-detail__identity"><p className="work-card__code">人物公开资料</p><h1>{performer.name}</h1>
        {performer.name_original ? <p className="original-title">{performer.name_original}</p> : null}
        <dl className="fact-list">
          <div><dt><Activity size={16} />活动状态</dt><dd>{activityLabel(performer.activity_status)}</dd></div>
          <div><dt><Building2 size={16} />所属</dt><dd>{performer.agency ?? "待补充"}</dd></div>
          <div><dt><CalendarDays size={16} />出生年份 / 出道年份</dt><dd>{performer.birth_year ?? "待补充"} / {performer.debut_year ?? "待补充"}</dd></div>
          <div><dt><Ruler size={16} />身高 / 身体参数</dt><dd>{performer.height_cm ? `${performer.height_cm} cm` : "待补充"} / {performer.measurements ?? "待补充"}</dd></div>
          <div><dt><UserRound size={16} />罗马字</dt><dd>{performer.romanized_name ?? "待补充"}</dd></div>
          <div><dt>人物别名</dt><dd>{performer.aliases.length ? performer.aliases.join("、") : "待补充"}</dd></div>
        </dl>
        <div className="detail-actions"><PerformerActions performerID={performer.id} /></div>
      </div></div>
      <section className="detail-section"><h2>关联作品</h2>{performer.works.length ? <div className="work-grid">{performer.works.map((item, index) => <WorkCard item={item} key={item.id} position={index + 1} />)}</div> : <p>暂无已发布关联作品。</p>}</section>
      <AdSlot label="详情页信息流展示位" placement="infeed" />
    </article><div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div></div></main>;
}

function activityLabel(status: "active" | "retired" | "unknown") {
  return ({ active: "活动中", retired: "已引退", unknown: "待核实" } as const)[status];
}
