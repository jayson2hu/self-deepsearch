import { ArrowRight, BarChart3, CalendarDays, Flame, Search, Sparkles, UsersRound } from "lucide-react";
import Link from "next/link";
import { AdSlot } from "../components/ad-slot";
import { DiscoveryGrid } from "../components/discovery-grid";
import { FollowedWorksSection } from "../components/followed-works-section";
import { PerformerCard } from "../components/performer-card";
import { PersonalizedWorkGrid } from "../components/personalized-work-grid";
import { getHome } from "../lib/api";

// Public configuration and catalog data are runtime-owned. Keeping the home
// route dynamic prevents build-time loopback URLs or synthetic data from being
// baked into the deployable image; the API fetch and edge still cache for 5m.
export const dynamic = "force-dynamic";

export default async function HomePage() {
  const { data, degraded } = await getHome();
  const discovery = data.sections.find((section) => section.key === "discovery")?.items ?? [];
  const latest = data.sections.find((section) => section.key === "latest")?.items ?? [];
  const editorial = data.sections.find((section) => section.key === "editorial")?.items ?? [];
  const performers = data.sections.find((section) => section.key === "performers")?.items ?? [];
  const trending = data.sections.find((section) => section.key === "trending")?.items ?? [];
  const mostViewed = data.sections.find((section) => section.key === "most_viewed")?.items ?? [];

  return (
    <main>
        <section className="search-band" aria-labelledby="search-title">
          <div className="shell search-band__inner">
            <div>
              <p className="eyebrow">作品资料查询</p>
              <h1 id="search-title">按番号找到作品公开资料</h1>
              <p>支持连字符、空格和大小写差异。搜索结果仅包含已经审核发布的资料。</p>
            </div>
            <form className="primary-search" action="/search" role="search">
              <label className="sr-only" htmlFor="primary-query">搜索番号、标题或人物</label>
              <Search size={22} aria-hidden="true" />
              <input id="primary-query" name="q" placeholder="例如 TEST-001" autoComplete="off" />
              <button type="submit">查询</button>
            </form>
          </div>
        </section>

        <div className="shell content-with-rails">
          <div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
          <div className="content-column">
            <AdSlot label="顶部横幅" placement="banner" />

            <section className="section-block" aria-labelledby="discovery-title">
              <div className="section-heading"><div><p className="section-kicker"><Sparkles size={15} /> 最新作品与人物混排</p><h2 id="discovery-title">发现</h2></div></div>
              <DiscoveryGrid items={discovery} />
            </section>

            <section className="section-block" aria-labelledby="latest-title">
              <div className="section-heading">
                <div>
                  <p className="section-kicker"><CalendarDays size={15} /> 按实际发行日期</p>
                  <h2 id="latest-title">最新发行</h2>
                </div>
                <Link href="/latest">查看全部 <ArrowRight size={16} /></Link>
              </div>
              {degraded && <p className="degraded-note">目录服务暂时不可用，当前不展示作品或人物资料。</p>}
              <PersonalizedWorkGrid items={latest} emptyText="暂无已发布作品" />
            </section>

            {editorial.length > 0 ? <section className="section-block" aria-labelledby="editorial-title">
              <div className="section-heading">
                <div><p className="section-kicker"><Sparkles size={15} /> 运营人工维护</p><h2 id="editorial-title">编辑推荐</h2></div>
              </div>
              <PersonalizedWorkGrid items={editorial} emptyText="暂无编辑推荐" />
            </section> : null}

            <FollowedWorksSection />

            <AdSlot label="信息流展示位" placement="infeed" />

            <section className="section-block" aria-labelledby="performers-title">
              <div className="section-heading">
                <div><p className="section-kicker"><UsersRound size={15} /> 继续浏览</p><h2 id="performers-title">人物资料</h2></div>
                <Link href="/performers">查看全部 <ArrowRight size={16} /></Link>
              </div>
              {performers.length > 0 ? (
                <div className="performer-grid">{performers.map((item) => <PerformerCard key={item.id} item={item} />)}</div>
              ) : (
                <div className="empty-copy">暂无已发布人物资料</div>
              )}
            </section>

            <section className="section-block" aria-labelledby="trending-title">
              <div className="section-heading">
                <div><p className="section-kicker"><Flame size={15} /> 最近 7 天详情请求</p><h2 id="trending-title">近期热门</h2></div>
              </div>
              {trending.length > 0 ? (
                <PersonalizedWorkGrid items={trending} emptyText="暂无足够的近期浏览数据" />
              ) : (
                <div className="empty-copy">暂无足够的近期浏览数据</div>
              )}
            </section>

            <section className="section-block" aria-labelledby="most-viewed-title">
              <div className="section-heading"><div><p className="section-kicker"><BarChart3 size={15} /> 最近 30 天详情请求</p><h2 id="most-viewed-title">浏览最多</h2></div><Link href="/popular">查看排行 <ArrowRight size={16} /></Link></div>
              {mostViewed.length > 0 ? <PersonalizedWorkGrid items={mostViewed} emptyText="暂无足够的浏览数据" /> : <div className="empty-copy">暂无足够的浏览数据</div>}
            </section>
          </div>
          <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div>
        </div>
    </main>
  );
}
