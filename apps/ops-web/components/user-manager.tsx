"use client";

import type { OperationsUser, OperationsUserListResponse, OperationsUserSummary, UserSummary } from "@self-deepsearch/api-contracts";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

type Filters = { q: string; role: string; status: string };
const emptyFilters: Filters = { q: "", role: "", status: "" };

export function UserManager({ initialPage, initialSummary, operator }: {
  initialPage: OperationsUserListResponse;
  initialSummary: OperationsUserSummary;
  operator: UserSummary;
}) {
  const [page, setPage] = useState(initialPage);
  const [summary] = useState(initialSummary);
  const [filters, setFilters] = useState<Filters>(emptyFilters);
  const [applied, setApplied] = useState<Filters>(emptyFilters);
  const [busy, setBusy] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [notice, setNotice] = useState("");

  async function load(nextFilters: Filters, cursor = "") {
    setLoading(true);
    setMessage("");
    const parameters = new URLSearchParams({ limit: "25" });
    if (nextFilters.q.trim()) parameters.set("q", nextFilters.q.trim());
    if (nextFilters.role) parameters.set("role", nextFilters.role);
    if (nextFilters.status) parameters.set("status", nextFilters.status);
    if (cursor) parameters.set("cursor", cursor);
    try {
      const response = await adminFetch(`/admin/v1/users?${parameters.toString()}`, { credentials: "include" });
      const body = await response.json().catch(() => null) as OperationsUserListResponse | { error?: { message?: string } } | null;
      if (!response.ok || !body || !("items" in body)) {
        setMessage(body && "error" in body ? body.error?.message ?? "用户列表加载失败" : "用户列表加载失败");
        return;
      }
      setPage(body);
      setApplied(nextFilters);
    } catch {
      setMessage("无法连接运营服务；当前数字未改为 0");
    } finally {
      setLoading(false);
    }
  }

  function submit(event: FormEvent) {
    event.preventDefault();
    void load(filters);
  }

  async function changeRole(user: OperationsUser, role: "admin" | "editor" | "user") {
    if (role === user.role) return;
    setBusy(user.id); setMessage(""); setNotice("");
    try {
      const response = await adminFetch(`/admin/v1/users/${user.id}/role`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ role }),
      });
      const body = await response.json().catch(() => null) as OperationsUser | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "角色修改失败" : "角色修改失败"); return; }
      const updated = body as OperationsUser;
      setPage((current) => ({ ...current, items: current.items.map((item) => item.id === updated.id ? updated : item) }));
      setNotice("角色已更新，该账号的旧登录会话已失效，需要重新登录。");
    } catch { setMessage("无法连接运营服务"); } finally { setBusy(null); }
  }

  async function changeStatus(user: OperationsUser, status: "active" | "locked" | "suspended") {
    const reason = window.prompt(status === "active" ? "填写恢复账号理由" : status === "locked" ? "填写锁定账号理由" : "填写暂停账号理由");
    if (!reason || reason.trim().length < 2) return;
    setBusy(user.id); setMessage(""); setNotice("");
    try {
      const response = await adminFetch(`/admin/v1/users/${encodeURIComponent(user.id)}/status`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ status, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as OperationsUser | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "账号状态修改失败" : "账号状态修改失败"); return; }
      const updated = body as OperationsUser;
      setPage((current) => ({ ...current, items: current.items.map((item) => item.id === updated.id ? updated : item) }));
      setNotice("账号状态已更新；统计卡片保持初次读取快照，刷新页面后更新。");
    } catch { setMessage("无法连接运营服务"); } finally { setBusy(null); }
  }

  return <>
    <section aria-label="用户统计" className="metric-grid user-metrics">
      <Metric label="账号记录" value={summary.account_total} hint={`环境：${summary.environment}`} />
      <Metric label="普通账号" value={summary.ordinary_total} hint={`启用 ${summary.active_ordinary}`} />
      <Metric label="已验证普通账号" value={summary.verified_ordinary} hint="包含已关闭账号；不是活跃用户" />
      <Metric label="已记录公开注册" value={summary.public_registrations} hint={summary.coverage_started_at ? `覆盖始于 ${formatDateTime(summary.coverage_started_at)}` : "尚无可确认公开注册"} />
      <Metric label="今日公开注册" value={summary.public_registrations_today} hint="Asia/Shanghai 自然日" />
      <Metric label="近 7 日公开注册" value={summary.public_registrations_seven_days} hint="含今日的 7 个自然日" />
      <Metric label="管理账号" value={summary.management_total} hint="与关闭账号可能交叉" />
      <Metric label="历史来源未知" value={summary.unknown_origin} hint={`统计口径 ${summary.definition_version}`} />
    </section>
    <p className="muted">统计时间：<time dateTime={summary.observed_at}>{formatDateTime(summary.observed_at)}</time>。账号状态不等于 DAU。</p>
    <form className="user-filters" onSubmit={submit}>
      <label><span>邮箱或用户 ID</span><input maxLength={320} onChange={(event) => setFilters({ ...filters, q: event.target.value })} value={filters.q} /></label>
      <label><span>角色</span><select onChange={(event) => setFilters({ ...filters, role: event.target.value })} value={filters.role}><option value="">全部角色</option><option value="user">普通用户</option><option value="editor">编辑</option><option value="admin">管理员</option><option value="owner">所有者</option></select></label>
      <label><span>状态</span><select onChange={(event) => setFilters({ ...filters, status: event.target.value })} value={filters.status}><option value="">全部状态</option><option value="pending">待验证</option><option value="active">正常</option><option value="locked">锁定</option><option value="suspended">暂停</option><option value="closed">已关闭</option></select></label>
      <button disabled={loading} type="submit">{loading ? "加载中…" : "筛选"}</button>
      <button disabled={loading} onClick={() => { setFilters(emptyFilters); void load(emptyFilters); }} type="button">清空</button>
    </form>
    {operator.role === "owner" ? <p className="muted">修改角色会使该账号在所有设备上的旧登录会话失效。</p> : null}
    {notice ? <p role="status" aria-live="polite">{notice}</p> : null}
    {message ? <p aria-live="polite" className="form-error queue-error">{message}</p> : null}
    <p className="user-result-count">筛选结果共 {page.total} 条；当前页 {page.items.length} 条</p>
    <div className="table-wrap user-table"><table><thead><tr><th>邮箱 / ID</th><th>状态</th><th>角色</th><th>验证</th><th>来源</th><th>最近登录</th><th>创建时间</th></tr></thead><tbody>
      {page.items.map((user) => <tr key={user.id}><td data-label="邮箱 / ID"><strong>{user.email}</strong><small>{user.id}</small></td><td data-label="状态">
        {canManageStatus(operator, user) && (user.account_status === "active" || user.account_status === "locked" || user.account_status === "suspended") ? <select aria-label={`修改 ${user.email} 的账号状态`} disabled={busy !== null} onChange={(event) => void changeStatus(user, event.target.value as "active" | "locked" | "suspended")} value={user.account_status}><option value="active">正常</option><option value="locked">锁定</option><option value="suspended">暂停</option></select> : <span>{statusLabel(user.account_status)}</span>}
      </td><td data-label="角色">{operator.role === "owner" && user.role !== "owner" && user.id !== operator.id && user.account_status !== "closed" ? <select aria-label={`修改 ${user.email} 的角色`} disabled={busy !== null} onChange={(event) => void changeRole(user, event.target.value as "admin" | "editor" | "user")} value={user.role}><option value="user">普通用户</option><option value="editor">编辑</option><option value="admin">管理员</option></select> : <span>{roleLabel(user.role)}</span>}</td>
      <td data-label="验证">{user.email_verified_at ? "已验证" : "未验证"}</td><td data-label="来源">{originLabel(user.registration_origin)}{user.is_test ? " · 测试" : ""}</td><td data-label="最近登录">{user.last_login_at ? <time dateTime={user.last_login_at}>{formatDateTime(user.last_login_at)}</time> : "未记录"}</td><td data-label="创建时间"><time dateTime={user.created_at}>{formatDateTime(user.created_at)}</time></td></tr>)}
    </tbody></table></div>
    {page.items.length === 0 ? <div className="empty-state">当前筛选没有账号。</div> : null}
    <div className="user-pagination"><button disabled={loading || !page.next_cursor} onClick={() => page.next_cursor && void load(applied, page.next_cursor)} type="button">下一页</button><span>分页基于统计时间快照；改变筛选会重新开始。</span></div>
  </>;
}

function Metric({ label, value, hint }: { label: string; value: number; hint: string }) { return <article><div><small>{label}</small><strong>{value}</strong><p>{hint}</p></div></article>; }
function canManageStatus(operator: UserSummary, user: OperationsUser) { return user.id !== operator.id && user.role !== "owner" && user.account_status !== "closed" && (operator.role === "owner" || (operator.role === "admin" && user.role === "user")); }
function statusLabel(status: OperationsUser["account_status"]) { return ({ pending: "待验证", active: "正常", locked: "锁定", suspended: "暂停", closed: "已关闭" } as const)[status]; }
function roleLabel(role: OperationsUser["role"]) { return ({ owner: "所有者", admin: "管理员", editor: "编辑", user: "普通用户" } as const)[role]; }
function originLabel(origin: OperationsUser["registration_origin"]) { return ({ public_signup: "公开注册", invitation: "邀请", bootstrap: "系统初始化", fixture: "测试夹具", unknown: "未知" } as const)[origin]; }
