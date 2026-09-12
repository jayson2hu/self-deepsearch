import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { NewPerformerForm } from "../../../components/new-performer-form";
import { getOperator } from "../../../lib/api";

export default async function NewPerformerPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  return <AdminShell active="资料目录" user={user}><main className="main-content narrow-content">
    <Link className="back-link" href="/catalog"><ArrowLeft size={16} />返回资料目录</Link>
    <div className="page-heading form-heading"><div><p>公开参数可选 · 发布前必须核验成年人状态</p><h1>新建人物资料</h1></div></div>
    <section className="form-surface"><NewPerformerForm /></section>
  </main></AdminShell>;
}
