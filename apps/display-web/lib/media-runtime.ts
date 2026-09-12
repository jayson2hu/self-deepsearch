import { mediaDefaultOnly } from "./media-policy";

const policyPath = "/internal/v1/media-delivery-policy";

export type MediaRuntimePolicy = {
  defaultOnly: boolean;
  available: boolean;
};

// Invalid non-empty values fail safe. Deployment validation still rejects
// them so a typo cannot silently become a permanent operating mode.
export function mediaDeliveryDynamicEnabled(mode = process.env.MEDIA_DELIVERY_DYNAMIC_MODE): boolean {
  return mode !== undefined && mode !== "off";
}

export async function readMediaRuntimePolicy(): Promise<MediaRuntimePolicy> {
  if (mediaDefaultOnly()) return { defaultOnly: true, available: true };
  if (!mediaDeliveryDynamicEnabled()) return { defaultOnly: false, available: true };

  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  try {
    const response = await fetch(new URL(policyPath, baseURL), {
      headers: { Accept: "application/json" },
      cache: "no-store",
      signal: AbortSignal.timeout(1000),
    });
    if (response.status !== 200 || !response.headers.get("content-type")?.toLowerCase().startsWith("application/json")) {
      return { defaultOnly: true, available: false };
    }
    const body = await response.text();
    if (body.length > 64) return { defaultOnly: true, available: false };
    const value = JSON.parse(body) as unknown;
    if (value === null || typeof value !== "object" || Array.isArray(value)) return { defaultOnly: true, available: false };
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length !== 1 || (record.mode !== "normal" && record.mode !== "default_only")) {
      return { defaultOnly: true, available: false };
    }
    if (response.headers.get("x-media-delivery-mode") !== record.mode ||
        !response.headers.get("cache-control")?.toLowerCase().includes("no-store")) {
      return { defaultOnly: true, available: false };
    }
    return { defaultOnly: record.mode === "default_only", available: true };
  } catch {
    return { defaultOnly: true, available: false };
  }
}
