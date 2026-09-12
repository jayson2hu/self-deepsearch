import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { NewStudioForm } from "../../../components/new-studio-form";
import { getOperator } from "../../../lib/api";

export default async function NewStudioPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  return <AdminShell active="资料目录" user={user}><main className="main-content narrow-content">
    <Link className="back-link" href="/catalog"><ArrowLeft size={16} />返回资料目录</Link>
    <div className="page-heading form-heading"><div><p>人工录入 · 提交后需要其他管理员审核</p><h1>新建厂牌资料</h1></div></div>
    <section className="form-surface"><NewStudioForm /></section>
  </main></AdminShell>;
}
