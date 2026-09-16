import type { Metadata } from "next";
import Link from "next/link";
import { RegisterForm } from "../../components/register-form";
import { authPath, returnTargetFromQuery } from "../../lib/return-target";
import { turnstilePublicConfig } from "../../lib/turnstile-config";

export const metadata: Metadata = { title: "注册", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default async function RegisterPage({ searchParams }: { searchParams: Promise<{ returnTo?: string | string[] }> }) {
  const returnTo = returnTargetFromQuery((await searchParams).returnTo);
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">邮箱验证</p><h1>注册账号</h1><p className="auth-notice">注册前请阅读<Link href="/privacy">数据与隐私说明</Link>，了解浏览历史、清除和关闭账号的当前行为。</p><RegisterForm returnTo={returnTo} turnstile={turnstilePublicConfig()} /><div className="auth-links"><Link href={authPath("/login", returnTo)}>已有账号，去登录</Link><Link href="/about">了解本站</Link></div></section></main>;
}
