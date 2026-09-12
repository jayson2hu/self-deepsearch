"use client";

import type { OperationsUser, ReviewTask, UserSummary } from "@self-deepsearch/api-contracts";
import { Check, Hand, UserRoundCog, X } from "lucide-react";
import { useState } from "react";
import { adminFetch } from "../lib/reauth-client";

export function ReviewTaskActions({
  task,
  operator,
  canInspectRevision,
  canApprove,
  unavailableReason,
  reviewers,
}: {
  task: ReviewTask;
  operator: UserSummary;
  canInspectRevision: boolean;
  canApprove: boolean;
  unavailableReason?: string;
  reviewers: OperationsUser[];
}) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [selectedReviewer, setSelectedReviewer] = useState("");
  const availabilityID = `review-task-${task.id}-availability`;

  async function act(action: "claim" | "approve" | "reject" | "reassign") {
    const reason = action === "claim" ? null : window.prompt(action === "approve" ? "填写审核通过理由" : action === "reject" ? "填写驳回理由" : "填写改派理由");
    if (action !== "claim" && (!reason || reason.trim().length < 2)) return;
    if (action === "reassign" && !selectedReviewer) {
      setMessage("请选择目标审核人");
      return;
    }
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/review-tasks/${task.id}/${action}`, {
        method: "POST",
        credentials: "include",
        headers: action === "claim" ? undefined : { "Content-Type": "application/json" },
        body: action === "claim" ? undefined : JSON.stringify(action === "reassign" ? { assignee_id: selectedReviewer, reason: reason?.trim() } : { reason: reason?.trim() }),
      });
      const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body?.error?.message ?? "操作失败");
        return;
      }
      window.location.reload();
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  const availabilityMessage = unavailableReason ?? (!canApprove && canInspectRevision ? "当前修订没有来源证据，不能通过；请驳回并要求补充来源。" : "");
  const canClaim = operator.role !== "editor";
  const canDecide = canClaim && task.assignee_id === operator.id;
  const assignedToOther = task.status === "claimed" && !canDecide;
  const canReassign = task.status === "claimed" && operator.role !== "editor" && reviewers.length > 0;

  return <div aria-busy={busy} className="review-detail-actions">
    {task.status === "pending" && canClaim ? <button aria-describedby={availabilityMessage ? availabilityID : undefined} className="primary" disabled={busy || !canInspectRevision} onClick={() => act("claim")} type="button"><Hand size={16} />领取审核</button> : null}
    {task.status === "claimed" && canDecide ? <>
      <button aria-describedby={availabilityMessage ? availabilityID : undefined} className="primary" disabled={busy || !canInspectRevision || !canApprove} onClick={() => act("approve")} type="button"><Check size={16} />审核通过</button>
      <button aria-describedby={unavailableReason ? availabilityID : undefined} className="danger" disabled={busy || !canInspectRevision} onClick={() => act("reject")} type="button"><X size={16} />驳回修订</button>
    </> : null}
    {canReassign ? <div className="review-reassign"><label htmlFor={`${availabilityID}-assignee`}>改派给</label><select id={`${availabilityID}-assignee`} disabled={busy} onChange={(event) => setSelectedReviewer(event.target.value)} value={selectedReviewer}><option value="">选择管理员</option>{reviewers.map((reviewer) => <option disabled={reviewer.id === task.assignee_id} key={reviewer.id} value={reviewer.id}>{reviewer.email} · {reviewer.role === "owner" ? "所有者" : "管理员"}</option>)}</select><button aria-label="重新分配审核任务" className="secondary" disabled={busy || !selectedReviewer || !reviewers.some((reviewer) => reviewer.id !== task.assignee_id && reviewer.id === selectedReviewer)} onClick={() => act("reassign")} title="重新分配审核任务" type="button"><UserRoundCog size={16} />改派</button></div> : null}
    {assignedToOther ? <p className="muted-text">该任务已由 {task.assignee ?? "其他管理员"} 领取，当前账号不能处理。</p> : null}
    {availabilityMessage ? <p className="form-error" id={availabilityID} role={unavailableReason ? "alert" : "status"}>{availabilityMessage}</p> : null}
    {message ? <p className="form-error" role="alert">{message}</p> : null}
  </div>;
}
