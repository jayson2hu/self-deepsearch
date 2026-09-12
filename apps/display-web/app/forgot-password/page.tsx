import type { Metadata } from "next";
import Link from "next/link";
import { PasswordResetForm } from "../../components/password-reset-form";
import { turnstilePublicConfig } from "../../lib/turnstile-config";

export const metadata: Metadata = { title: "重置密码", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default function ForgotPasswordPage() {
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">邮箱验证</p><h1>重置密码</h1><PasswordResetForm turnstile={turnstilePublicConfig()} /><div className="auth-links"><Link href="/login">返回登录</Link></div></section></main>;
}
