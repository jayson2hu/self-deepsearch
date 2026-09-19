import type { HomeItem } from "@self-deepsearch/api-contracts";
import { Building2, Search, SearchX } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { PerformerCard } from "../../components/performer-card";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { searchPerformers, searchStudios, searchWorks, type PerformerSearchResult, type SearchEntityType, type StudioSearchResult, type WorkSearchResult } from "../../lib/api";

export const metadata: Metadata = { title: "资料搜索", robots: { index: false, follow: true } };

const searchTypes: Array<{ type: SearchEntityType; label: string }> = [
  { type: "work", label: "作品" },
  { type: "performer", label: "人物" },
  { type: "studio", label: "厂牌" },
];

function searchHref(query: string, cursor = "", type: SearchEntityType = "work") {
  const parameters = new URLSearchParams({ q: query });
  if (type !== "work") parameters.set("type", type);
  if (cursor) parameters.set("cursor", cursor);
  return `/search?${parameters.toString()}`;
}

type PageSearchResult = WorkSearchResult | PerformerSearchResult | StudioSearchResult | { status: "idle"; type: SearchEntityType } | { status: "invalid_type"; type: string };

export default async function SearchPage({ searchParams }: { searchParams: Promise<{ q?: string; cursor?: string; type?: string }> }) {
  const parameters = await searchParams;
  const query = parameters.q?.trim() ?? "";
  const cursor = parameters.cursor?.trim() ?? "";
  const requestedType = parameters.type?.trim() || "work";
  const validType = searchTypes.some((option) => option.type === requestedType);
  const activeType: SearchEntityType = validType ? requestedType as SearchEntityType : "work";
  const queryLength = Array.from(query).length;
  const validLength = queryLength >= 2 && queryLength <= 100;
  let result: PageSearchResult;
  if (!validType) result = { status: "invalid_type", type: requestedType };
  else if (!query) result = { status: "idle", type: activeType };
  else if (!validLength) result = { status: "invalid_query", type: activeType, data: null };
  else if (activeType === "performer") result = await searchPerformers(query, cursor);
  else if (activeType === "studio") result = await searchStudios(query, cursor);
  else result = await searchWorks(query, cursor);

  const displayedQuery = result.status === "ok" ? result.data.query : query;
  const typeLabel = searchTypes.find((option) => option.type === activeType)?.label ?? "作品";
  const workItems = result.status === "ok" && result.type === "work" ? result.data.items : [];
  const performerItems: HomeItem[] = result.status === "ok" && result.type === "performer" ? result.data.items.map((item) => ({
    id: item.id, type: "performer", title: item.name, subtitle: "人物资料", href: item.href,
    image_url: item.image_url, image: item.image,
  })) : [];
  const studioItems = result.status === "ok" && result.type === "studio" ? result.data.items : [];
  const itemCount = workItems.length + performerItems.length + studioItems.length;
  const nextCursor = result.status === "ok" ? result.data.next_cursor : null;
  const publishedTypeLabel = `仅返回已发布${typeLabel}资料`;
  const emptyResultLabel = `没有找到相关已发布${typeLabel}资料`;

  const scopeCopy = activeType === "work"
    ? "按番号、标题查询作品；人物艺名、已收录别名或厂牌名称会返回关联作品。查找人物资料请切换到“人物”。"
    : activeType === "performer"
      ? "只返回已发布人物资料；支持公开艺名、已收录的曾用名/别名、原文名及罗马字名，也可输入部分名称。未收录的别名不会自动猜测。"
      : "只返回厂牌资料；同名厂牌保持为不同资料，不会自动合并。";

  return (
    <main>
      <section className="page-band">
        <div className="shell page-band__inner">
          <p className="eyebrow">作品资料查询</p>
          <h1>资料搜索</h1>
          <form className="primary-search" action="/search" method="get" role="search">
            <label className="sr-only" htmlFor="result-query">输入作品番号、标题、人物艺名或厂牌</label>
            <Search size={22} aria-hidden="true" />
            <input id="result-query" name="q" defaultValue={query} placeholder={activeType === "performer" ? "输入艺名、曾用名或别名" : activeType === "studio" ? "输入厂牌名称" : "输入番号、标题或人物艺名"} autoComplete="off" />
            {validType && activeType !== "work" ? <input type="hidden" name="type" value={activeType} /> : null}
            <button type="submit">查询</button>
          </form>
        </div>
      </section>
      <div className="shell content-with-rails">
        <div className="content-column">
          <section className="section-block" aria-labelledby="results-title">
            <div className="section-heading"><div><p className="section-kicker">{publishedTypeLabel}</p><h2 id="results-title">{query ? `“${displayedQuery}” 的结果` : "输入关键词开始查询"}</h2></div></div>
            <nav className="filter-tabs" aria-label="搜索类型">
              {searchTypes.map((option) => <Link className={validType && option.type === activeType ? "active" : ""} href={searchHref(query, "", option.type)} key={option.type} aria-current={validType && option.type === activeType ? "page" : undefined}>{option.label}</Link>)}
            </nav>
            <p className="search-scope-note">{scopeCopy}</p>
            {result.status === "invalid_type" && (
              <div className="empty-state"><SearchX size={30} /><strong>搜索类型不符合要求</strong><span>没有向未知搜索端点发送请求；可返回作品、人物或厂牌类型重新查询。</span><Link className="button button--quiet" href={searchHref(query)}>返回作品搜索</Link></div>
            )}
            {result.status === "idle" && (
              <div className="empty-state"><Search size={30} /><strong>先输入要确认的{typeLabel}资料</strong><span>空查询不会加载整站结果；切换类型后再按对应名称或番号查询。</span></div>
            )}
            {result.status === "invalid_query" && (
              <div className="empty-state"><SearchX size={30} /><strong>关键词不符合查询要求</strong><span>请保留至少 2 个可检索的文字或数字，最多 100 个字符；已保留你的原输入。</span></div>
            )}
            {result.status === "invalid_cursor" && (
              <div className="empty-state"><SearchX size={30} /><strong>分页条件已失效</strong><span>查询词和类型仍已保留，请从第一页重新查询。</span><Link className="button button--quiet" href={searchHref(query, "", activeType)}>返回第一页</Link></div>
            )}
            {result.status === "unavailable" && (
              <div className="empty-state"><SearchX size={30} /><strong>查询服务暂时不可用</strong><span>这不是“没有结果”；可按原查询条件和类型重试。</span><Link className="button button--quiet" href={searchHref(query, cursor, activeType)}>重试查询</Link></div>
            )}
            {result.status === "ok" && itemCount === 0 && (
              <div className="empty-state"><SearchX size={30} /><strong>{emptyResultLabel}</strong><span>可修改查询词，或切换其他资料类型继续查询。</span><div className="empty-state__actions"><Link className="button button--quiet" href="/latest">最新作品</Link><Link className="button button--quiet" href="/performers">人物目录</Link><Link className="button button--quiet" href="/studios">厂牌目录</Link></div></div>
            )}
            {workItems.length > 0 && <PersonalizedWorkGrid context="catalog" items={workItems} emptyText="没有找到已发布资料" />}
            {performerItems.length > 0 ? <div className="performer-grid">{performerItems.map((item) => <PerformerCard item={item} key={item.id} />)}</div> : null}
            {studioItems.length > 0 ? <div className="studio-grid">{studioItems.map((item) => <Link className="studio-link" href={item.href} key={item.id}><Building2 size={19} /><strong>{item.name}</strong><span>厂牌资料</span></Link>)}</div> : null}
            {nextCursor ? <Link className="button pagination-next" href={searchHref(displayedQuery, nextCursor, activeType)}>下一页</Link> : null}
          </section>
        </div>
      </div>
    </main>
  );
}
