"use client";

/* Reviewed ad images intentionally use their declared dimensions. */
/* eslint-disable @next/next/no-img-element */

import type { PublicAdResponse } from "@self-deepsearch/api-contracts";
import { useEffect, useState } from "react";
import { resolveAdCreative, type AdCreative, type AdPlacement } from "../lib/ad-policy";

export function LiveAdSlot({ assetHosts, initialCreative, placement, polling, preview, slot, targetHosts }: {
  assetHosts: string;
  initialCreative: AdCreative | null;
  placement: AdPlacement;
  polling: boolean;
  preview: boolean;
  slot: string;
  targetHosts: string;
}) {
  const [creative, setCreative] = useState(initialCreative);

  useEffect(() => {
    if (!polling || placement === "footer") return;
    let active = true;
    const refresh = async () => {
      try {
        const response = await fetch(`/api/v1/site/ads?slot=${encodeURIComponent(slot)}&placement=${encodeURIComponent(placement)}`, {
          cache: "no-store", credentials: "same-origin", signal: AbortSignal.timeout(5000),
        });
        if (!response.ok) throw new Error("ad service unavailable");
        const body = await response.json() as PublicAdResponse;
        if (!active) return;
        if (!body.creative) {
          setCreative(null);
          return;
        }
        const result = resolveAdCreative(body.creative, placement, {
          DISPLAY_AD_ASSET_HOSTS: assetHosts, DISPLAY_AD_TARGET_HOSTS: targetHosts,
        }, new Date(), slot);
        setCreative("creative" in result ? result.creative : null);
      } catch {
        if (active) setCreative(null);
      }
    };
    const timer = window.setInterval(refresh, 30_000);
    const onVisibility = () => { if (document.visibilityState === "visible") void refresh(); };
    document.addEventListener("visibilitychange", onVisibility);
    void refresh();
    return () => { active = false; window.clearInterval(timer); document.removeEventListener("visibilitychange", onVisibility); };
  }, [assetHosts, placement, polling, slot, targetHosts]);

  if (!creative) return null;
  const content = <>
    <img alt={creative.imageAlt} height={creative.height} src={creative.imageUrl} width={creative.width} />
    <span className="ad-slot__copy"><span className="ad-slot__mark">广告</span><strong>{creative.title}</strong><small>{preview ? "测试素材 · 生产环境禁用" : creative.sponsor}</small></span>
  </>;
  return <aside className={`ad-slot ad-slot--${placement}`} aria-label={`广告：${creative.title}，广告主 ${creative.sponsor}`}
    data-ad-device={creative.deviceScope} data-ad-placement={placement} data-ad-slot={creative.slot} data-ad-state={preview ? "preview" : "ready"}>
    {creative.targetUrl
      ? <a aria-label={`广告：${creative.title}，广告主 ${creative.sponsor}`} href={creative.targetUrl} rel="noopener noreferrer sponsored" target="_blank">{content}</a>
      : <div>{content}</div>}
  </aside>;
}
