"use client";

import type { MessageResponse } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { authRequest, reloadAfterAuthChange } from "../lib/auth-client";

const noticeVersion = "2026-08-07";

export function CloseAccountForm() {
  const [sent, setSent] = useState(false);
  const [confirmed, setConfirmed] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function requestCode() {
    setBusy(true); setError("");
    try {
      const response = await authRequest<MessageResponse>("/api/v1/account/close/code");
      setMessage(response.message); setSent(true);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "验证码发送失败"); }
    finally { setBusy(false); }
  }

  async function close(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!confirmed) { setError("请先确认关闭账号说明"); return; }
    const data = new FormData(event.currentTarget);
    setBusy(true); setError("");
    try {
      await authRequest<MessageResponse>("/api/v1/account/close", { code: data.get("code"), notice_version: noticeVersion });
	  reloadAfterAuthChange();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "关闭账号失败"); setBusy(false); }
  }

  return (
    <div className="danger-zone">
      <h2>关闭账号</h2>
      <p>关闭账号后将无法登录，收藏、关注和浏览记录将不再对你展示。部分操作记录、审计记录和隐私说明约定的信息会继续保存。</p>
      {!sent ? <button className="button button--danger" type="button" onClick={requestCode} disabled={busy}>{busy ? "发送中…" : "发送关闭验证码"}</button> : (
        <form className="auth-form auth-form--compact" onSubmit={close}>
          <label>邮箱验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} /></label>
          <label className="check-row"><input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} />我已阅读并理解上述保留说明</label>
          <button className="button button--danger" type="submit" disabled={busy || !confirmed}>{busy ? "处理中…" : "关闭账号"}</button>
        </form>
      )}
      {message && <p className="form-message" role="status">{message}</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
    </div>
  );
}
