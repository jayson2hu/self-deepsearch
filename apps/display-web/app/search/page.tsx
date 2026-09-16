import { Search, SearchX } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { searchWorks, type WorkSearchResult } from "../../lib/api";

export const metadata: Metadata = { title: "作品搜索", robots: { index: false, follow: true } };

function searchHref(query: string, cursor = "") {
  const parameters = new URLSearchParams({ q: query });
  if (cursor) parameters.set("cursor", cursor);
  return `/search?${parameters.toString()}`;
}

export default async function SearchPage({ searchParams }: { searchParams: Promise<{ q?: string; cursor?: string }> }) {
  const parameters = await searchParams;
  const query = parameters.q?.trim() ?? "";
  const cursor = parameters.cursor?.trim() ?? "";
  const queryLength = Array.from(query).length;
  const validLength = queryLength >= 2 && queryLength <= 100;
  const result: WorkSearchResult | { status: "idle" } = !query
    ? { status: "idle" }
    : validLength
      ? await searchWorks(query, cursor)
      : { status: "invalid_query", data: null };
  const items = result.status === "ok" ? result.data.items : [];
  const displayedQuery = result.status === "ok" ? result.data.query : query;

  return (
    <main>
      <section className="page-band">
        <div className="shell page-band__inner">
          <p className="eyebrow">作品资料查询</p>
          <h1>作品搜索</h1>
          <form className="primary-search" action="/search" method="get" role="search">
            <label className="sr-only" htmlFor="result-query">输入作品番号、标题、人物或厂牌</label>
            <Search size={22} aria-hidden="true" />
            <input id="result-query" name="q" defaultValue={query} placeholder="例如 TEST-001" autoComplete="off" />
            <button type="submit">查询</button>
          </form>
        </div>
      </section>
      <div className="shell content-with-rails">
        <div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
        <div className="content-column">
          <AdSlot label="顶部横幅" placement="banner" />
          <section className="section-block" aria-labelledby="results-title">
            <div className="section-heading"><div><p className="section-kicker">当前阶段仅返回已发布作品</p><h2 id="results-title">{query ? `“${displayedQuery}” 的结果` : "输入关键词开始查询"}</h2></div></div>
            <p className="search-scope-note">可按番号、标题查询作品；人物名称、人物别名或厂牌名称会返回关联作品，不会在本页冒充人物或厂牌结果。</p>
            {result.status === "idle" && (
              <div className="empty-state"><Search size={30} /><strong>先输入要确认的资料</strong><span>例如完整番号、作品标题，或人物与厂牌名称；空查询不会加载整站结果。</span></div>
            )}
            {result.status === "invalid_query" && (
              <div className="empty-state"><SearchX size={30} /><strong>关键词不符合查询要求</strong><span>请保留至少 2 个可检索的文字或数字，最多 100 个字符；已保留你的原输入。</span></div>
            )}
            {result.status === "invalid_cursor" && (
              <div className="empty-state"><SearchX size={30} /><strong>分页条件已失效</strong><span>查询词仍已保留，请从第一页重新查询。</span><Link className="button button--quiet" href={searchHref(query)}>返回第一页</Link></div>
            )}
            {result.status === "unavailable" && (
              <div className="empty-state"><SearchX size={30} /><strong>查询服务暂时不可用</strong><span>这不是“没有结果”；可按原查询条件重试。</span><Link className="button button--quiet" href={searchHref(query, cursor)}>重试查询</Link></div>
            )}
            {result.status === "ok" && items.length === 0 && (
              <div className="empty-state"><SearchX size={30} /><strong>没有找到相关已发布作品</strong><span>可修改番号或名称，也可前往最新作品、人物或厂牌目录继续浏览。</span><div className="empty-state__actions"><Link className="button button--quiet" href="/latest">最新作品</Link><Link className="button button--quiet" href="/performers">人物目录</Link><Link className="button button--quiet" href="/studios">厂牌目录</Link></div></div>
            )}
            {items.length > 0 && <PersonalizedWorkGrid context="catalog" items={items} emptyText="没有找到已发布资料" />}
            {result.status === "ok" && result.data.next_cursor ? <Link className="button pagination-next" href={searchHref(result.data.query, result.data.next_cursor)}>下一页</Link> : null}
          </section>
        </div>
        <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div>
      </div>
    </main>
  );
}
