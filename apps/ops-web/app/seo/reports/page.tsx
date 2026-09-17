import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { SEOReportManager } from "../../../components/seo-report-manager";
import { getOperator, getSEOReports } from "../../../lib/api";

export default async function SEOReportsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const reports = await getSEOReports();
  return <AdminShell active="SEO 中心" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>导入负责人授权提供的官方导出；不默认连接 OAuth 或 Search Console API</p><h1>搜索报表</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>Search Console 导出</h2><p>真实数据与演示数据始终分列，技术可索引数量不等同搜索引擎已收录。</p></div></div>
      {reports ? <SEOReportManager initialItems={reports.items} operatorRole={operator.role} /> : <div className="service-warning">无法读取导入记录；不会把服务失败显示为无数据。</div>}
    </section>
  </main></AdminShell>;
}
