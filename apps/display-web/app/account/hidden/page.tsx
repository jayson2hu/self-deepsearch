import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { HiddenWorksManager } from "../../../components/hidden-works-manager";
import { getCurrentUser, getHiddenWorks } from "../../../lib/api";

export const metadata: Metadata = { title: "已隐藏作品", robots: { index: false, follow: false } };

export default async function HiddenWorksPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getHiddenWorks();
  return <main className="shell account-page"><header><p className="eyebrow">发现偏好</p><h1>已隐藏作品</h1><p>这些作品不会出现在你的发现列表中，恢复后会重新参与展示。</p></header>{data === null ? <div className="state-note state-note--warning" role="alert">隐藏偏好暂时不可用，请稍后重试。</div> : <HiddenWorksManager initialItems={data.items} />}</main>;
}
