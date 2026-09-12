import { Search, SearchX } from "lucide-react";
import type { Metadata } from "next";
import { AdSlot } from "../../components/ad-slot";
import { PersonalizedWorkGrid } from "../../components/personalized-work-grid";
import { searchWorks } from "../../lib/api";

export const metadata: Metadata = { title: "作品搜索", robots: { index: false, follow: true } };

export default async function SearchPage({ searchParams }: { searchParams: Promise<{ q?: string; cursor?: string }> }) {
  const parameters = await searchParams;
  const query = parameters.q?.trim() ?? "";
  const cursor = parameters.cursor?.trim() ?? "";
  const valid = query.length >= 2 && query.length <= 100;
  const result = valid ? await searchWorks(query, cursor) : { data: null, degraded: false };
  const items = result.data?.items ?? [];

  return (
    <main>
      <section className="page-band">
        <div className="shell page-band__inner">
          <p className="eyebrow">作品资料查询</p>
          <h1>作品搜索</h1>
          <form className="primary-search" action="/search" role="search">
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
            <div className="section-heading"><div><p className="section-kicker">仅检索已发布资料</p><h2 id="results-title">{query ? `“${query}” 的结果` : "输入关键词开始查询"}</h2></div></div>
            {!valid && query && <p className="state-note state-note--warning">请输入 2 至 100 个字符的关键词。</p>}
            {result.degraded && <p className="state-note state-note--warning">查询服务暂时不可用，请稍后重试。</p>}
            {valid && !result.degraded && items.length === 0 && (
              <div className="empty-state"><SearchX size={30} /><strong>没有找到已发布资料</strong><span>可尝试番号、标题、人物别名或厂牌名称。</span></div>
            )}
            {items.length > 0 && <PersonalizedWorkGrid items={items} emptyText="匹配的作品均已被你隐藏" />}
            {result.data?.next_cursor ? <a className="button pagination-next" href={`/search?q=${encodeURIComponent(query)}&cursor=${encodeURIComponent(result.data.next_cursor)}`}>下一页</a> : null}
          </section>
        </div>
        <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div>
      </div>
    </main>
  );
}
