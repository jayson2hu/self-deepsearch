"use client";

import type { SEOReportImport, UserSummary } from "@self-deepsearch/api-contracts";
import { Upload } from "lucide-react";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

export function SEOReportManager({ initialItems, operatorRole }: { initialItems: SEOReportImport[]; operatorRole: UserSummary["role"] }) {
  const [items, setItems] = useState(initialItems);
  const [reportType, setReportType] = useState<"performance" | "indexing">("performance");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const isOwner = operatorRole === "owner";

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    const exportedAt = String(data.get("exported_at") ?? "");
    if (exportedAt) data.set("exported_at", new Date(exportedAt).toISOString());
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch("/admin/v1/seo/reports/import", { method: "POST", body: data });
      const body = await response.json().catch(() => null) as SEOReportImport | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "导入失败" : "导入失败");
        return;
      }
      const saved = body as SEOReportImport;
      setItems((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
      setMessage(saved.dataset_kind === "demo" ? "演示报表已导入，并与真实报表隔离。" : "真实来源报表已导入；请保留授权与导出证据供验收。 ");
      form.reset();
      setReportType("performance");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  const real = items.filter((item) => item.dataset_kind === "real");
  const demo = items.filter((item) => item.dataset_kind === "demo");
  return <div className="seo-report-manager">
    {isOwner ? <form className="entity-form seo-report-form" onSubmit={submit}><div className="form-grid">
      <label><span>报表类型</span><select name="report_type" value={reportType} onChange={(event) => setReportType(event.target.value as typeof reportType)}><option value="performance">搜索表现</option><option value="indexing">索引状态</option></select></label>
      <label><span>数据标识</span><select name="dataset_kind" defaultValue="real"><option value="real">真实授权导出</option><option value="demo">演示数据</option></select></label>
      <label className="wide"><span>Search Console 资源</span><input name="property_uri" placeholder="https://example.com/ 或 sc-domain:example.com" required /></label>
      <label><span>数据开始日期</span><input name="period_start" required type="date" /></label><label><span>数据结束日期</span><input name="period_end" required type="date" /></label>
      <label><span>官方导出时间</span><input name="exported_at" required type="datetime-local" /></label><label><span>CSV 文件</span><input accept=".csv,text/csv" name="file" required type="file" /></label>
      <p className="wide seo-format-hint">{reportType === "performance" ? "表头：query,page,country,device,date,clicks,impressions,ctr,position" : "表头：page,status,reason,last_crawled"}。最大 5 MiB / 10,000 行；公式单元格会被拒绝。</p>
    </div>{message ? <p aria-live="polite" className={message.includes("失败") || message.includes("无法") ? "form-error" : "form-success"}>{message}</p> : null}<div className="form-actions"><button className="primary" disabled={busy} type="submit"><Upload size={16} />{busy ? "导入中" : "导入官方导出"}</button></div></form> : <div className="phase-note"><div><strong>仅 owner 可以导入</strong><p>管理员可查看来源、时间区间、完整性和真实/演示标识。</p></div></div>}
    <ReportTable items={real} title="真实授权导出" empty="尚未导入真实报表；没有数据时不展示虚构的点击、展现或收录量。" />
    <ReportTable items={demo} title="演示数据" empty="没有演示报表。" />
  </div>;
}

function ReportTable({ items, title, empty }: { items: SEOReportImport[]; title: string; empty: string }) {
  return <section className="seo-report-list"><div className="section-heading"><div><h2>{title}</h2><p>{items.length} 份导入记录</p></div></div><div className="table-wrap"><table><thead><tr><th>类型</th><th>资源</th><th>数据区间</th><th>导出/导入</th><th>完整性</th></tr></thead><tbody>
    {items.map((item) => <tr key={item.id}><td><span className={`status ${item.dataset_kind === "demo" ? "waiting" : "ok"}`}>{item.dataset_kind === "demo" ? "演示" : "真实"}</span><small>{item.report_type}</small></td><td>{item.property_uri}</td><td>{item.period_start} 至 {item.period_end}</td><td>导出 {formatDateTime(item.exported_at)}<small>导入 {formatDateTime(item.created_at)} · {item.imported_by}</small></td><td>{item.row_count} 行<small>SHA-256 {item.file_sha256.slice(0, 16)}…</small></td></tr>)}
    {!items.length ? <tr><td className="empty-state" colSpan={5}>{empty}</td></tr> : null}
  </tbody></table></div></section>;
}
