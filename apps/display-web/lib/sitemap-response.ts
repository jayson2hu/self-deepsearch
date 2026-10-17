import { NextResponse } from "next/server";
import { getHome, getLatestWorks, getPerformers, getRecentlyAddedWorks, getSitemapEntries, getStudios } from "./api";
import { seoReleaseEnabled } from "./seo-policy";
import { siteURL } from "./site";
import { sitemapEntityPath, sitemapPageSize, sitemapWithinLimits, type SitemapEntity } from "./sitemap-policy";

export { sitemapPageSize, type SitemapEntity } from "./sitemap-policy";

export async function entitySitemapResponse(entity: SitemapEntity, page: number) {
  if (!seoReleaseEnabled()) return unavailableSitemap();
  if (!Number.isSafeInteger(page) || page < 1) return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store" } });
  const entityType = ({ works: "work", performers: "performer", studios: "studio" } as const)[entity];
  const response = await getSitemapEntries(entityType, page, sitemapPageSize);
  if (!response) return sitemapSourceUnavailable();
  if (response.page !== page || response.page_size !== sitemapPageSize || page > response.total_pages) {
    return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  const base = siteURL();
  try {
    return sitemapResponse(response.items.map((entry) => ({
      location: new URL(sitemapEntityPath(entity, entry.slug), base).toString(),
      lastModified: entry.updated_at,
    })));
  } catch {
    // An upstream path must never inject queries, fragments or another entity.
    return sitemapSourceUnavailable();
  }
}

export async function fixedPagesSitemapResponse() {
  if (!seoReleaseEnabled()) return unavailableSitemap();
  const base = siteURL();
  const [home, latest, recent, performers, studios] = await Promise.all([
    getHome(), getLatestWorks(), getRecentlyAddedWorks(), getPerformers(), getStudios(),
  ]);
  if (home.degraded || latest.degraded || recent.degraded || performers.degraded || studios.degraded) {
    return sitemapSourceUnavailable();
  }
  const paths = ["/about"];
  if (!home.degraded && home.data.sections.find((section) => section.key === "latest")?.items.length) paths.push("/");
  if (!latest.degraded && latest.data?.items.length) paths.push("/latest");
  if (!recent.degraded && recent.data?.items.length) paths.push("/recent");
  if (!performers.degraded && performers.data?.items.length) paths.push("/performers");
  if (!studios.degraded && studios.data?.items.length) paths.push("/studios");
  return sitemapResponse(paths.map((pathname) => ({
    location: new URL(pathname, base).toString(), lastModified: null,
  })));
}

export function sitemapResponse(entries: Array<{ location: string; lastModified: string | null }>) {
  const urls = entries.map((entry) => `<url><loc>${escapeXML(entry.location)}</loc>${entry.lastModified ? `<lastmod>${escapeXML(entry.lastModified)}</lastmod>` : ""}</url>`).join("");
  const xml = `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls}</urlset>`;
  if (!sitemapWithinLimits(entries.length, xml)) {
    return new NextResponse("Sitemap exceeds URL or uncompressed size limit", { status: 503, headers: { "Cache-Control": "no-store" } });
  }
  return new NextResponse(xml, { headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "public, s-maxage=300, stale-while-revalidate=60" } });
}

export function unavailableSitemap() {
  return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store", "X-Robots-Tag": "noindex, nofollow" } });
}

export function sitemapSourceUnavailable(message = "Sitemap source unavailable") {
  return new NextResponse(message, { status: 503, headers: { "Cache-Control": "no-store", "Retry-After": "60" } });
}

export function escapeXML(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&apos;");
}
