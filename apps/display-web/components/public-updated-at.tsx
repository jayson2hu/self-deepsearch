import { formatPublicDate } from "../lib/date-time";

export function PublicUpdatedAt({ value }: { value?: string | null }) {
  if (!value) return null;
  const label = formatPublicDate(value);
  if (!label) return null;
  return <p className="public-updated-at">资料更新：<time dateTime={value}>{label}</time></p>;
}
