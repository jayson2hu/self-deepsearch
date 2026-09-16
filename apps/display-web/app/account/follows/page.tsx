import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { PerformerCard } from "../../../components/performer-card";
import { getCurrentUser, getFollows } from "../../../lib/api";

export const metadata: Metadata = { title: "关注人物", robots: { index: false, follow: false } };

export default async function FollowsPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFollows();
  return <main className="shell account-page"><header className="account-heading"><div><p className="eyebrow"><Link href="/account">我的账号</Link></p><h1>关注人物</h1></div><Link className="button button--quiet" href="/account/follows/works">查看关注人物的已发布作品</Link></header>{data === null ? <div className="state-note state-note--warning" role="alert">关注服务暂时不可用。<Link href="/account/follows">重试</Link></div> : data.items.length ? <div className="performer-grid private-grid">{data.items.map((item) => <PerformerCard key={item.id} item={item} />)}</div> : <div className="empty-state"><strong>还没有关注人物</strong><p>从人物详情可以关注；取消关注也在对应详情页完成。</p><Link className="button button--quiet" href="/performers">浏览人物目录</Link></div>}</main>;
}
