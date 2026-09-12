import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { getCurrentUser, getFeedback } from "../../../lib/api";
import { formatDateTime } from "../../../lib/date-time";

export const metadata: Metadata = { title: "我的反馈", robots: { index: false, follow: false } };

export default async function FeedbackPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFeedback();
  return <main className="shell account-page"><header><p className="eyebrow">个人资料</p><h1>我的反馈</h1></header>{data === null ? <div className="state-note state-note--warning" role="alert">反馈记录暂时不可用，请稍后重试。</div> : data.items.length ? <div className="feedback-list">{data.items.map((item) => <article key={item.id}><div><strong>{item.feedback_type}</strong><span>{item.review_status}</span></div><p>{item.message}</p><time dateTime={item.created_at}>{formatDateTime(item.created_at)}</time></article>)}</div> : <div className="empty-state"><strong>暂无反馈记录</strong></div>}</main>;
}
