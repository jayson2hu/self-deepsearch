const fallbackSiteURL = "http://127.0.0.1:3000";

export function siteURL(): URL {
  const production = process.env.NODE_ENV === "production";
  const value = process.env.SITE_URL ?? (production ? undefined : process.env.NEXT_PUBLIC_SITE_URL);
  if (!value) {
    if (production) throw new Error("SITE_URL must be configured as an HTTP(S) origin in production");
    return new URL(fallbackSiteURL);
  }
  try {
    const parsed = new URL(value);
    const originOnly = parsed.pathname === "/" && !parsed.search && !parsed.hash && !parsed.username && !parsed.password;
    const loopback = parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1" || parsed.hostname === "[::1]";
    if (!originOnly || !["http:", "https:"].includes(parsed.protocol) || (production && parsed.protocol !== "https:" && !loopback)) {
      throw new Error("invalid site origin");
    }
    return new URL(parsed.origin);
  } catch {
    if (production) throw new Error("SITE_URL must be configured as an HTTP(S) origin in production");
    return new URL(fallbackSiteURL);
  }
}
