import type { PublicAdResponse } from "@self-deepsearch/api-contracts";
import { resolveAdCreative, resolveAdSlot, type AdPlacement, type AdSlotResolution } from "../lib/ad-policy";
import { LiveAdSlot } from "./live-ad-slot";

type AdSlotProps = { label: string; placement: AdPlacement; preview?: boolean; slot: string };

export async function AdSlot({ label, placement, preview = false, slot }: AdSlotProps) {
  const source = process.env.DISPLAY_AD_SOURCE === "backend" ? "backend" : "env";
  let result: AdSlotResolution;
  if (source === "backend" && !preview && process.env.DISPLAY_ADS_ENABLED === "true") {
    result = await fetchBackendAd(slot, placement);
  } else {
    result = resolveAdSlot(placement, label, process.env, new Date(), preview, slot);
  }
  if (source !== "backend" && !("creative" in result)) return null;
  return <LiveAdSlot
    assetHosts={process.env.DISPLAY_AD_ASSET_HOSTS ?? ""}
    initialCreative={"creative" in result ? result.creative : null}
    placement={placement}
    polling={source === "backend" && !preview && process.env.DISPLAY_ADS_ENABLED === "true"}
    preview={result.state === "preview"}
    slot={slot}
    targetHosts={process.env.DISPLAY_AD_TARGET_HOSTS ?? ""}
  />;
}

async function fetchBackendAd(slot: string, placement: AdPlacement): Promise<AdSlotResolution> {
  if (placement === "footer") return { state: "empty" };
  const base = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  try {
    const url = new URL("/api/v1/site/ads", base);
    url.searchParams.set("slot", slot);
    url.searchParams.set("placement", placement);
    const response = await fetch(url, { cache: "no-store", signal: AbortSignal.timeout(5000) });
    if (!response.ok) return { state: "error" };
    const body = await response.json() as PublicAdResponse;
    if (!body.creative) return { state: "empty" };
    return resolveAdCreative(body.creative, placement, process.env, new Date(), slot);
  } catch {
    return { state: "error" };
  }
}
