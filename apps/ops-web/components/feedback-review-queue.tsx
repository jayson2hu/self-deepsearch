"use client";

import type { AdminFeedbackItem, UserSummary } from "@self-deepsearch/api-contracts";
import { Check, Hand, Lock, X } from "lucide-react";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

type ReviewStatus = "reviewing" | "accepted" | "rejected" | "closed";

export function FeedbackReviewQueue({ initialItems, user }: { initialItems: AdminFeedbackItem[]; user: UserSummary }) {
  const [items, setItems] = useState(initialItems);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");

  async function review(item: AdminFeedbackItem, status: ReviewStatus) {
    const reason = window.prompt(status === "reviewing" ? "填写开始处理说明" : "填写审核结论");
    if (!reason || reason.trim().length < 2) return;
    setBusy(item.id);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/feedback/${encodeURIComponent(item.id)}/review`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as AdminFeedbackItem | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "处理失败" : "处理失败");
        return;
      }
      const updated = body as AdminFeedbackItem;
      setItems((current) => current.map((candidate) => candidate.id === updated.id ? updated : candidate));
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  if (!items.length) return <div className="empty-state">暂无用户反馈</div>;
  return <>
    {message ? <p className="form-error queue-error" role="alert">{message}</p> : null}
    <div className="feedback-review-list">{items.map((item) => <article key={item.id}>
      <header><div><strong>{feedbackTypeLabel(item.feedback_type)}</strong><span className={`status ${item.review_status === "accepted" ? "ok" : item.review_status === "rejected" ? "waiting" : "neutral"}`}>{statusLabel(item.review_status)}</span></div><time dateTime={item.created_at}>{formatDateTime(item.created_at)}</time></header>
      <p>{item.message}</p>
      <dl><div><dt>提交账号</dt><dd>{item.submitter}</dd></div><div><dt>资料对象</dt><dd>{item.content_type && item.content_id ? `${item.content_type} / ${item.content_id}` : "全站反馈"}</dd></div><div><dt>证据链接</dt><dd>{item.evidence_url ? <code>{item.evidence_url}</code> : "未提供"}</dd></div><div><dt>处理人</dt><dd>{item.reviewer ?? "未领取"}{item.reviewed_at ? <> · <time dateTime={item.reviewed_at}>{formatDateTime(item.reviewed_at)}</time></> : null}</dd></div></dl>
      <div className="row-actions">
        {item.review_status === "pending" ? <button aria-label="开始处理" disabled={busy !== null} onClick={() => review(item, "reviewing")} title="开始处理" type="button"><Hand size={15} /></button> : null}
        {item.review_status === "reviewing" && user.role !== "editor" ? <><button aria-label="接受反馈" className="approve" disabled={busy !== null} onClick={() => review(item, "accepted")} title="接受反馈" type="button"><Check size={15} /></button><button aria-label="驳回反馈" className="reject" disabled={busy !== null} onClick={() => review(item, "rejected")} title="驳回反馈" type="button"><X size={15} /></button><button aria-label="关闭反馈" disabled={busy !== null} onClick={() => review(item, "closed")} title="关闭反馈" type="button"><Lock size={15} /></button></> : null}
      </div>
    </article>)}</div>
  </>;
}

function feedbackTypeLabel(type: AdminFeedbackItem["feedback_type"]) {
  return ({ correction: "资料纠错", source_suggestion: "来源建议", rights: "权利问题", other: "其他反馈" } as const)[type];
}

function statusLabel(status: AdminFeedbackItem["review_status"]) {
  return ({ pending: "待处理", reviewing: "处理中", accepted: "已接受", rejected: "已驳回", closed: "已关闭" } as const)[status];
}
