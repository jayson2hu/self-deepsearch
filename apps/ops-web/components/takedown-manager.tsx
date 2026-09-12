"use client";

import type { TakedownRequest, UserSummary } from "@self-deepsearch/api-contracts";
import { CheckCircle2, Gavel } from "lucide-react";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

const apiBase = "";

export function TakedownManager({ initialItems, operator }: { initialItems: TakedownRequest[]; operator: UserSummary }) {
  const [items, setItems] = useState(initialItems);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy("create"); setMessage("");
    const form = event.currentTarget;
    const data = new FormData(form);
    try {
      const response = await adminFetch(`${apiBase}/admin/v1/takedowns`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          requester_reference: String(data.get("requester_reference") ?? "").trim(),
          entity_type: String(data.get("entity_type") ?? ""),
          entity_id: String(data.get("entity_id") ?? "").trim(),
          evidence_reference: String(data.get("evidence_reference") ?? "").trim() || null,
        }),
      });
      const body = await response.json().catch(() => null) as TakedownRequest | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "登记失败" : "登记失败"); return; }
      setItems((current) => [body as TakedownRequest, ...current]);
      form.reset();
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(null); }
  }

  async function complete(item: TakedownRequest) {
    const resultSummary = window.prompt("填写处理结论。确认后公开资料会立即下架，媒体对象进入物理删除队列。", "权利请求已核验并执行下架");
    if (!resultSummary?.trim()) return;
    setBusy(item.id); setMessage("");
    try {
      const response = await adminFetch(`${apiBase}/admin/v1/takedowns/${item.id}/complete`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ result_summary: resultSummary.trim() }),
      });
      const body = await response.json().catch(() => null) as TakedownRequest | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "下架失败" : "下架失败"); return; }
      setItems((current) => current.map((entry) => entry.id === item.id ? body as TakedownRequest : entry));
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(null); }
  }

  return <>
    {operator.role !== "editor" ? <section className="form-surface rights-form">
      <form className="entity-form" onSubmit={create}>
        <div className="form-grid">
          <label><span>请求人/邮件工单标识</span><input maxLength={500} name="requester_reference" required /></label>
          <label><span>资料类型</span><select name="entity_type"><option value="work">作品</option><option value="performer">人物</option><option value="studio">厂牌</option><option value="media">单个图片资产</option></select></label>
          <label className="wide"><span>资料或图片资产 ID</span><input name="entity_id" pattern="[0-9a-fA-F-]{36}" required /></label>
          <label className="wide"><span>证据引用（可选，不会自动访问）</span><textarea maxLength={2000} name="evidence_reference" rows={3} /></label>
        </div>
        <div className="form-actions"><button className="primary" disabled={busy === "create"} type="submit"><Gavel size={17} />{busy === "create" ? "登记中" : "登记权利请求"}</button></div>
      </form>
    </section> : null}
    {message ? <p aria-live="polite" className="form-error">{message}</p> : null}
    <section className="review-section dashboard-section">
      <div className="section-heading"><div><h2>下架处理记录</h2><p>公开 URL 从数据库撤下后，S3 物理删除由媒体 Worker 继续处理。</p></div></div>
      <div className="table-wrap"><table><thead><tr><th>状态</th><th>类型</th><th>资料 ID</th><th>请求标识</th><th>收到时间</th><th>操作</th></tr></thead><tbody>
        {items.map((item) => <tr key={item.id}>
          <td><span className={`status ${item.status === "completed" ? "ok" : "waiting"}`}>{statusLabel(item.status)}</span></td>
          <td>{typeLabel(item.entity_type)}</td><td><code>{item.entity_id}</code></td><td>{item.requester_reference}</td>
          <td><time dateTime={item.received_at}>{formatDateTime(item.received_at)}</time></td>
          <td>{operator.role !== "editor" && item.status !== "completed" && item.status !== "rejected" ? <button className="publish-button" disabled={busy === item.id} onClick={() => complete(item)} title="执行权利下架" type="button"><CheckCircle2 size={15} />执行下架</button> : "-"}</td>
        </tr>)}
        {!items.length ? <tr><td className="empty-state" colSpan={6}>暂无权利下架记录</td></tr> : null}
      </tbody></table></div>
    </section>
  </>;
}

function statusLabel(status: TakedownRequest["status"]) {
  return ({ received: "待核验", validating: "核验中", approved: "已批准", rejected: "已驳回", completed: "已完成" } as const)[status];
}

function typeLabel(type: TakedownRequest["entity_type"]) {
  return ({ work: "作品", performer: "人物", studio: "厂牌", media: "图片" } as const)[type];
}
