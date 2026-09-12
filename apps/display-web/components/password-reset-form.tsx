"use client";

import type { MessageResponse } from "@self-deepsearch/api-contracts";
import { useCallback, useState } from "react";
import { authRequest } from "../lib/auth-client";
import { TurnstileField } from "./turnstile-field";
import type { TurnstilePublicConfig } from "../lib/turnstile-config";

export function PasswordResetForm({ turnstile }: { turnstile: TurnstilePublicConfig }) {
  const [token, setToken] = useState("");
  const [sent, setSent] = useState(false);
  const [done, setDone] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [challengeReset, setChallengeReset] = useState(0);
  const receiveToken = useCallback((value: string) => setToken(value), []);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = event.currentTarget;
    const data = new FormData(form);
    setBusy(true); setError("");
    try {
      if (!sent) {
        if (!token) throw new Error("请完成人机验证");
        const response = await authRequest<MessageResponse>("/api/v1/auth/password/code", { email: data.get("email"), turnstile_token: token });
        setMessage(response.message); setSent(true);
      } else {
        if (data.get("password") !== data.get("password_confirm")) throw new Error("两次输入的密码不一致");
        const response = await authRequest<MessageResponse>("/api/v1/auth/password/reset", { email: data.get("email"), code: data.get("code"), password: data.get("password") });
        setMessage(response.message); setDone(true);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "请求失败，请稍后重试");
      if (!sent) {
        setToken("");
        setChallengeReset((current) => current + 1);
      }
    }
    finally { setBusy(false); }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <label>邮箱<input name="email" type="email" autoComplete="email" required maxLength={320} readOnly={sent} /></label>
      {!sent && <TurnstileField action="password_code" allowDevelopmentBypass={turnstile.allowDevelopmentBypass} onToken={receiveToken} resetKey={challengeReset} siteKey={turnstile.siteKey} />}
      {sent && !done && <>
        <label>邮箱验证码<input name="code" inputMode="numeric" autoComplete="one-time-code" required pattern="[0-9]{6}" maxLength={6} /></label>
        <label>新密码<input name="password" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
        <label>确认新密码<input name="password_confirm" type="password" autoComplete="new-password" required minLength={12} maxLength={128} /></label>
      </>}
      {message && <p className="form-message" role="status">{message}</p>}
      {error && <p className="form-error" role="alert">{error}</p>}
      {!done && <button className="button button--primary auth-submit" type="submit" disabled={busy || (!sent && !token)}>{busy ? "处理中…" : sent ? "重置密码" : "发送验证码"}</button>}
    </form>
  );
}
