import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { DiscoveryMixForm } from "../../components/discovery-mix-form";
import { getDiscoveryMixRule, getOperator } from "../../lib/api";

export default async function SettingsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const rule = await getDiscoveryMixRule();
  return <AdminShell active="首页配置" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>控制公开首页“发现”区域的作品与人物排列比例</p><h1>首页配置</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>发现流混排</h2><p>默认每 4 部作品插入 1 个人物资料，首屏展示 10 条。</p></div></div>
      {rule ? <DiscoveryMixForm initialRule={rule} /> : <div className="service-warning">无法读取首页混排规则。</div>}
    </section>
  </main></AdminShell>;
}
