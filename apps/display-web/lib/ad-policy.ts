export type AdPlacement = "banner" | "rail" | "infeed" | "footer";

export type AdCreative = {
  slot: string;
  placement: AdPlacement;
  title: string;
  sponsor: string;
  imageUrl: string;
  imageAlt: string;
  targetUrl: string | null;
  width: number;
  height: number;
  deviceScope: "all" | "desktop" | "mobile";
};

export type AdSlotResolution =
  | { state: "empty" | "error" }
  | { state: "ready" | "preview"; creative: AdCreative };

type Environment = Record<string, string | undefined>;

const dimensions: Record<AdPlacement, { width: number; height: number }> = {
  banner: { width: 960, height: 90 },
  rail: { width: 160, height: 600 },
  infeed: { width: 640, height: 120 },
  footer: { width: 960, height: 90 },
};
const safeText = /^[^<>\u0000-\u001f\u007f]{1,120}$/;
const safeAltText = /^[^<>\u0000-\u001f\u007f]{0,160}$/;
const rfc3339Pattern = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;
const creativeKeys = new Set(["slot", "placement", "device_scope", "title", "sponsor", "image_url", "image_alt", "target_url", "width", "height", "starts_at", "ends_at"]);

export function adSlotKey(placement: AdPlacement, label: string): string {
  return `${placement}:${label}`;
}

export function resolveAdSlot(
  placement: AdPlacement,
  label: string,
  environment: Environment = process.env,
  now = new Date(),
  preview = false,
  slotCode = "",
): AdSlotResolution {
  const slot = slotCode.trim() || adSlotKey(placement, label);
  if (!safeText.test(slot)) return { state: "error" };
  if (preview) return previewResolution(placement, label, slot, environment);
  if (environment.DISPLAY_ADS_ENABLED !== "true") return { state: "empty" };
  const raw = environment.DISPLAY_AD_SLOTS_JSON;
  if (!raw) return { state: "empty" };

  let entries: unknown;
  try {
    entries = JSON.parse(raw);
  } catch {
    return { state: "error" };
  }
  if (!Array.isArray(entries) || entries.length > 20) return { state: "error" };
  const matches = entries.filter(isObject).filter((entry) => entry.slot === slot);
  if (matches.length === 0) return { state: "empty" };
  if (matches.length !== 1) return { state: "error" };

  return resolveAdCreative(matches[0], placement, environment, now, slot);
}

export function resolveAdCreative(value: unknown, placement: AdPlacement, environment: Environment = process.env, now = new Date(), expectedSlot = ""): AdSlotResolution {
  if (!isObject(value)) return { state: "error" };
  const creative = validateCreative(value, placement, environment, now);
  if (!creative || (expectedSlot && creative.slot !== expectedSlot)) return { state: "error" };
  return { state: "ready", creative };
}

function previewResolution(placement: AdPlacement, label: string, slot: string, environment: Environment): AdSlotResolution {
  if (environment.NODE_ENV === "production" || environment.DISPLAY_AD_PREVIEW_FIXTURE !== "enabled") return { state: "error" };
  const size = dimensions[placement];
  return {
    state: "preview",
    creative: {
      slot,
      placement,
      title: label,
      sponsor: "设计预览",
      imageUrl: "/ad-preview.svg",
      imageAlt: "",
      targetUrl: null,
      width: size.width,
      height: size.height,
      deviceScope: "all",
    },
  };
}

function validateCreative(value: Record<string, unknown>, placement: AdPlacement, environment: Environment, now: Date): AdCreative | null {
  const size = dimensions[placement];
  if (Object.keys(value).some((key) => !creativeKeys.has(key))) return null;
  if (value.placement !== placement || value.width !== size.width || value.height !== size.height) return null;
  if (![value.slot, value.title, value.sponsor, value.image_url, value.image_alt, value.starts_at, value.ends_at].every((item) => typeof item === "string")) return null;
  if (value.target_url !== null && typeof value.target_url !== "string") return null;
  if (value.device_scope !== undefined && !["all", "desktop", "mobile"].includes(String(value.device_scope))) return null;
  const strings = value as Record<string, string>;
  if (!safeText.test(strings.slot) || !safeText.test(strings.title) || !safeText.test(strings.sponsor) || !safeAltText.test(strings.image_alt)) return null;
  if (strings.slot.length > 200) return null;
  const targetURL = value.target_url as string | null;
  if (!validAssetURL(strings.image_url, environment) || (targetURL !== null && !validTargetURL(targetURL, environment))) return null;
  if (!rfc3339Pattern.test(strings.starts_at) || !rfc3339Pattern.test(strings.ends_at)) return null;
  const startsAt = Date.parse(strings.starts_at);
  const endsAt = Date.parse(strings.ends_at);
  const instant = now.getTime();
  if (!Number.isFinite(startsAt) || !Number.isFinite(endsAt) || endsAt <= startsAt || instant < startsAt || instant >= endsAt) return null;
  return {
    slot: strings.slot,
    placement,
    title: strings.title,
    sponsor: strings.sponsor,
    imageUrl: strings.image_url,
    imageAlt: strings.image_alt,
    targetUrl: targetURL,
    width: size.width,
    height: size.height,
    deviceScope: (value.device_scope ?? "all") as AdCreative["deviceScope"],
  };
}

function validAssetURL(value: string, environment: Environment): boolean {
  if (/^\/ads\/[a-zA-Z0-9][a-zA-Z0-9/_.-]{0,300}$/.test(value) && !value.includes("..")) return true;
  return allowedHTTPSURL(value, environment.DISPLAY_AD_ASSET_HOSTS);
}

function validTargetURL(value: string, environment: Environment): boolean {
  return allowedHTTPSURL(value, environment.DISPLAY_AD_TARGET_HOSTS);
}

function allowedHTTPSURL(value: string, hostList: string | undefined): boolean {
  const allowed = new Set((hostList ?? "").split(",").map((host) => host.trim().toLowerCase()).filter(Boolean));
  if (allowed.size === 0) return false;
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password && !url.hash && allowed.has(url.hostname.toLowerCase());
  } catch {
    return false;
  }
}

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}
