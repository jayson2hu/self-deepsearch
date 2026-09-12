import type { Metadata } from "next";
import Link from "next/link";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "确认退出", robots: { index: false, follow: false } };

export default function LogoutPage() {
  return (
    <main className="auth-page">
      <section className="auth-panel" aria-labelledby="logout-title">
        <p className="eyebrow">账号</p>
        <h1 id="logout-title">退出登录</h1>
        <p className="form-error" role="alert">退出尚未确认：暂时无法确认服务器是否已撤销当前会话。</p>
        <p>浏览器暂时保留登录凭据用于重试。</p>
        <p>请重试退出；关闭页面不代表已退出登录。</p>
        <form action="/auth/logout" method="post" className="auth-form">
          <button className="button button--primary" type="submit">重试退出</button>
        </form>
        <div className="auth-links"><Link href="/">返回网站</Link></div>
      </section>
    </main>
  );
}
