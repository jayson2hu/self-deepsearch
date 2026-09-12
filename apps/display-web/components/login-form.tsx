"use client";

import type { SessionResponse } from "@self-deepsearch/api-contracts";
import { useState } from "react";
import { authRequest, reloadAfterAuthChange } from "../lib/auth-client";

export function LoginForm() {
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    setBusy(true); setError("");
    try {
      await authRequest<SessionResponse>("/api/v1/auth/login", { email: form.get("email"), password: form.get("password") });
	  reloadAfterAuthChange();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "登录失败，请稍后重试");
      setBusy(false);
    }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <label>邮箱<input name="email" type="email" autoComplete="email" required maxLength={320} /></label>
      <label>密码<input name="password" type="password" autoComplete="current-password" required minLength={12} maxLength={128} /></label>
      {error && <p className="form-error" role="alert">{error}</p>}
      <button className="button button--primary auth-submit" type="submit" disabled={busy}>{busy ? "登录中…" : "登录"}</button>
    </form>
  );
}
