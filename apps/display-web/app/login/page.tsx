import type { Metadata } from "next";
import Link from "next/link";
import { LoginForm } from "../../components/login-form";

export const metadata: Metadata = { title: "登录", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default function LoginPage() {
  return <main className="auth-page"><section className="auth-panel"><p className="eyebrow">账号</p><h1>登录</h1><LoginForm /><div className="auth-links"><Link href="/forgot-password">忘记密码</Link><Link href="/invite">接受邀请</Link><Link href="/register">注册新账号</Link></div></section></main>;
}
