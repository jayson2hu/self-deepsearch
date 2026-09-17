import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { AdManager } from "../../components/ad-manager";
import { getAdCampaigns, getOperator } from "../../lib/api";

export default async function AdsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const campaigns = await getAdCampaigns();
  return <AdminShell active="广告管理" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>后台审核素材、控制排期；无有效广告时前台自动隐藏</p><h1>广告管理</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>广告活动与版本</h2><p>管理员维护草稿；仅所有者可发布或紧急暂停，全部操作进入审计日志。</p></div></div>
      {campaigns ? <AdManager initialItems={campaigns.items} operatorRole={operator.role} /> : <div className="service-warning">无法读取广告数据；不会把读取失败显示为无广告。</div>}
    </section>
  </main></AdminShell>;
}
