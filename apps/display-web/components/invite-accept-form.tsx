"use client";

import type { SessionResponse } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { authRequest, reloadAfterAuthChange } from "../lib/auth-client";

export function InviteAcceptForm() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    if (form.get("password") !== form.get("password_confirm")) {
      setError("两次输入的密码不一致");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await authRequest<SessionResponse>("/api/v1/auth/invitations/accept", {
        email: form.get("email"),
        code: form.get("code"),
        password: form.get("password"),
      });
      reloadAfterAuthChange();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "接受邀请失败，请稍后重试");
      setBusy(false);
    }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <label>邀请邮箱<input name="email" type="email" autoComplete="email" required maxLength={320} /></label>
      <label>邮箱验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} /></label>
      <label>设置密码<input name="password" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
      <label>确认密码<input name="password_confirm" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
      <p className="form-hint">验证码由管理员发送到邀请邮箱，48 小时内有效且只能使用一次。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="button button--primary auth-submit" type="submit" disabled={busy}>{busy ? "处理中…" : "接受邀请并登录"}</button>
    </form>
  );
}
