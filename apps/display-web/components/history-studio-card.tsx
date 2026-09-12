import type { HomeItem } from "@self-deepsearch/api-contracts";
import { ArrowUpRight, Building2 } from "lucide-react";
import Link from "next/link";

/** Compact history presentation for a studio entry, which has no cover image. */
export function HistoryStudioCard({ item }: { item: HomeItem }) {
  return (
    <article className="history-studio-card">
      <Link
        className="history-studio-card__link"
        href={item.href}
        aria-label={`查看 ${item.title} 的厂牌资料`}
      >
        <span className="history-studio-card__icon" aria-hidden="true">
          <Building2 size={22} />
        </span>
        <span className="history-studio-card__body">
          <span className="history-studio-card__eyebrow">厂牌资料</span>
          <strong>{item.title}</strong>
          <span className="history-studio-card__subtitle">{item.subtitle || "查看已发布作品"}</span>
          <span className="history-studio-card__action">
            查看厂牌资料
            <ArrowUpRight size={14} aria-hidden="true" />
          </span>
        </span>
      </Link>
    </article>
  );
}
