/* Local or reviewed remote creative images intentionally use their declared ad dimensions. */
/* eslint-disable @next/next/no-img-element */

import { resolveAdSlot, type AdPlacement } from "../lib/ad-policy";

type AdSlotProps = { label: string; placement: AdPlacement; preview?: boolean; slot: string };

export function AdSlot({ label, placement, preview = false, slot }: AdSlotProps) {
  const result = resolveAdSlot(placement, label, process.env, new Date(), preview, slot);
  if (!("creative" in result)) return null;
  const { creative } = result;
  const content = <>
    <img alt={creative.imageAlt} height={creative.height} src={creative.imageUrl} width={creative.width} />
    <span className="ad-slot__copy"><span className="ad-slot__mark">广告</span><strong>{creative.title}</strong><small>{result.state === "preview" ? "测试素材 · 生产环境禁用" : creative.sponsor}</small></span>
  </>;
  return (
    <aside
      className={`ad-slot ad-slot--${placement}`}
      aria-label={`广告：${creative.title}，广告主 ${creative.sponsor}`}
      data-ad-placement={placement}
      data-ad-slot={creative.slot}
      data-ad-state={result.state}
    >
      {creative.targetUrl
        ? <a aria-label={`广告：${creative.title}，广告主 ${creative.sponsor}`} href={creative.targetUrl} rel="noopener noreferrer sponsored" target="_blank">{content}</a>
        : <div>{content}</div>}
    </aside>
  );
}
