import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { getCurrentUser } from "../../lib/api";

export const metadata: Metadata = { title: "我的账号", robots: { index: false, follow: false } };

const entries = [
  ["收藏作品", "查看已经明确收藏的公开作品。", "/account/favorites"],
  ["关注人物", "管理关注人物，并进入其已发布作品列表。", "/account/follows"],
  ["推荐偏好", "恢复曾选择“不再推荐”的作品。", "/account/hidden"],
  ["浏览历史", "查看或清除当前账号可见的浏览记录。", "/account/history"],
  ["我的反馈", "查看自己提交的内容与真实处理状态。", "/account/feedback"],
  ["账号设置", "核对账号与关闭账号的当前规则。", "/account/settings"],
] as const;

export default async function AccountPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login?returnTo=%2Faccount");
  return <main className="shell account-page"><header><p className="eyebrow">个人资料</p><h1>我的账号</h1><p>{user.email}</p></header>
    <p className="retention-note">这里不提供未读数量或新消息角标；每个入口只展示对应接口能够证明的当前记录与状态。</p>
    <nav className="account-link-grid" aria-label="我的账号功能">{entries.map(([title, description, href]) => <Link className="account-link-card" href={href} key={href}><strong>{title}</strong><span>{description}</span><em>进入</em></Link>)}</nav>
  </main>;
}
