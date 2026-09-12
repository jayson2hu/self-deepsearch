"use client";

import { LogIn } from "lucide-react";
import { useRouter } from "next/navigation";
import { FormEvent, useState } from "react";

export function LoginForm() {
	const router = useRouter();
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    const form = new FormData(event.currentTarget);
    try {
      const base = "";
      const response = await fetch(`${base}/api/v1/auth/login`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: form.get("email"), password: form.get("password") }),
      });
      if (!response.ok) {
        const body = (await response.json().catch(() => null)) as { error?: { message?: string } } | null;
        setMessage(body?.error?.message ?? "登录失败，请稍后重试");
        return;
      }
      const body = (await response.json()) as { user?: { role?: string } };
      if (body.user?.role === "user") {
        try {
          const logout = await fetch(`${base}/api/v1/auth/logout`, {
            method: "POST", credentials: "include", redirect: "error", signal: AbortSignal.timeout(3000),
          });
          if (logout.status !== 204) {
            router.replace("/logout?error=unconfirmed");
            return;
          }
        } catch {
          router.replace("/logout?error=unconfirmed");
          return;
        }
        setMessage("该账号没有运营后台权限");
        return;
      }
		router.push("/");
		router.refresh();
    } catch {
      setMessage("无法连接账号服务");
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="auth-form" onSubmit={submit}>
      <label htmlFor="email">邮箱</label>
      <input autoComplete="username" id="email" name="email" required type="email" />
      <label htmlFor="password">密码</label>
      <input autoComplete="current-password" id="password" minLength={12} name="password" required type="password" />
      {message ? <p aria-live="polite" className="form-error">{message}</p> : null}
      <button className="primary" disabled={busy} type="submit"><LogIn size={17} />{busy ? "登录中" : "登录"}</button>
    </form>
  );
}
