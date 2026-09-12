import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { WorkCard } from "../../../components/work-card";
import { getCurrentUser, getFavorites } from "../../../lib/api";

export const metadata: Metadata = { title: "收藏作品", robots: { index: false, follow: false } };

export default async function FavoritesPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFavorites();
  return <main className="shell account-page"><header><p className="eyebrow">个人资料</p><h1>收藏作品</h1></header>{data === null ? <div className="state-note state-note--warning" role="alert">收藏服务暂时不可用，请稍后重试。</div> : data.items.length ? <div className="work-grid private-grid">{data.items.map((item, index) => <WorkCard key={item.id} item={item} position={index + 1} />)}</div> : <div className="empty-state"><strong>还没有收藏作品</strong></div>}</main>;
}
