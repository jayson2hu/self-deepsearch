import type { Metadata } from "next";
import Link from "next/link";
import { InviteAcceptForm } from "../../components/invite-accept-form";

export const metadata: Metadata = { title: "接受邀请", robots: { index: false, follow: false } };
export const dynamic = "force-dynamic";

export default function InvitePage() {
  return (
    <main className="auth-page">
      <section className="auth-panel">
        <p className="eyebrow">邮箱验证</p>
        <h1>接受邀请</h1>
        <InviteAcceptForm />
        <div className="auth-links"><Link href="/login">返回登录</Link><Link href="/register">注册新账号</Link></div>
      </section>
    </main>
  );
}
