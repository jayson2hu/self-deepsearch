"use client";

import type { MessageResponse, SessionResponse } from "@self-deepsearch/api-contracts";
import { useCallback, useState } from "react";
import { authRequest, reloadAfterAuthChange } from "../lib/auth-client";
import { TurnstileField } from "./turnstile-field";
import type { TurnstilePublicConfig } from "../lib/turnstile-config";

export function RegisterForm({ turnstile }: { turnstile: TurnstilePublicConfig }) {
  const [token, setToken] = useState("");
  const [sent, setSent] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [challengeReset, setChallengeReset] = useState(0);
  const receiveToken = useCallback((value: string) => setToken(value), []);

  async function requestCode(form: HTMLFormElement) {
    const data = new FormData(form);
    if (!token) { setError("请完成人机验证"); return; }
    setBusy(true); setError("");
    try {
      const response = await authRequest<MessageResponse>("/api/v1/auth/signup/code", { email: data.get("email"), turnstile_token: token });
      setMessage(response.message); setSent(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "验证码发送失败");
      setToken("");
      setChallengeReset((current) => current + 1);
    }
    finally { setBusy(false); }
  }

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    if (!sent) { await requestCode(form); return; }
    if (data.get("password") !== data.get("password_confirm")) { setError("两次输入的密码不一致"); return; }
    setBusy(true); setError("");
    try {
      await authRequest<SessionResponse>("/api/v1/auth/signup/verify", { email: data.get("email"), code: data.get("code"), password: data.get("password") });
	  reloadAfterAuthChange();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "注册失败，请稍后重试"); setBusy(false); }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <label>邮箱<input name="email" type="email" autoComplete="email" required maxLength={320} readOnly={sent} /></label>
      {!sent ? <TurnstileField action="signup_code" allowDevelopmentBypass={turnstile.allowDevelopmentBypass} onToken={receiveToken} resetKey={challengeReset} siteKey={turnstile.siteKey} /> : null}
      {sent && <>
        <label>邮箱验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} /></label>
        <label>密码<input name="password" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
        <label>确认密码<input name="password_confirm" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
      </>}
      {message && <p className="form-message" role="status">{message}</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="button button--primary auth-submit" type="submit" disabled={busy || (!sent && !token)}>{busy ? "处理中…" : sent ? "完成注册" : "发送验证码"}</button>
    </form>
  );
}
