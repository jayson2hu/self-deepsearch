"use client";

import type { FeedbackItem } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { authRequest } from "../lib/auth-client";

export function FeedbackForm({ workID }: { workID: string }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget);
    setBusy(true); setError("");
    try {
      await authRequest<FeedbackItem>("/api/v1/feedback", {
        content_type: "work", content_id: workID, feedback_type: data.get("feedback_type"),
        message: data.get("message"), evidence_url: data.get("evidence_url") || null,
      });
      setMessage("已提交，管理员审核后会更新状态。"); event.currentTarget.reset();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "提交失败"); }
    finally { setBusy(false); }
  }
  return <div className="feedback-box"><button className="button button--quiet" type="button" onClick={() => setOpen(!open)}>{open ? "收起反馈" : "纠错与反馈"}</button>{open && <form className="auth-form auth-form--compact" onSubmit={submit}><label>类型<select name="feedback_type" defaultValue="correction"><option value="correction">资料纠错</option><option value="source_suggestion">来源建议</option><option value="rights">权利问题</option><option value="other">其他</option></select></label><label>说明<textarea name="message" required minLength={1} maxLength={4000} rows={5} /></label><label>证据链接（可选）<input name="evidence_url" type="url" maxLength={2048} /></label><button className="button button--primary" type="submit" disabled={busy}>{busy ? "提交中…" : "提交反馈"}</button>{message && <p className="form-message" role="status">{message}</p>}{error && <p className="form-error" role="alert">{error}</p>}</form>}</div>;
}
