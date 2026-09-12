import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { ConflictManager } from "../../components/conflict-manager";
import { getConflictReviews, getOperator } from "../../lib/api";

export default async function ConflictsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const conflicts = await getConflictReviews();
  return <AdminShell active="字段冲突" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>人工裁决不会直接覆盖正式资料</p><h1>字段冲突</h1></div></div>
    {conflicts ? <ConflictManager initialItems={conflicts.items} operator={operator} /> : <div className="service-warning dashboard-section">无法读取字段冲突记录。</div>}
  </main></AdminShell>;
}
