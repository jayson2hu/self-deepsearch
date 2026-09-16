import { ArrowRight, Building2, CalendarDays, History, Search, UsersRound } from "lucide-react";
import Link from "next/link";
import { AdSlot } from "../components/ad-slot";
import { PersonalizedWorkGrid } from "../components/personalized-work-grid";
import { getHome } from "../lib/api";

// Public configuration and catalog data are runtime-owned. Keeping the home
// route dynamic prevents build-time loopback URLs or synthetic data from being
// baked into the deployable image; the API fetch and edge still cache for 5m.
export const dynamic = "force-dynamic";

export default async function HomePage() {
  const { data, degraded } = await getHome();
  const latest = data.sections.find((section) => section.key === "latest")?.items ?? [];

  return (
    <main>
        <section className="search-band" aria-labelledby="search-title">
          <div className="shell search-band__inner">
            <div>
              <p className="eyebrow">先查询，再浏览</p>
              <h1 id="search-title">查询番号或名称，确认公开资料</h1>
              <p>现阶段返回作品结果；输入人物或厂牌名称也可查找关联作品。</p>
            </div>
            <form className="primary-search" action="/search" role="search">
              <label className="sr-only" htmlFor="primary-query">搜索番号、标题、人物或厂牌</label>
              <Search size={22} aria-hidden="true" />
              <input id="primary-query" name="q" placeholder="输入番号或名称" autoComplete="off" />
              <button type="submit">查询</button>
            </form>
          </div>
        </section>

        <div className="shell content-with-rails">
          <div className="ad-rail ad-rail--left"><AdSlot label="左侧展示位" placement="rail" /></div>
          <div className="content-column">
            <section className="section-block" aria-labelledby="latest-title">
              <div className="section-heading">
                <div>
                  <p className="section-kicker"><CalendarDays size={15} /> 主要目录 · 按实际发行日期</p>
                  <h2 id="latest-title">最新发行</h2>
                </div>
                <Link href="/latest">查看全部 <ArrowRight size={16} /></Link>
              </div>
              {degraded && <p className="degraded-note">目录服务暂时不可用，当前不展示作品或人物资料。</p>}
              <PersonalizedWorkGrid context="recommendation" items={latest} emptyText="暂无可推荐的已发布作品" />
            </section>

            <section className="section-block" aria-labelledby="browse-title">
              <div className="section-heading">
                <div>
                  <p className="section-kicker">关联浏览</p>
                  <h2 id="browse-title">继续查找资料</h2>
                </div>
              </div>
              <div className="browse-paths">
                <Link className="browse-path" href="/recent">
                  <History aria-hidden="true" size={20} />
                  <span><strong>最近收录</strong><small>包括发行日期暂缺的公开资料</small></span>
                  <ArrowRight aria-hidden="true" size={17} />
                </Link>
                <Link className="browse-path" href="/performers">
                  <UsersRound aria-hidden="true" size={20} />
                  <span><strong>人物资料</strong><small>按名称进入关联作品</small></span>
                  <ArrowRight aria-hidden="true" size={17} />
                </Link>
                <Link className="browse-path" href="/studios">
                  <Building2 aria-hidden="true" size={20} />
                  <span><strong>厂牌资料</strong><small>查看厂牌及其关联作品</small></span>
                  <ArrowRight aria-hidden="true" size={17} />
                </Link>
              </div>
            </section>

            <AdSlot label="正文后横幅" placement="banner" />
            <AdSlot label="信息流展示位" placement="infeed" />
          </div>
          <div className="ad-rail ad-rail--right"><AdSlot label="右侧展示位" placement="rail" /></div>
        </div>
    </main>
  );
}
