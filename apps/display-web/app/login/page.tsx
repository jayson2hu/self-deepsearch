import type { Metadata } from "next";
import Link from "next/link";
import { LoginForm } from "../../components/login-form";
import { authPath, returnTargetFromQuery } from "../../lib/return-target";

export const metadata: Metadata = { title: "登录", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ returnTo?: string | string[] }> }) {
  const returnTo = returnTargetFromQuery((await searchParams).returnTo);
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">账号</p><h1>登录</h1><LoginForm returnTo={returnTo} /><div className="auth-links"><Link href={authPath("/forgot-password", returnTo)}>忘记密码</Link><Link href="/invite">接受邀请</Link><Link href={authPath("/register", returnTo)}>注册新账号</Link></div></section></main>;
}
