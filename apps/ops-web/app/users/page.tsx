import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { InvitationManager } from "../../components/invitation-manager";
import { UserManager } from "../../components/user-manager";
import { getOperationsInvitations, getOperationsUsers, getOperator } from "../../lib/api";

export default async function UsersPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");
  const [users, invitations] = await Promise.all([getOperationsUsers(), getOperationsInvitations()]);
  return <AdminShell active="用户与权限" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>owner 可调整角色；admin 可管理普通用户账号状态</p><h1>用户与权限</h1></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>账号列表</h2><p>账号必须先完成公开站注册和邮箱验证；锁定或暂停会立即撤销现有登录</p></div></div>
      {users ? <UserManager initialUsers={users.items} operator={operator} /> : <div className="service-warning">无法读取用户列表。</div>}
    </section>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>邀请账号</h2><p>邀请仅用于创建已验证账号；管理员不能邀请其他管理员或编辑。</p></div></div>
      {invitations ? <InvitationManager initialInvitations={invitations.items} operator={operator} /> : <div className="service-warning">无法读取邀请列表。</div>}
    </section>
  </main></AdminShell>;
}
