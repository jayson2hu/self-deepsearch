"use client";

import type { OperationsEntity } from "@self-deepsearch/api-contracts";
import { Save } from "lucide-react";
import { FormEvent, useState } from "react";
import { SourceEvidenceFields, sourceEvidenceFromForm } from "./source-evidence-fields";

export function NewWorkForm() {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [created, setCreated] = useState<OperationsEntity | null>(null);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    setCreated(null);
    const form = new FormData(event.currentTarget);
    const optional = (key: string) => String(form.get(key) ?? "").trim() || null;
		const performerIDs = String(form.get("performer_ids") ?? "").split(/[\s,]+/).map((value) => value.trim()).filter(Boolean);
    try {
      const base = "";
      const response = await fetch(`${base}/admin/v1/works`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          code: optional("code"), title: optional("title"), title_original: optional("title_original"),
          release_date: optional("release_date"), studio_id: optional("studio_id"), summary: optional("summary"),
					performer_ids: performerIDs,
          sources: sourceEvidenceFromForm(form),
          reason: optional("reason"),
        }),
      });
      const body = await response.json().catch(() => null) as OperationsEntity | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败");
        return;
      }
      setCreated(body as OperationsEntity);
      event.currentTarget.reset();
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="entity-form" onSubmit={submit}>
      <div className="form-grid">
        <label><span>作品番号</span><input autoComplete="off" maxLength={100} name="code" required /></label>
        <label><span>实际发行日期</span><input name="release_date" type="date" /></label>
        <label className="wide"><span>作品标题</span><input maxLength={500} name="title" required /></label>
        <label className="wide"><span>原文标题（可选）</span><input maxLength={500} name="title_original" /></label>
        <label className="wide"><span>厂牌 ID（可选）</span><input name="studio_id" pattern="[0-9a-fA-F-]{36}" /></label>
				<label className="wide"><span>人物 ID（可选，最多 20 个，以逗号或换行分隔）</span><textarea name="performer_ids" rows={3} /></label>
        <label className="wide"><span>摘要（可选）</span><textarea maxLength={5000} name="summary" rows={5} /></label>
        <SourceEvidenceFields />
        <label className="wide"><span>录入理由</span><textarea maxLength={1000} minLength={2} name="reason" required rows={3} /></label>
      </div>
      {message ? <p aria-live="polite" className="form-error">{message}</p> : null}
      {created ? <p aria-live="polite" className="form-success">作品已进入审核队列，资料 ID：<code>{created.id}</code></p> : null}
      <div className="form-actions"><button className="primary" disabled={busy} type="submit"><Save size={17} />{busy ? "保存中" : "保存并提交审核"}</button></div>
    </form>
  );
}
