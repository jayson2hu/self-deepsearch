import type { Metadata } from "next";
import Link from "next/link";
import { PasswordResetForm } from "../../components/password-reset-form";
import { authPath, returnTargetFromQuery } from "../../lib/return-target";
import { turnstilePublicConfig } from "../../lib/turnstile-config";

export const metadata: Metadata = { title: "重置密码", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default async function ForgotPasswordPage({ searchParams }: { searchParams: Promise<{ returnTo?: string | string[] }> }) {
  const returnTo = returnTargetFromQuery((await searchParams).returnTo);
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">邮箱验证</p><h1>重置密码</h1><PasswordResetForm returnTo={returnTo} turnstile={turnstilePublicConfig()} /><div className="auth-links"><Link href={authPath("/login", returnTo)}>返回登录</Link></div></section></main>;
}
