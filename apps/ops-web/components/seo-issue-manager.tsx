"use client";

import type { SEOIssue, SEOScan, UserSummary } from "@self-deepsearch/api-contracts";
import { Play, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

export function SEOIssueManager({ initialIssues, initialScans, operator }: {
  initialIssues: SEOIssue[];
  initialScans: SEOScan[];
  operator: UserSummary;
}) {
  const [issues, setIssues] = useState(initialIssues);
  const [scans, setScans] = useState(initialScans);
  const [filter, setFilter] = useState<SEOIssue["status"] | "all">("all");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  async function runScan() {
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch("/admin/v1/seo/scans", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{}" });
      const body = await response.json().catch(() => null) as SEOScan | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "扫描失败" : "扫描失败");
        return;
      }
      const scan = body as SEOScan;
      setScans((current) => [scan, ...current].slice(0, 20));
      const refreshed = await fetch("/admin/v1/seo/issues?status=all", { credentials: "include", cache: "no-store" });
      if (refreshed.ok) setIssues(((await refreshed.json()) as { items: SEOIssue[] }).items);
      setMessage(`扫描完成：${scan.scanned_count} 个目标，发现或更新 ${scan.issue_count} 个问题${scan.truncated ? "；本批达到 1,000 条上限" : ""}。`);
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  async function transition(issue: SEOIssue) {
    const next = ({ open: "in_progress", in_progress: "retest", retest: "closed" } as const)[issue.status as "open" | "in_progress" | "retest"];
    if (!next) return;
    const note = window.prompt(next === "in_progress" ? "填写处理计划或分派说明" : next === "retest" ? "填写已完成的修复内容" : "填写复测证据与结论");
    if (!note?.trim()) return;
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/seo/issues/${encodeURIComponent(issue.id)}`, {
        method: "PATCH", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: next, assignee_id: operator.id, due_at: null, note: note.trim() }),
      });
      const body = await response.json().catch(() => null) as SEOIssue | { error?: { message?: string } } | null;
      if (!response.ok) {
        if (response.status === 409 && next === "closed") {
          setIssues((current) => current.map((value) => value.id === issue.id ? { ...value, status: "in_progress", resolution_note: note.trim() } : value));
          setMessage("复测未通过：当前页面仍不满足资格规则，问题已退回处理中。");
        } else setMessage(body && "error" in body ? body.error?.message ?? "更新失败" : "更新失败");
        return;
      }
      const saved = body as SEOIssue;
      setIssues((current) => current.map((value) => value.id === saved.id ? saved : value));
      setMessage(saved.status === "closed" ? "复测通过，问题已关闭并保留历史。" : "问题状态已更新。");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  const visible = filter === "all" ? issues : issues.filter((issue) => issue.status === filter);
  return <div className="seo-issue-manager">
    <div className="seo-scan-bar"><div><strong>最近扫描</strong><p>{scans[0] ? `${formatDateTime(scans[0].started_at)} · ${scans[0].status} · 扫描 ${scans[0].scanned_count} · 问题 ${scans[0].issue_count}` : "尚未执行扫描"}</p></div><button className="primary" disabled={busy} type="button" onClick={runScan}><RefreshCw size={16} />{busy ? "处理中" : "立即扫描"}</button></div>
    {message ? <p aria-live="polite" className={message.includes("失败") || message.includes("无法") || message.includes("未通过") ? "form-error" : "form-success"}>{message}</p> : null}
    <div className="seo-issue-filter"><label>状态<select value={filter} onChange={(event) => setFilter(event.target.value as typeof filter)}><option value="all">全部</option><option value="open">待处理</option><option value="in_progress">处理中</option><option value="retest">待复测</option><option value="closed">已关闭</option></select></label><span>当前 {visible.length} 条</span></div>
    <div className="table-wrap"><table className="seo-issue-table"><thead><tr><th>优先级/规则</th><th>页面</th><th>证据</th><th>负责人</th><th>发现时间</th><th>状态与操作</th></tr></thead><tbody>
      {visible.map((issue) => <tr key={issue.id}><td><span className={`status ${issue.priority === "P0" ? "neutral" : issue.priority === "P1" ? "waiting" : "ok"}`}>{issue.priority}</span><code>{issue.rule_code}</code></td><td><strong>{issue.url_path}</strong><small>{issue.entity_type} · {issue.entity_id}</small></td><td><code>{JSON.stringify(issue.evidence)}</code>{issue.resolution_note ? <small>{issue.resolution_note}</small> : null}</td><td>{issue.assignee ?? "未分派"}</td><td>{formatDateTime(issue.first_seen_at)}<small>最近 {formatDateTime(issue.last_seen_at)}</small></td><td><span className={`status ${issue.status === "closed" ? "ok" : "waiting"}`}>{statusLabel(issue.status)}</span><div className="seo-issue-actions"><Link className="button compact-button" href={`/catalog/${issue.entity_type}/${issue.entity_id}`}>修订</Link>{issue.status !== "closed" ? <button disabled={busy} type="button" onClick={() => transition(issue)}><Play size={14} />{actionLabel(issue.status)}</button> : null}</div></td></tr>)}
      {!visible.length ? <tr><td className="empty-state" colSpan={6}>当前筛选没有问题。</td></tr> : null}
    </tbody></table></div>
  </div>;
}

function statusLabel(status: SEOIssue["status"]) {
  return ({ open: "待处理", in_progress: "处理中", retest: "待复测", closed: "已关闭" } as const)[status];
}

function actionLabel(status: SEOIssue["status"]) {
  return ({ open: "开始处理", in_progress: "提交复测", retest: "复测关闭", closed: "" } as const)[status];
}
