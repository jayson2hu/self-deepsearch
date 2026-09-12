import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { NewWorkForm } from "../../../components/new-work-form";
import { getOperator } from "../../../lib/api";

export default async function NewWorkPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  return (
    <AdminShell active="新建作品" user={user}>
      <main className="main-content narrow-content">
        <Link className="back-link" href="/"><ArrowLeft size={16} />返回运营概览</Link>
        <div className="page-heading form-heading"><div><p>人工录入 · 提交后需要其他管理员审核</p><h1>新建作品资料</h1></div></div>
        <section className="form-surface"><NewWorkForm /></section>
      </main>
    </AdminShell>
  );
}
