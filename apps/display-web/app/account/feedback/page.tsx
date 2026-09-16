import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { getCurrentUser, getFeedback } from "../../../lib/api";
import { formatDateTime } from "../../../lib/date-time";
import { feedbackStatusLabels, feedbackTypeLabels } from "../../../lib/feedback-labels";

export const metadata: Metadata = { title: "我的反馈", robots: { index: false, follow: false } };

export default async function FeedbackPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getFeedback();
  return <main className="shell account-page"><header><p className="eyebrow"><Link href="/account">我的账号</Link></p><h1>我的反馈</h1><p>这里只展示你提交的内容与公开处理状态；内部审核备注和其他用户反馈不会下发。</p></header>{data === null ? <div className="state-note state-note--warning" role="alert">反馈记录暂时不可用。<Link href="/account/feedback">重试</Link></div> : data.items.length ? <div className="feedback-list">{data.items.map((item) => <article key={item.id}><div><strong>{feedbackTypeLabels[item.feedback_type]}</strong><span>{feedbackStatusLabels[item.review_status]}</span></div><p>{item.message}</p><time dateTime={item.created_at}>{formatDateTime(item.created_at)}</time></article>)}</div> : <div className="empty-state"><strong>暂无反馈记录</strong><p>在作品详情提交反馈后，可回到这里查看处理状态。</p></div>}</main>;
}
