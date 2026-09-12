import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { EditorialRecommendationManager } from "../../components/editorial-recommendation-manager";
import { getCatalogEntities, getEditorialRecommendations, getOperator } from "../../lib/api";

export default async function EditorialPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const [recommendations, catalog] = await Promise.all([getEditorialRecommendations(), getCatalogEntities()]);
  const works = catalog?.items.filter((item) => item.entity_type === "work" && item.status === "published") ?? [];
  return <AdminShell active="编辑推荐" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>只选择已审核发布的作品，隐藏或下架后会自动退出公开推荐</p><h1>编辑推荐</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>推荐排期</h2><p>顺序、有效期和每次状态变更均写入审计记录。</p></div></div>
      {recommendations && catalog ? <EditorialRecommendationManager initialItems={recommendations.items} works={works} /> : <div className="service-warning">无法读取编辑推荐或资料目录。</div>}
    </section>
  </main></AdminShell>;
}
