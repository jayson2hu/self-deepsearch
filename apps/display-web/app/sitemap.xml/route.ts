import { NextResponse } from "next/server";
import { getSitemapEntries } from "../../lib/api";
import { seoReleaseEnabled } from "../../lib/seo-policy";
import { escapeXML, unavailableSitemap } from "../../lib/sitemap-response";
import { sitemapShardPaths } from "../../lib/sitemap-policy";
import { siteURL } from "../../lib/site";

export const dynamic = "force-dynamic";

export async function GET() {
  if (!seoReleaseEnabled()) return unavailableSitemap();
  const base = siteURL();
  const types = ["work", "performer", "studio"] as const;
  const counts = await Promise.all(types.map((type) => getSitemapEntries(type, 1, 1)));
  if (counts.some((result) => result === null)) return new NextResponse("Sitemap source unavailable", { status: 503, headers: { "Cache-Control": "no-store", "Retry-After": "60" } });
  const paths = sitemapShardPaths({
    works: counts[0]?.total_items ?? 0,
    performers: counts[1]?.total_items ?? 0,
    studios: counts[2]?.total_items ?? 0,
  });
  const locations = paths.map((pathname) => new URL(pathname, base).toString());
  const entries = locations.map((location) => `<sitemap><loc>${escapeXML(location)}</loc></sitemap>`).join("");
  return new NextResponse(`<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${entries}</sitemapindex>`, {
    headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "public, s-maxage=300, stale-while-revalidate=60" },
  });
}
