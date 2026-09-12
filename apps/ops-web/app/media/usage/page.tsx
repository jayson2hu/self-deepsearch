import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { MediaUsageManager } from "../../../components/media-usage-manager";
import { getOperator } from "../../../lib/api";

export default async function UsagePage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  if (user.role !== "admin" && user.role !== "owner") redirect("/");
  return <AdminShell active="用量复核" user={user}><main className="main-content narrow-content">
    <div className="page-heading form-heading"><div><p>只处理观察建议，不代表实际停图或恢复</p><h1>图片用量复核</h1></div></div>
    <section className="form-surface"><MediaUsageManager /></section>
  </main></AdminShell>;
}
