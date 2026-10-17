import { NextResponse } from "next/server";
import { getSitemapEntries } from "../../lib/api";
import { seoReleaseEnabled } from "../../lib/seo-policy";
import { escapeXML, sitemapSourceUnavailable, unavailableSitemap } from "../../lib/sitemap-response";
import { sitemapShardPaths, sitemapWithinLimits } from "../../lib/sitemap-policy";
import { siteURL } from "../../lib/site";

export const dynamic = "force-dynamic";

export async function GET() {
  if (!seoReleaseEnabled()) return unavailableSitemap();
  const base = siteURL();
  const types = ["work", "performer", "studio"] as const;
  const counts = await Promise.all(types.map((type) => getSitemapEntries(type, 1, 1)));
  if (counts.some((result) => result === null)) return sitemapSourceUnavailable();
  let paths: string[];
  try {
    paths = sitemapShardPaths({
      works: counts[0]?.total_items ?? 0,
      performers: counts[1]?.total_items ?? 0,
      studios: counts[2]?.total_items ?? 0,
    });
  } catch {
    return sitemapSourceUnavailable("Invalid sitemap source counts");
  }
  const locations = paths.map((pathname) => new URL(pathname, base).toString());
  const entries = locations.map((location) => `<sitemap><loc>${escapeXML(location)}</loc></sitemap>`).join("");
  const xml = `<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${entries}</sitemapindex>`;
  if (!sitemapWithinLimits(paths.length, xml)) return sitemapSourceUnavailable("Sitemap index exceeds limits");
  return new NextResponse(xml, {
    headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "public, s-maxage=300, stale-while-revalidate=60" },
  });
}
