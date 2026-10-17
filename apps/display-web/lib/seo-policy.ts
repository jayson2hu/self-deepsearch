import type { Metadata } from "next";
type Environment = Record<string, string | undefined>;

export type PublicSEOProfile = {
  indexable: boolean;
  sitemap_eligible: boolean;
  adult_rating: "adult" | null;
  image_preview_mode: "standard" | "none";
};

const SAFE_DEFAULT_PROFILE: PublicSEOProfile = {
  indexable: false,
  sitemap_eligible: false,
  adult_rating: null,
  image_preview_mode: "none",
};

export function resolvePublicSEOProfile(value: unknown): PublicSEOProfile {
  if (!value || typeof value !== "object") return SAFE_DEFAULT_PROFILE;
  const profile = value as Partial<PublicSEOProfile>;
  if (typeof profile.indexable !== "boolean" || typeof profile.sitemap_eligible !== "boolean" ||
      ![null, "adult"].includes(profile.adult_rating ?? null) || !["standard", "none"].includes(profile.image_preview_mode ?? "")) {
    return SAFE_DEFAULT_PROFILE;
  }
  return {
    indexable: profile.indexable,
    sitemap_eligible: profile.indexable && profile.sitemap_eligible,
    adult_rating: profile.adult_rating ?? null,
    image_preview_mode: profile.indexable && profile.adult_rating !== "adult" ? profile.image_preview_mode as "standard" | "none" : "none",
  };
}

export function seoReleaseEnabled(environment: Environment = process.env): boolean {
  if (environment.APP_ENV?.trim().toLowerCase() !== "production") return false;
  if (environment.SEO_INDEXING_ENABLED?.trim().toLowerCase() !== "true") return false;
  if (environment.SEO_CONTENT_POLICY_READY?.trim().toLowerCase() !== "true") return false;
  try {
    const value = environment.SITE_URL?.trim();
    if (!value) return false;
    const origin = new URL(value);
    const originOnly = origin.pathname === "/" && !origin.search && !origin.hash && !origin.username && !origin.password;
    // Index only a DNS origin, never a staging IP, IPv6 literal or internal name.
    // URL normalizes alternative IPv4 spellings (127.1, integer and hex forms).
    const host = origin.hostname.toLowerCase();
    const dnsName = host.length <= 253 && host.includes(".") && host.split(".").every(
      (label) => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label),
    );
    const internal = /(?:^|\.)(?:localhost|local|internal|test|invalid|example)$/.test(host) || /(?:^|\.)home\.arpa$/.test(host);
    return originOnly && origin.protocol === "https:" && dnsName && !internal && !/^[\d.]+$/.test(host);
  } catch {
    return false;
  }
}

export function externalImagePreviewAllowed(profile: PublicSEOProfile): boolean {
  return seoReleaseEnabled() && profile.indexable && profile.adult_rating !== "adult" && profile.image_preview_mode === "standard";
}

export function pageRobots(indexable: boolean, imagePreview: "standard" | "none" = "standard"): Metadata["robots"] {
  if (!seoReleaseEnabled() || !indexable) {
    return { index: false, follow: false, noarchive: true, googleBot: { index: false, follow: false, noimageindex: true } };
  }
  return {
    index: true,
    follow: true,
    googleBot: imagePreview === "standard"
      ? { index: true, follow: true, "max-image-preview": "standard" }
      : { index: true, follow: true, noimageindex: true, "max-image-preview": "none" },
  };
}

export function canonicalWithCursor(pathname: string, cursor = ""): string {
  if (!cursor) return pathname;
  const parameters = new URLSearchParams({ cursor });
  return `${pathname}?${parameters.toString()}`;
}

export function alwaysNoIndexPath(pathname: string): boolean {
  return pathname === "/search" || pathname === "/account" || pathname.startsWith("/account/") || ["/login", "/logout", "/register", "/forgot-password", "/invite"].includes(pathname);
}
