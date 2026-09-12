import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { MediaManifestForm } from "../../components/media-manifest-form";
import { getOperator } from "../../lib/api";

export default async function MediaPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  if (user.role === "editor") redirect("/");
  return <AdminShell active="图片资产" user={user}><main className="main-content narrow-content">
    <div className="page-heading form-heading"><div><p>仅导入媒体处理器生成并验证的清单</p><h1>图片资产发布</h1></div></div>
    <section className="form-surface"><MediaManifestForm /></section>
  </main></AdminShell>;
}
