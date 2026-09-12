import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { PerformerCard } from "../../../components/performer-card";
import { getCurrentUser, getFollows } from "../../../lib/api";

export const metadata: Metadata = { title: "关注人物", robots: { index: false, follow: false } };

export default async function FollowsPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFollows();
  return <main className="shell account-page"><header><p className="eyebrow">个人资料</p><h1>关注人物</h1></header>{data === null ? <div className="state-note state-note--warning" role="alert">关注服务暂时不可用，请稍后重试。</div> : data.items.length ? <div className="performer-grid private-grid">{data.items.map((item) => <PerformerCard key={item.id} item={item} />)}</div> : <div className="empty-state"><strong>还没有关注人物</strong></div>}</main>;
}
