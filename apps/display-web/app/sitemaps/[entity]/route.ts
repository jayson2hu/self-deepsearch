import { NextResponse } from "next/server";
import { getSitemapEntries } from "../../../lib/api";
import { siteURL } from "../../../lib/site";

type Context = { params: Promise<{ entity: string }> };

export const revalidate = 300;

export async function GET(_request: Request, context: Context) {
  const entity = (await context.params).entity;
  if (entity === "pages") return sitemapResponse(fixedPages());
  const entityType = ({ works: "work", performers: "performer", studios: "studio" } as const)[entity as "works" | "performers" | "studios"];
  if (!entityType) return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store" } });

  const response = await getSitemapEntries(entityType);
  if (!response) return new NextResponse("Sitemap source unavailable", { status: 503, headers: { "Cache-Control": "no-store" } });
  const path = entityType === "work" ? "works" : entityType === "performer" ? "performers" : "studios";
  return sitemapResponse(response.items.map((entry) => ({
    location: new URL(`/${path}/${entry.slug}`, siteURL()).toString(),
    lastModified: entry.updated_at,
  })));
}

function fixedPages() {
  const base = siteURL();
  return ["/", "/latest", "/recent", "/performers", "/studios"].map((path) => ({
    location: new URL(path, base).toString(), lastModified: null,
  }));
}

function sitemapResponse(entries: Array<{ location: string; lastModified: string | null }>) {
  const urls = entries.map((entry) => `<url><loc>${escapeXML(entry.location)}</loc>${entry.lastModified ? `<lastmod>${escapeXML(entry.lastModified)}</lastmod>` : ""}</url>`).join("");
  return new NextResponse(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${urls}</urlset>`, {
    headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "public, s-maxage=300, stale-while-revalidate=60" },
  });
}

function escapeXML(value: string) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&apos;");
}
