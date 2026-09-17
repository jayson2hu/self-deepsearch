import { Eye } from "lucide-react";

const number = new Intl.NumberFormat("zh-CN");

export function ViewStats({ last30Days, total }: { last30Days: number; total: number }) {
  return <p className="view-stats" aria-label={`近 30 天浏览 ${last30Days} 次，累计浏览 ${total} 次`}>
    <Eye aria-hidden="true" size={14} />
    <span>近 30 天 <strong>{number.format(last30Days)}</strong></span>
    <span>累计 <strong>{number.format(total)}</strong> 次详情浏览</span>
  </p>;
}
