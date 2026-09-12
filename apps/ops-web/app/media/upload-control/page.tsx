import { redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { MediaUploadManager } from "../../../components/media-upload-manager";
import { getOperator } from "../../../lib/api";

export default async function UploadControlPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  if (user.role !== "admin" && user.role !== "owner") redirect("/");
  return <AdminShell active="上传控制" user={user}><main className="main-content narrow-content">
    <div className="page-heading form-heading"><div><p>授权操作与执行回执分开确认</p><h1>图片上传控制</h1></div></div>
    <section className="form-surface"><MediaUploadManager operatorID={user.id} /></section>
  </main></AdminShell>;
}
