"use client";

import type { WorkCSVPreflightResponse, WorkImportBatchResponse } from "@self-deepsearch/api-contracts";
import { Download, FileSearch, Upload } from "lucide-react";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { workImportErrorCSV } from "../lib/work-csv-errors.mjs";

const apiBase = "";

export function WorkCSVPreflight({ initialBatches }: { initialBatches: WorkImportBatchResponse[] }) {
  const [report, setReport] = useState<WorkCSVPreflightResponse | null>(null);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [pending, setPending] = useState<{ file: File; reason: string; key: string } | null>(null);
  const [batch, setBatch] = useState<WorkImportBatchResponse | null>(null);
  const [batches, setBatches] = useState(initialBatches);

  async function preflight(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setMessage(""); setReport(null); setPending(null); setBatch(null);
    const data = new FormData(event.currentTarget);
    try {
      const response = await fetch(`${apiBase}/admin/v1/imports/works/preflight`, { method: "POST", credentials: "include", body: data });
      const body = await response.json().catch(() => null) as WorkCSVPreflightResponse | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "预检失败" : "预检失败"); return; }
      const next = body as WorkCSVPreflightResponse;
      setReport(next);
      const file = data.get("file");
      const reason = String(data.get("reason") ?? "").trim();
      if (file instanceof File && next.total_count > 0 && next.invalid_count === 0 && next.file_issues.length === 0) {
        setPending({ file, reason, key: crypto.randomUUID() });
      }
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }

  async function commit() {
    if (!pending) return;
    setBusy(true); setMessage("");
    const data = new FormData(); data.set("file", pending.file); data.set("reason", pending.reason);
    try {
      const response = await fetch(`${apiBase}/admin/v1/imports/works`, { method: "POST", credentials: "include", headers: { "Idempotency-Key": pending.key }, body: data });
      const body = await response.json().catch(() => null) as WorkImportBatchResponse | WorkCSVPreflightResponse | { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body && "error" in body ? body.error?.message ?? "导入失败" : "导入失败，请重新预检"); return; }
      const imported = body as WorkImportBatchResponse;
      setBatch(imported); setPending(null);
      setBatches((current) => [imported, ...current.filter((item) => item.id !== imported.id)]);
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }

  function downloadErrors() {
    if (!report) return;
    const url = URL.createObjectURL(new Blob([workImportErrorCSV(report)], { type: "text/csv;charset=utf-8" }));
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "work-import-errors.csv";
    anchor.click();
    setTimeout(() => URL.revokeObjectURL(url), 0);
  }

  return <>
    <section className="form-surface import-surface">
      <form className="entity-form" onSubmit={preflight}>
        <label><span>CSV 文件</span><input accept=".csv,text/csv" name="file" required type="file" /></label>
        <label><span>导入理由</span><textarea maxLength={1000} minLength={2} name="reason" required rows={3} /></label>
        <p className="import-columns"><code>code,title,title_original,release_date,studio_id,performer_ids,summary</code><br />人物 ID 使用 <code>|</code> 分隔；仅 code、title 必填。</p>
        <div className="form-actions"><button className="primary" disabled={busy} type="submit"><FileSearch size={17} />{busy ? "预检中" : "上传并预检"}</button></div>
      </form>
    </section>
    {message ? <p aria-live="polite" className="form-error dashboard-section">{message}</p> : null}
    {report ? <section className="review-section dashboard-section">
      <div className="section-heading"><div><h2>预检结果</h2><p>共 {report.total_count} 行，可导入 {report.valid_count} 行，需修正 {report.invalid_count} 行</p></div>{report.invalid_count || report.file_issues.length ? <button onClick={downloadErrors} type="button"><Download size={14} />下载错误 CSV</button> : null}</div>
      {report.file_issues.map((issue) => <p className="form-error" key={`${issue.code}-${issue.field}`}>{issue.message}（{issue.code}）</p>)}
      <div className="table-wrap"><table className="import-table"><thead><tr><th>行</th><th>番号</th><th>标题</th><th>发行日期</th><th>结果</th></tr></thead><tbody>
        {report.rows.map((row) => <tr key={row.row_number}><td>{row.row_number}</td><td>{row.code || "-"}</td><td>{row.title || "-"}</td><td>{row.release_date || "-"}</td><td>{row.issues.length ? row.issues.map((issue) => <span className="import-issue" key={`${issue.field}-${issue.code}`}>{issue.message}</span>) : <span className="status ok">通过</span>}</td></tr>)}
      </tbody></table></div>
      {pending ? <div className="form-actions import-commit"><button className="primary" disabled={busy} onClick={commit} type="button"><Upload size={16} />{busy ? "导入中" : `导入 ${report.valid_count} 条并提交审核`}</button></div> : null}
    </section> : null}
    {batch ? <p className="form-success dashboard-section">批次已完成：{batch.accepted_count} 条作品进入审核队列。批次 ID：<code>{batch.id}</code></p> : null}
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>最近导入批次</h2><p>仅显示人工 CSV 作品批次</p></div></div>
      <div className="table-wrap"><table><thead><tr><th>状态</th><th>批次 ID</th><th>输入</th><th>已接受</th><th>完成时间</th></tr></thead><tbody>
        {batches.map((item) => <tr key={item.id}><td><span className="status ok">{item.status === "accepted" ? "已完成" : item.status}</span></td><td><code>{item.id}</code></td><td>{item.input_count}</td><td>{item.accepted_count}</td><td><time dateTime={item.completed_at}>{formatDateTime(item.completed_at)}</time></td></tr>)}
        {!batches.length ? <tr><td className="empty-state" colSpan={5}>暂无人工 CSV 导入批次</td></tr> : null}
      </tbody></table></div>
    </section>
  </>;
}
