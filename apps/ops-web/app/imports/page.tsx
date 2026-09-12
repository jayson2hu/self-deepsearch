import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { WorkCSVPreflight } from "../../components/work-csv-preflight";
import { getOperator, getWorkImportBatches } from "../../lib/api";

export default async function ImportsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const batches = await getWorkImportBatches();
  return <AdminShell active="CSV 导入" user={operator}><main className="main-content narrow-content">
    <div className="page-heading"><div><p>最多 500 行、1 MiB，提交前必须通过服务端预检</p><h1>作品 CSV 导入</h1></div></div>
    <WorkCSVPreflight initialBatches={batches?.items ?? []} />
  </main></AdminShell>;
}
