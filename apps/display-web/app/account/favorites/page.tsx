import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { WorkCard } from "../../../components/work-card";
import { getCurrentUser, getFavorites } from "../../../lib/api";

export const metadata: Metadata = { title: "收藏作品", robots: { index: false, follow: false } };

export default async function FavoritesPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFavorites();
  return <main className="shell account-page"><header><p className="eyebrow"><Link href="/account">我的账号</Link></p><h1>收藏作品</h1></header>{data === null ? <div className="state-note state-note--warning" role="alert">收藏服务暂时不可用。<Link href="/account/favorites">重试</Link></div> : data.items.length ? <div className="work-grid private-grid">{data.items.map((item, index) => <WorkCard key={item.id} item={item} position={index + 1} />)}</div> : <div className="empty-state"><strong>还没有收藏作品</strong><p>从公开作品详情可以收藏；取消收藏也在对应详情页完成。</p><Link className="button button--quiet" href="/latest">浏览最新发行</Link></div>}</main>;
}
