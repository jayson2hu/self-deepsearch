import { ArrowRight, Building2, CalendarDays, History, RefreshCw, Search, UsersRound } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";
import { AdSlot } from "../components/ad-slot";
import { PersonalizedWorkGrid } from "../components/personalized-work-grid";
import { getHome } from "../lib/api";
import { pageRobots } from "../lib/seo-policy";
import styles from "./search/search.module.css";

// Public configuration and catalog data are runtime-owned. Keeping the home
// route dynamic prevents build-time loopback URLs or synthetic data from being
// baked into the deployable image; the API fetch and edge still cache for 5m.
export const dynamic = "force-dynamic";

export async function generateMetadata(): Promise<Metadata> {
  const { data, degraded } = await getHome();
  const hasPrimaryContent = Boolean(data.sections.find((section) => section.key === "latest")?.items.length);
  return { alternates: { canonical: "/" }, robots: pageRobots(!degraded && hasPrimaryContent) };
}

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
              <p>查询作品、人物与厂牌资料；人物搜索支持已收录的艺名和别名。</p>
            </div>
            <form className={styles.queryPanel} action="/search" method="get" role="search">
              <fieldset className={styles.typeChoice}>
                <legend className="sr-only">搜索类型</legend>
                <label className={styles.typeOption}><input type="radio" name="type" value="work" defaultChecked /><span>作品</span></label>
                <label className={styles.typeOption}><input type="radio" name="type" value="performer" /><span>人物</span></label>
                <label className={styles.typeOption}><input type="radio" name="type" value="studio" /><span>厂牌</span></label>
              </fieldset>
              <div className={`primary-search ${styles.queryBar}`}>
                <label className="sr-only" htmlFor="primary-query">搜索番号、标题、人物或厂牌</label>
                <Search size={22} aria-hidden="true" />
                <input id="primary-query" name="q" placeholder="输入番号或名称" autoComplete="off" />
                <button type="submit">查询</button>
              </div>
            </form>
          </div>
        </section>

        <div className="shell content-with-rails">
          <div className="content-column">
            <section className="section-block" aria-labelledby="latest-title">
              <div className="section-heading">
                <div>
                  <p className="section-kicker"><CalendarDays size={15} /> 主要目录 · 按实际发行日期</p>
                  <h2 id="latest-title">最新发行</h2>
                </div>
                <Link href="/latest">查看全部 <ArrowRight size={16} /></Link>
              </div>
              {degraded ? (
                <div className={`empty-state ${styles.serviceState}`} role="alert">
                  <RefreshCw size={30} aria-hidden="true" />
                  <strong>目录暂时无法加载</strong>
                  <p>资料服务暂时不可用，请稍后重试。</p>
                  <form action="/" method="get"><button className="button button--quiet" type="submit">重试加载</button></form>
                </div>
              ) : <PersonalizedWorkGrid context="recommendation" items={latest} emptyText="暂无可推荐的已发布作品" nativeAd={<AdSlot label="首页推荐原生作品卡广告" placement="native" slot="home.recommendation_card" />} nativeAdPosition={4} />}
            </section>

            <AdSlot label="最新内容后广告" placement="banner" slot="home.after_latest" />

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

          </div>
          <div className="ad-rail ad-rail--right"><AdSlot label="首页右侧广告" placement="rail" slot="home.desktop_right" /></div>
        </div>
    </main>
  );
}
