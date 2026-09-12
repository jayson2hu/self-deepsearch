import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { FeedbackReviewQueue } from "../../components/feedback-review-queue";
import { getFeedbackReviewQueue, getOperator } from "../../lib/api";

export default async function FeedbackPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  const feedback = await getFeedbackReviewQueue();
  return <AdminShell active="反馈审核" user={user}><main className="main-content">
    <div className="page-heading"><div><p>证据链接仅展示，不由后台自动访问</p><h1>反馈审核</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>用户提交</h2><p>editor 可领取处理，admin/owner 可完成审核，所有状态变化记录审计理由。</p></div></div>
      {feedback ? <FeedbackReviewQueue initialItems={feedback.items} user={user} /> : <div className="service-warning">无法读取反馈队列。</div>}
    </section>
  </main></AdminShell>;
}
