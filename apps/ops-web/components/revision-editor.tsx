"use client";

import type { ContentRevision } from "@self-deepsearch/api-contracts";
import { Save } from "lucide-react";
import { FormEvent, useState } from "react";
import { SourceEvidenceFields, sourceEvidenceFromForm } from "./source-evidence-fields";

export function RevisionEditor({ latest }: { latest: ContentRevision }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const payload = latest.payload;
  const text = (key: string) => typeof payload[key] === "string" ? payload[key] as string : "";
  const number = (key: string) => typeof payload[key] === "number" ? String(payload[key]) : "";
  const performerIDs = Array.isArray(payload.performer_ids) ? payload.performer_ids.filter((value): value is string => typeof value === "string").join("\n") : "";
  const aliases = Array.isArray(payload.aliases) ? payload.aliases.filter((value): value is string => typeof value === "string").join("\n") : "";

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage("");
    const data = new FormData(event.currentTarget);
    const optional = (key: string) => String(data.get(key) ?? "").trim() || null;
    const optionalNumber = (key: string) => optional(key) ? Number(optional(key)) : null;
    let revisionPayload: Record<string, unknown>;
    if (latest.entity_type === "work") {
      revisionPayload = {
        canonical_code: optional("canonical_code"), title: optional("title"), title_original: optional("title_original"),
        release_date: optional("release_date"), studio_id: optional("studio_id"), summary: optional("summary"),
        performer_ids: String(data.get("performer_ids") ?? "").split(/[\s,]+/).map((value) => value.trim()).filter(Boolean),
      };
    } else if (latest.entity_type === "performer") {
      revisionPayload = {
        display_name: optional("display_name"), name_original: optional("name_original"), romanized_name: optional("romanized_name"),
        aliases: String(data.get("aliases") ?? "").split(/[\n,]+/).map((value) => value.trim()).filter(Boolean),
        adult_status: optional("adult_status"), activity_status: optional("activity_status"), agency: optional("agency"),
        birth_year: optionalNumber("birth_year"), height_cm: optionalNumber("height_cm"), measurements: optional("measurements"), debut_year: optionalNumber("debut_year"),
      };
    } else {
      revisionPayload = { name: optional("name") };
    }
    try {
      const base = "";
      const response = await fetch(`${base}/admin/v1/entities/${latest.entity_id}/revisions`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entity_type: latest.entity_type, payload: revisionPayload, sources: sourceEvidenceFromForm(data), reason: optional("reason") }),
      });
      const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body?.error?.message ?? "提交修订失败"); return; }
      window.location.reload();
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }

  return <section className="form-surface revision-editor"><div className="section-heading"><div><h2>提交新修订</h2><p>以当前最新快照为基线，提交后仍需其他管理员审核。</p></div></div>
    <form className="entity-form" onSubmit={submit}><div className="form-grid">
      {latest.entity_type === "work" ? <>
        <label><span>作品番号</span><input defaultValue={text("canonical_code")} maxLength={100} name="canonical_code" required /></label>
        <label><span>实际发行日期</span><input defaultValue={text("release_date")} name="release_date" type="date" /></label>
        <label className="wide"><span>作品标题</span><input defaultValue={text("title")} maxLength={500} name="title" required /></label>
        <label className="wide"><span>原文标题（可选）</span><input defaultValue={text("title_original")} maxLength={500} name="title_original" /></label>
        <label className="wide"><span>厂牌 ID（可选）</span><input defaultValue={text("studio_id")} name="studio_id" pattern="[0-9a-fA-F-]{36}" /></label>
        <label className="wide"><span>人物 ID（最多 20 个）</span><textarea defaultValue={performerIDs} name="performer_ids" rows={3} /></label>
        <label className="wide"><span>摘要（可选）</span><textarea defaultValue={text("summary")} maxLength={5000} name="summary" rows={4} /></label>
      </> : null}
      {latest.entity_type === "performer" ? <>
        <label><span>公开艺名</span><input defaultValue={text("display_name")} maxLength={200} name="display_name" required /></label>
        <label><span>活动状态</span><select defaultValue={text("activity_status") || "unknown"} name="activity_status"><option value="unknown">待核实</option><option value="active">活动中</option><option value="retired">已引退</option></select></label>
        <label><span>原文名</span><input defaultValue={text("name_original")} maxLength={200} name="name_original" /></label>
        <label><span>罗马字</span><input defaultValue={text("romanized_name")} maxLength={200} name="romanized_name" /></label>
        <label className="wide"><span>人物别名（最多 30 个，以逗号或换行分隔）</span><textarea defaultValue={aliases} name="aliases" rows={3} /></label>
        <label><span>成人状态</span><select defaultValue={text("adult_status") || "unverified"} name="adult_status"><option value="unverified">未核验</option><option value="verified">已核验为成年人</option><option value="restricted">限制展示</option></select></label>
        <label><span>所属</span><input defaultValue={text("agency")} maxLength={200} name="agency" /></label>
        <label><span>出生年份</span><input defaultValue={number("birth_year")} max="2200" min="1900" name="birth_year" type="number" /></label>
        <label><span>出道年份</span><input defaultValue={number("debut_year")} max="2200" min="1900" name="debut_year" type="number" /></label>
        <label><span>身高 cm</span><input defaultValue={number("height_cm")} max="250" min="100" name="height_cm" type="number" /></label>
        <label><span>身体参数</span><input defaultValue={text("measurements")} maxLength={200} name="measurements" /></label>
      </> : null}
      {latest.entity_type === "studio" ? <label className="wide"><span>厂牌名称</span><input defaultValue={text("name")} maxLength={200} name="name" required /></label> : null}
      <SourceEvidenceFields />
      <label className="wide"><span>修订理由</span><textarea maxLength={1000} minLength={2} name="reason" required rows={3} /></label>
    </div>{message ? <p className="form-error">{message}</p> : null}<div className="form-actions"><button className="primary" disabled={busy} type="submit"><Save size={17} />{busy ? "提交中" : "提交审核"}</button></div></form>
  </section>;
}
