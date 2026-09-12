import type { Metadata } from "next";
import Link from "next/link";

export const dynamic = "force-dynamic";
export const metadata: Metadata = { title: "确认退出 · 运营后台", robots: { index: false, follow: false } };

export default function LogoutPage() {
  return (
    <main className="auth-page">
      <section className="auth-panel" aria-labelledby="logout-title">
        <div className="auth-brand"><strong>幕鉴</strong><span>运营后台</span></div>
        <h1 id="logout-title">退出登录</h1>
        <p className="form-error" role="alert">退出尚未确认：暂时无法确认服务器是否已撤销当前会话。</p>
        <p>浏览器暂时保留登录凭据用于重试。</p>
        <p>请重试退出；关闭页面不代表已退出登录。</p>
        <form action="/auth/logout" method="post" className="auth-form">
          <button className="primary" type="submit">重试退出</button>
        </form>
        <p><Link href="/">返回后台</Link></p>
      </section>
    </main>
  );
}
