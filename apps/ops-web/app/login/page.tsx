import { redirect } from "next/navigation";
import { LoginForm } from "../../components/login-form";
import { getOperator } from "../../lib/api";

export default async function LoginPage() {
  if (await getOperator()) redirect("/");
  return (
    <main className="auth-page">
      <section className="auth-panel" aria-labelledby="login-title">
        <div className="auth-brand"><strong>幕鉴</strong><span>运营后台</span></div>
        <h1 id="login-title">后台登录</h1>
        <p>仅限 owner、admin 或 editor 角色账号。</p>
        <LoginForm />
      </section>
    </main>
  );
}
