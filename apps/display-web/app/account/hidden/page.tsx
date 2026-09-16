import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { HiddenWorksManager } from "../../../components/hidden-works-manager";
import { getCurrentUser, getHiddenWorks } from "../../../lib/api";

export const metadata: Metadata = { title: "推荐偏好", robots: { index: false, follow: false } };

export default async function HiddenWorksPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getHiddenWorks();
  return <main className="shell account-page"><header><p className="eyebrow">个人设置</p><h1>推荐偏好</h1><p>这些作品不会出现在被动推荐中，但仍可通过搜索、目录、人物或厂牌资料找到。</p></header>{data === null ? <div className="state-note state-note--warning" role="alert">推荐偏好暂时不可用，请稍后重试。</div> : <HiddenWorksManager initialItems={data.items} />}</main>;
}
