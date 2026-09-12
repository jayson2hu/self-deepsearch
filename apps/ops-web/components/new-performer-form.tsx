"use client";

import type { OperationsEntity } from "@self-deepsearch/api-contracts";
import { Save } from "lucide-react";
import { FormEvent, useState } from "react";
import { SourceEvidenceFields, sourceEvidenceFromForm } from "./source-evidence-fields";

export function NewPerformerForm() {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [created, setCreated] = useState<OperationsEntity | null>(null);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage(""); setCreated(null);
    const form = event.currentTarget;
    const data = new FormData(form);
    const optional = (key: string) => String(data.get(key) ?? "").trim() || null;
    const optionalNumber = (key: string) => optional(key) ? Number(optional(key)) : null;
    const aliases = String(data.get("aliases") ?? "").split(/[\n,]+/).map((value) => value.trim()).filter(Boolean);
    try {
      const base = "";
      const response = await fetch(`${base}/admin/v1/performers`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          display_name: optional("display_name"), name_original: optional("name_original"), romanized_name: optional("romanized_name"),
          aliases,
          adult_status: String(data.get("adult_status") ?? "unverified"), activity_status: String(data.get("activity_status") ?? "unknown"),
          agency: optional("agency"), birth_year: optionalNumber("birth_year"), height_cm: optionalNumber("height_cm"),
          measurements: optional("measurements"), debut_year: optionalNumber("debut_year"), sources: sourceEvidenceFromForm(data), reason: optional("reason"),
        }),
      });
      const body = await response.json().catch(() => null) as OperationsEntity | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败"); return; }
      setCreated(body as OperationsEntity); form.reset();
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }
  return <form className="entity-form" onSubmit={submit}><div className="form-grid">
    <label><span>公开艺名</span><input maxLength={200} name="display_name" required /></label>
    <label><span>活动状态</span><select name="activity_status"><option value="unknown">待核实</option><option value="active">活动中</option><option value="retired">已引退</option></select></label>
    <label><span>原文名（可选）</span><input maxLength={200} name="name_original" /></label>
    <label><span>罗马字（可选）</span><input maxLength={200} name="romanized_name" /></label>
    <label className="wide"><span>人物别名（可选，最多 30 个，以逗号或换行分隔）</span><textarea name="aliases" rows={3} /></label>
    <label><span>成人状态核验</span><select name="adult_status"><option value="unverified">未核验</option><option value="verified">已核验为成年人</option><option value="restricted">限制展示</option></select></label>
    <label><span>所属（可选）</span><input maxLength={200} name="agency" /></label>
    <label><span>出生年份（可选）</span><input max="2200" min="1900" name="birth_year" type="number" /></label>
    <label><span>出道年份（可选）</span><input max="2200" min="1900" name="debut_year" type="number" /></label>
    <label><span>身高 cm（可选）</span><input max="250" min="100" name="height_cm" type="number" /></label>
    <label><span>身体参数（可选）</span><input maxLength={200} name="measurements" /></label>
    <SourceEvidenceFields />
    <label className="wide"><span>录入理由</span><textarea maxLength={1000} minLength={2} name="reason" required rows={3} /></label>
  </div>{message ? <p className="form-error">{message}</p> : null}{created ? <p className="form-success">人物已进入审核队列，资料 ID：<code>{created.id}</code></p> : null}
    <div className="form-actions"><button className="primary" disabled={busy} type="submit"><Save size={17} />{busy ? "保存中" : "保存并提交审核"}</button></div>
  </form>;
}
