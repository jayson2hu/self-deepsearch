"use client";

import type { OperationsUser, UserSummary } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

export function UserManager({ initialUsers, operator }: { initialUsers: OperationsUser[]; operator: UserSummary }) {
  const [users, setUsers] = useState(initialUsers);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [notice, setNotice] = useState("");

  async function changeRole(user: OperationsUser, role: "admin" | "editor" | "user") {
    if (role === user.role) return;
    setBusy(user.id);
    setMessage("");
    setNotice("");
    try {
      const base = "";
      const response = await adminFetch(`${base}/admin/v1/users/${user.id}/role`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ role }),
      });
      const body = await response.json().catch(() => null) as OperationsUser | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "角色修改失败" : "角色修改失败");
        return;
      }
      const updated = body as OperationsUser;
      setUsers((current) => current.map((item) => item.id === updated.id ? updated : item));
      setNotice("角色已更新，该账号的旧登录会话已失效，需要重新登录。");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  async function changeStatus(user: OperationsUser, status: "active" | "locked" | "suspended") {
    const reason = window.prompt(status === "active" ? "填写恢复账号理由" : status === "locked" ? "填写锁定账号理由" : "填写暂停账号理由");
    if (!reason || reason.trim().length < 2) return;
    setBusy(user.id);
    setMessage("");
    setNotice("");
    try {
      const response = await adminFetch(`/admin/v1/users/${encodeURIComponent(user.id)}/status`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as OperationsUser | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "账号状态修改失败" : "账号状态修改失败");
        return;
      }
      const updated = body as OperationsUser;
      setUsers((current) => current.map((item) => item.id === updated.id ? updated : item));
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  return <>
    {operator.role === "owner" ? <p className="muted">修改角色会使该账号在所有设备上的旧登录会话失效，需重新登录后使用新权限。</p> : null}
    {notice ? <p role="status" aria-live="polite">{notice}</p> : null}
    {message ? <p aria-live="polite" className="form-error queue-error">{message}</p> : null}
    <div className="table-wrap user-table"><table><thead><tr><th>邮箱</th><th>状态</th><th>当前角色</th><th>创建时间</th></tr></thead><tbody>
      {users.map((user) => <tr key={user.id}><td data-label="邮箱"><strong>{user.email}</strong></td><td data-label="状态">
        {canManageStatus(operator, user) && (user.account_status === "active" || user.account_status === "locked" || user.account_status === "suspended") ? <select aria-label={`修改 ${user.email} 的账号状态`} disabled={busy !== null} onChange={(event) => changeStatus(user, event.target.value as "active" | "locked" | "suspended")} value={user.account_status}>
          <option value="active">正常</option><option value="locked">锁定</option><option value="suspended">暂停</option>
        </select> : <span>{statusLabel(user.account_status)}</span>}
      </td><td data-label="当前角色">
        {operator.role === "owner" && user.role !== "owner" && user.id !== operator.id && user.account_status !== "closed" ? <select aria-label={`修改 ${user.email} 的角色`} disabled={busy !== null} onChange={(event) => changeRole(user, event.target.value as "admin" | "editor" | "user")} value={user.role}>
          <option value="user">普通用户</option><option value="editor">编辑</option><option value="admin">管理员</option>
        </select> : <span>{user.role}</span>}
      </td><td data-label="创建时间"><time dateTime={user.created_at}>{formatDateTime(user.created_at)}</time></td></tr>)}
    </tbody></table></div>
  </>;
}

function canManageStatus(operator: UserSummary, user: OperationsUser) {
  if (user.id === operator.id || user.role === "owner" || user.account_status === "closed") return false;
  return operator.role === "owner" || (operator.role === "admin" && user.role === "user");
}

function statusLabel(status: OperationsUser["account_status"]) {
  return ({ pending: "待验证", active: "正常", locked: "锁定", suspended: "暂停", closed: "已关闭" } as const)[status];
}
