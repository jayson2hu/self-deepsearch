import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { WorkCard } from "../../../../components/work-card";
import { getCurrentUser, getFollowedWorks } from "../../../../lib/api";

export const metadata: Metadata = { title: "关注人物的已发布作品", robots: { index: false, follow: false } };

export default async function FollowedWorksPage() {
  if (!await getCurrentUser()) redirect("/login?returnTo=%2Faccount%2Ffollows%2Fworks");
  const data = await getFollowedWorks();
  return <main className="shell account-page"><header><p className="eyebrow"><Link href="/account/follows">关注人物</Link></p><h1>关注人物的已发布作品</h1><p>当前接口最多展示按发行日期排序的 12 条作品，卡片日期是作品发行日期。接口不公开本站收录时间，因此这里不会标记“新收录”、未读或新消息数量。</p></header>
    {data === null ? <div className="state-note state-note--warning" role="alert">关注作品暂时不可用。<Link href="/account/follows/works">重试</Link></div> : data.items.length ? <div className="work-grid private-grid">{data.items.map((item, index) => <WorkCard key={item.id} item={item} position={index + 1} />)}</div> : <div className="empty-state"><strong>关注人物暂无已发布关联作品</strong><p>这表示当前公开资料中没有可展示作品，不是未读数量为零。</p><Link className="button button--quiet" href="/account/follows">返回关注人物</Link></div>}
  </main>;
}
