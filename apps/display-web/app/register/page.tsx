import type { Metadata } from "next";
import Link from "next/link";
import { RegisterForm } from "../../components/register-form";
import { turnstilePublicConfig } from "../../lib/turnstile-config";

export const metadata: Metadata = { title: "注册", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default function RegisterPage() {
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">邮箱验证</p><h1>注册账号</h1><RegisterForm turnstile={turnstilePublicConfig()} /><div className="auth-links"><Link href="/login">已有账号，去登录</Link></div></section></main>;
}
