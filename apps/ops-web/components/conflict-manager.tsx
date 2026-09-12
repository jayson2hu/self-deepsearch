"use client";

import type { ConflictResolution, ConflictReview, UserSummary } from "@self-deepsearch/api-contracts";
import { CheckCircle2 } from "lucide-react";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

const apiBase = "";
const decisions: Array<{ value: ConflictResolution; label: string }> = [
  { value: "keep_current", label: "保留现值" },
  { value: "accept_candidate", label: "接受候选" },
  { value: "mark_unknown", label: "标记未知" },
  { value: "merge", label: "合并记录" },
  { value: "reject", label: "拒绝候选" },
];

export function ConflictManager({ initialItems, operator }: { initialItems: ConflictReview[]; operator: UserSummary }) {
  const [items, setItems] = useState(initialItems);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [resolutions, setResolutions] = useState<Record<string, ConflictResolution>>({});
  const [reasons, setReasons] = useState<Record<string, string>>({});

  async function resolve(item: ConflictReview) {
    const resolution = resolutions[item.id] ?? "keep_current";
    const reason = reasons[item.id]?.trim() ?? "";
    if (reason.length < 2) { setMessage("请填写至少 2 个字符的裁决理由"); return; }
    setBusy(item.id); setMessage("");
    try {
      const response = await adminFetch(`${apiBase}/admin/v1/conflicts/${item.id}/resolve`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ resolution, reason }),
      });
      const body = await response.json().catch(() => null) as ConflictReview | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "裁决失败" : "裁决失败"); return; }
      setItems((current) => current.map((entry) => entry.id === item.id ? body as ConflictReview : entry));
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(null); }
  }

  return <section className="review-section dashboard-section">
    <div className="section-heading"><div><h2>冲突记录</h2><p>接受候选值后仍需在资料目录创建修订，并完成审核与发布。</p></div></div>
    {message ? <p aria-live="polite" className="form-error queue-error">{message}</p> : null}
    <div className="table-wrap"><table className="conflict-table"><thead><tr><th>状态 / 字段</th><th>资料</th><th>现值</th><th>候选值</th><th>来源</th><th>裁决</th></tr></thead><tbody>
      {items.map((item) => <tr key={item.id}>
        <td><span className={`status ${item.resolution ? "ok" : "waiting"}`}>{item.resolution ? resolutionLabel(item.resolution) : "待处理"}</span><strong className="conflict-field">{item.field_name}</strong></td>
        <td><span>{typeLabel(item.entity_type)}</span><code>{item.entity_id}</code></td>
        <td><pre className="conflict-value">{formatValue(item.current_value)}</pre></td>
        <td><pre className="conflict-value">{formatValue(item.candidate_value)}</pre></td>
        <td>{item.source_name ?? "人工/未登记"}<small className="conflict-source">{item.source_priority ? `优先级 ${item.source_priority}` : "无优先级"}</small></td>
        <td>{item.resolution ? <span>{item.resolved_by ?? "已处理"}<small className="conflict-source">{item.resolved_at ? <time dateTime={item.resolved_at}>{formatDateTime(item.resolved_at)}</time> : null}</small></span> : operator.role === "editor" ? "仅查看" : <div className="conflict-decision">
          <label className="sr-only" htmlFor={`resolution-${item.id}`}>裁决结果</label>
          <select id={`resolution-${item.id}`} value={resolutions[item.id] ?? "keep_current"} onChange={(event) => setResolutions((current) => ({ ...current, [item.id]: event.target.value as ConflictResolution }))}>{decisions.map((decision) => <option key={decision.value} value={decision.value}>{decision.label}</option>)}</select>
          <label className="sr-only" htmlFor={`reason-${item.id}`}>裁决理由</label>
          <input id={`reason-${item.id}`} maxLength={1000} onChange={(event) => setReasons((current) => ({ ...current, [item.id]: event.target.value }))} placeholder="裁决理由" value={reasons[item.id] ?? ""} />
          <button className="publish-button" disabled={busy === item.id} onClick={() => resolve(item)} type="button"><CheckCircle2 size={14} />确认</button>
        </div>}</td>
      </tr>)}
      {!items.length ? <tr><td className="empty-state" colSpan={6}>暂无字段冲突</td></tr> : null}
    </tbody></table></div>
  </section>;
}

function formatValue(value: unknown) {
  if (value === null || value === undefined) return "（空）";
  return typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

function resolutionLabel(value: ConflictResolution) {
  return decisions.find((decision) => decision.value === value)?.label ?? value;
}

function typeLabel(value: ConflictReview["entity_type"]) {
  return ({ work: "作品", performer: "人物", studio: "厂牌", media: "图片" } as const)[value];
}
