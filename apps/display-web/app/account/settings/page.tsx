import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { CloseAccountForm } from "../../../components/close-account-form";
import { getCurrentUser } from "../../../lib/api";

export const metadata: Metadata = { title: "账号设置", robots: { index: false, follow: false } };

export default async function AccountSettingsPage() {
  const user = await getCurrentUser();
  if (!user) redirect("/login");
  return <main className="shell account-page"><header><p className="eyebrow">账号设置</p><h1>账号设置</h1><p>{user.email}</p></header><CloseAccountForm /></main>;
}
