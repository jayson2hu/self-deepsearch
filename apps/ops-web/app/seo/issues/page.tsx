import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { SEOIssueManager } from "../../../components/seo-issue-manager";
import { getOperator, getSEOIssues, getSEOScans } from "../../../lib/api";

export default async function SEOIssuesPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const [issues, scans] = await Promise.all([getSEOIssues(), getSEOScans()]);
  return <AdminShell active="SEO 中心" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>问题去重、分派、修复、复测与关闭均保留历史</p><h1>SEO 优化问题</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>扫描与问题闭环</h2><p>扫描目标仅来自内部正式清单，单批最多 1,000 条；关闭前重新核验当前资格。</p></div></div>
      {issues && scans ? <SEOIssueManager initialIssues={issues.items} initialScans={scans.items} operator={operator} /> : <div className="service-warning">无法读取扫描或问题数据；不会将失败显示为无问题。</div>}
    </section>
  </main></AdminShell>;
}
