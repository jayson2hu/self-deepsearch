import type { Metadata } from "next";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ClearHistoryButton } from "../../../components/clear-history-button";
import { HistoryStudioCard } from "../../../components/history-studio-card";
import { PerformerCard } from "../../../components/performer-card";
import { WorkCard } from "../../../components/work-card";
import type { HomeItem } from "@self-deepsearch/api-contracts";
import { getCurrentUser, getHistory } from "../../../lib/api";
import { formatDateTime } from "../../../lib/date-time";

export const metadata: Metadata = { title: "浏览历史", robots: { index: false, follow: false } };

export default async function HistoryPage() {
  if (!await getCurrentUser()) redirect("/login");
  const data = await getHistory();
  return (
    <main className="shell account-page">
      <header className="account-heading">
        <div>
          <p className="eyebrow">个人资料</p>
          <h1>浏览历史</h1>
        </div>
        {data !== null ? <ClearHistoryButton /> : null}
      </header>
      <p className="retention-note">清除后这些记录不再向你展示，但不会物理删除底层记录；当前没有停止未来记录的开关。查看<Link href="/privacy">数据与隐私说明</Link>。</p>
      {data === null ? (
        <div className="state-note state-note--warning" role="alert">浏览历史暂时不可用，请稍后重试。</div>
      ) : data.items.length ? (
        <div className="history-list">
          {data.items.map((entry, index) => (
            <div key={`${entry.item.id}-${entry.viewed_at}`}>
              <time dateTime={entry.viewed_at}>
                {formatDateTime(entry.viewed_at)}
              </time>
              <HistoryItemCard item={entry.item} position={index + 1} />
            </div>
          ))}
        </div>
      ) : (
        <div className="empty-state"><strong>暂无可见浏览历史</strong></div>
      )}
    </main>
  );
}

function HistoryItemCard({ item, position }: { item: HomeItem; position: number }) {
  if (item.type === "work") return <WorkCard item={item} position={position} />;
  if (item.type === "performer") return <PerformerCard item={item} />;
  return <HistoryStudioCard item={item} />;
}
