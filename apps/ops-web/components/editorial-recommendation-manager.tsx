"use client";

import type { CatalogEntity, EditorialRecommendation } from "@self-deepsearch/api-contracts";
import { Pause, Pencil, Play, Plus, Save, Trash2, X } from "lucide-react";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

type Draft = {
  id: string | null;
  work_id: string;
  position: string;
  starts_at: string;
  ends_at: string;
  reason: string;
  change_reason: string;
  status: "active" | "paused";
};

const emptyDraft = (): Draft => ({
  id: null,
  work_id: "",
  position: "1",
  starts_at: toLocalInput(new Date().toISOString()),
  ends_at: "",
  reason: "",
  change_reason: "",
  status: "active",
});

export function EditorialRecommendationManager({ initialItems, works }: {
  initialItems: EditorialRecommendation[];
  works: CatalogEntity[];
}) {
  const [items, setItems] = useState(initialItems);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  function edit(item: EditorialRecommendation) {
    if (item.status === "removed") return;
    setDraft({ id: item.id, work_id: item.work_id, position: String(item.position),
      starts_at: toLocalInput(item.starts_at), ends_at: item.ends_at ? toLocalInput(item.ends_at) : "",
      reason: item.reason, change_reason: "", status: item.status });
    setMessage("");
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    await saveDraft(draft);
  }

  async function saveDraft(value: Draft) {
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(value.id
        ? `/admin/v1/editorial-recommendations/${encodeURIComponent(value.id)}`
        : "/admin/v1/editorial-recommendations", {
        method: value.id ? "PUT" : "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ work_id: value.work_id, position: Number(value.position),
          starts_at: new Date(value.starts_at).toISOString(),
          ends_at: value.ends_at ? new Date(value.ends_at).toISOString() : null,
          reason: value.reason.trim(), change_reason: value.change_reason.trim(), status: value.status }),
      });
      const body = await response.json().catch(() => null) as EditorialRecommendation | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败");
        return;
      }
      const saved = body as EditorialRecommendation;
      setItems((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
      setDraft(emptyDraft());
      setMessage("编辑推荐已保存，公开首页缓存会在五分钟内更新");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  async function changeStatus(item: EditorialRecommendation, status: "active" | "paused") {
    await saveDraft({ id: item.id, work_id: item.work_id, position: String(item.position),
      starts_at: toLocalInput(item.starts_at), ends_at: item.ends_at ? toLocalInput(item.ends_at) : "",
      reason: item.reason, change_reason: status === "active" ? "恢复编辑推荐" : "暂停编辑推荐", status });
  }

  async function remove(item: EditorialRecommendation) {
    if (!window.confirm(`确认移除 ${item.work_code} 的编辑推荐？`)) return;
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/editorial-recommendations/${encodeURIComponent(item.id)}`, {
        method: "DELETE", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ reason: "运营移除编辑推荐" }),
      });
      const body = await response.json().catch(() => null) as EditorialRecommendation | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "移除失败" : "移除失败");
        return;
      }
      const removed = body as EditorialRecommendation;
      setItems((current) => current.map((value) => value.id === removed.id ? removed : value));
      if (draft.id === removed.id) setDraft(emptyDraft());
      setMessage("推荐已移除，审计记录已保留");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  return <div className="editorial-manager">
    <form className="entity-form" onSubmit={submit}>
      <div className="form-grid">
        <label className="wide"><span>已发布作品</span><select required value={draft.work_id} onChange={(event) => setDraft({ ...draft, work_id: event.target.value })}>
          <option value="">选择作品</option>
          {works.map((work) => <option key={work.id} value={work.id}>{work.label}</option>)}
        </select></label>
        <label><span>展示顺序</span><input min={1} max={100} required type="number" value={draft.position} onChange={(event) => setDraft({ ...draft, position: event.target.value })} /></label>
        <label><span>状态</span><select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Draft["status"] })}><option value="active">启用</option><option value="paused">暂停</option></select></label>
        <label><span>开始时间</span><input required type="datetime-local" value={draft.starts_at} onChange={(event) => setDraft({ ...draft, starts_at: event.target.value })} /></label>
        <label><span>结束时间（可选）</span><input type="datetime-local" value={draft.ends_at} onChange={(event) => setDraft({ ...draft, ends_at: event.target.value })} /></label>
        <label className="wide"><span>推荐理由</span><textarea minLength={2} maxLength={1000} required rows={3} value={draft.reason} onChange={(event) => setDraft({ ...draft, reason: event.target.value })} /></label>
        <label className="wide"><span>本次操作理由</span><textarea minLength={2} maxLength={1000} required rows={2} value={draft.change_reason} onChange={(event) => setDraft({ ...draft, change_reason: event.target.value })} /></label>
      </div>
      {message ? <p aria-live="polite" className={message.includes("失败") || message.includes("无法") ? "form-error" : "form-success"}>{message}</p> : null}
      <div className="form-actions">
        {draft.id ? <button disabled={busy} type="button" onClick={() => setDraft(emptyDraft())}><X size={17} />取消编辑</button> : null}
        <button className="primary" disabled={busy || !works.length} type="submit">{draft.id ? <Save size={17} /> : <Plus size={17} />}{busy ? "处理中" : draft.id ? "保存修改" : "新增推荐"}</button>
      </div>
    </form>

    <div className="table-wrap"><table><thead><tr><th>顺序</th><th>作品</th><th>有效期</th><th>状态</th><th>维护信息</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => <tr key={item.id}><td>{item.position}</td><td><strong>{item.work_code}</strong><br />{item.work_title}</td><td><time dateTime={item.starts_at}>{formatDateTime(item.starts_at)}</time><br />至 {item.ends_at ? <time dateTime={item.ends_at}>{formatDateTime(item.ends_at)}</time> : "长期"}</td><td><span className={`status ${item.status === "active" ? "ok" : "neutral"}`}>{statusLabel(item.status)}</span></td><td>{item.created_by}<br /><small>{item.reason}</small></td><td><div className="row-actions">
        {item.status !== "removed" ? <button aria-label="编辑推荐" title="编辑" type="button" disabled={busy} onClick={() => edit(item)}><Pencil size={15} /></button> : null}
        {item.status === "active" ? <button aria-label="暂停推荐" title="暂停" type="button" disabled={busy} onClick={() => changeStatus(item, "paused")}><Pause size={15} /></button> : null}
        {item.status === "paused" ? <button aria-label="启用推荐" className="approve" title="启用" type="button" disabled={busy} onClick={() => changeStatus(item, "active")}><Play size={15} /></button> : null}
        {item.status !== "removed" ? <button aria-label="移除推荐" className="reject" title="移除" type="button" disabled={busy} onClick={() => remove(item)}><Trash2 size={15} /></button> : null}
      </div></td></tr>)}
      {!items.length ? <tr><td className="empty-state" colSpan={6}>暂无编辑推荐</td></tr> : null}
    </tbody></table></div>
  </div>;
}

function toLocalInput(value: string) {
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function statusLabel(status: EditorialRecommendation["status"]) {
  return ({ active: "启用", paused: "暂停", removed: "已移除" } as const)[status];
}
