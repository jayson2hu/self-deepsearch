import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { TakedownManager } from "../../components/takedown-manager";
import { getOperator, getTakedownRequests } from "../../lib/api";

export default async function RightsPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const requests = await getTakedownRequests();
  return <AdminShell active="权利下架" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>登记、核验、执行和审计必须使用同一个请求编号</p><h1>权利下架</h1></div></div>
    {requests ? <TakedownManager initialItems={requests.items} operator={operator} /> : <div className="service-warning dashboard-section">无法读取权利下架记录。</div>}
  </main></AdminShell>;
}
