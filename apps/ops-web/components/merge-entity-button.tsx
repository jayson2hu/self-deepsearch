"use client";

import type { CatalogEntity, EntityMerge } from "@self-deepsearch/api-contracts";
import { Merge, X } from "lucide-react";
import { FormEvent, useState } from "react";
import { adminFetch } from "../lib/reauth-client";

export function MergeEntityButton({ source, targets }: { source: CatalogEntity; targets: CatalogEntity[] }) {
  const [open, setOpen] = useState(false);
  const [targetID, setTargetID] = useState("");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!window.confirm("合并后旧地址将永久跳转，源资料不能重新发布。确认继续？")) return;
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/entities/${encodeURIComponent(source.id)}/merge`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entity_type: source.entity_type, target_entity_id: targetID, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as EntityMerge | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "合并失败" : "合并失败");
        return;
      }
      const merge = body as EntityMerge;
      setMessage(`合并完成：${merge.source_path} 已永久跳转至 ${merge.target_path}`);
      setTimeout(() => window.location.reload(), 900);
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  if (!targets.length) return null;
  return <>
    <button className="button compact-button" type="button" onClick={() => setOpen(true)}><Merge size={14} />合并</button>
    {open ? <div className="modal-backdrop" role="presentation"><section className="merge-dialog" role="dialog" aria-modal="true" aria-labelledby={`merge-${source.id}`}>
      <header><div><p>不可逆操作</p><h2 id={`merge-${source.id}`}>合并 {source.label}</h2></div><button aria-label="关闭" type="button" onClick={() => setOpen(false)}><X size={18} /></button></header>
      <form className="entity-form" onSubmit={submit}><label><span>合并到</span><select required value={targetID} onChange={(event) => setTargetID(event.target.value)}><option value="">选择同类型已发布资料</option>{targets.map((target) => <option value={target.id} key={target.id}>{target.label}</option>)}</select></label>
        <label><span>合并理由</span><textarea required minLength={2} maxLength={1000} rows={3} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
        {message ? <p className={message.includes("失败") || message.includes("无法") ? "form-error" : "form-success"} role="status">{message}</p> : null}
        <div className="form-actions"><button type="button" onClick={() => setOpen(false)}>取消</button><button className="primary" disabled={busy} type="submit"><Merge size={16} />{busy ? "合并中" : "确认合并"}</button></div>
      </form>
    </section></div> : null}
  </>;
}
