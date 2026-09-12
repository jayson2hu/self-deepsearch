"use client";

import type { Invitation, UserSummary } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

export function InvitationManager({ initialInvitations, operator }: { initialInvitations: Invitation[]; operator: UserSummary }) {
  const [invitations, setInvitations] = useState(initialInvitations);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  async function createInvitation(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy("create");
    setMessage("");
    setError("");
    try {
      const response = await adminFetch("/admin/v1/invitations", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: data.get("email"), role: data.get("role"), reason: data.get("reason") }),
      });
      const body = await response.json().catch(() => null) as Invitation | { error?: { message?: string } } | null;
      if (!response.ok) {
        setError(body && "error" in body ? body.error?.message ?? "邀请创建失败" : "邀请创建失败");
        return;
      }
      setInvitations((current) => [body as Invitation, ...current]);
      form.reset();
      setMessage("邀请已创建，验证码已发送到对应邮箱。");
    } catch {
      setError("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  async function revokeInvitation(invitation: Invitation) {
    const reason = window.prompt("填写撤销邀请理由");
    if (!reason || reason.trim().length < 2) return;
    setBusy(invitation.id);
    setMessage("");
    setError("");
    try {
      const response = await adminFetch(`/admin/v1/invitations/${encodeURIComponent(invitation.id)}/revoke`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as Invitation | { error?: { message?: string } } | null;
      if (!response.ok) {
        setError(body && "error" in body ? body.error?.message ?? "邀请撤销失败" : "邀请撤销失败");
        return;
      }
      const updated = body as Invitation;
      setInvitations((current) => current.map((item) => item.id === updated.id ? updated : item));
      setMessage("邀请已撤销。");
    } catch {
      setError("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  return <div className="invitation-layout">
    <form className="form-surface entity-form invitation-form" onSubmit={createInvitation}>
      <div className="section-heading"><div><h2>创建邀请</h2><p>验证码仅通过邮件发送，不在后台或接口返回明文。</p></div></div>
      <div className="form-grid">
        <label><span>邮箱</span><input name="email" type="email" autoComplete="email" maxLength={320} required /></label>
        <label><span>角色</span><select name="role" defaultValue="user"><option value="user">普通用户</option>{operator.role === "owner" && <><option value="editor">编辑</option><option value="admin">管理员</option></>}</select></label>
        <label className="wide"><span>邀请理由</span><textarea name="reason" rows={3} minLength={2} maxLength={500} required /></label>
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {message && <p className="form-success" role="status">{message}</p>}
      <div className="form-actions"><button className="button primary" type="submit" disabled={busy !== null}>{busy === "create" ? "发送中…" : "发送邀请"}</button></div>
    </form>

    <section className="invitation-list">
      <div className="section-heading"><div><h2>邀请记录</h2><p>邀请默认 48 小时有效；已接受、撤销或过期记录保留审计痕迹。</p></div></div>
      {invitations.length === 0 ? <div className="empty-state">暂无邀请记录。</div> : <div className="table-wrap"><table><thead><tr><th>邮箱</th><th>角色</th><th>状态</th><th>发送时间</th><th>有效期</th><th>操作</th></tr></thead><tbody>
        {invitations.map((invitation) => <tr key={invitation.id}><td><strong>{invitation.email}</strong></td><td>{roleLabel(invitation.role)}</td><td><span className={`status invitation-status ${invitation.status}`}>{statusLabel(invitation.status)}</span></td><td><time dateTime={invitation.sent_at}>{formatDateTime(invitation.sent_at)}</time></td><td><time dateTime={invitation.expires_at}>{formatDateTime(invitation.expires_at)}</time></td><td>{invitation.status === "pending" ? <button className="button compact-button danger-outline" type="button" disabled={busy !== null} onClick={() => revokeInvitation(invitation)}>撤销</button> : <span className="muted-text">无</span>}</td></tr>)}
      </tbody></table></div>}
    </section>
  </div>;
}

function roleLabel(role: Invitation["role"]) {
  return ({ admin: "管理员", editor: "编辑", user: "普通用户" } as const)[role];
}

function statusLabel(status: Invitation["status"]) {
  return ({ pending: "待接受", accepted: "已接受", revoked: "已撤销", expired: "已过期" } as const)[status];
}
