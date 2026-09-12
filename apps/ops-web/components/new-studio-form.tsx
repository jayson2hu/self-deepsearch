"use client";

import type { OperationsEntity } from "@self-deepsearch/api-contracts";
import { Save } from "lucide-react";
import { FormEvent, useState } from "react";
import { SourceEvidenceFields, sourceEvidenceFromForm } from "./source-evidence-fields";

export function NewStudioForm() {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [created, setCreated] = useState<OperationsEntity | null>(null);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage(""); setCreated(null);
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const base = "";
      const response = await fetch(`${base}/admin/v1/studios`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: String(data.get("name") ?? "").trim(), canonical_slug: String(data.get("canonical_slug") ?? "").trim(), sources: sourceEvidenceFromForm(data), reason: String(data.get("reason") ?? "").trim() }),
      });
      const body = await response.json().catch(() => null) as OperationsEntity | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败"); return; }
      setCreated(body as OperationsEntity); form.reset();
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }
  return <form className="entity-form" onSubmit={submit}><div className="form-grid">
    <label className="wide"><span>厂牌 / 制作方名称</span><input maxLength={200} name="name" required /></label>
    <label className="wide"><span>公开 slug</span><input maxLength={200} name="canonical_slug" pattern="[a-z0-9]+(?:-[a-z0-9]+)*" required /></label>
    <SourceEvidenceFields />
    <label className="wide"><span>录入理由</span><textarea maxLength={1000} minLength={2} name="reason" required rows={3} /></label>
  </div>{message ? <p className="form-error">{message}</p> : null}{created ? <p className="form-success">厂牌已进入审核队列，资料 ID：<code>{created.id}</code></p> : null}
    <div className="form-actions"><button className="primary" disabled={busy} type="submit"><Save size={17} />{busy ? "保存中" : "保存并提交审核"}</button></div>
  </form>;
}
