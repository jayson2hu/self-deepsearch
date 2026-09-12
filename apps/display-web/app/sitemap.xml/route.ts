import { NextResponse } from "next/server";
import { siteURL } from "../../lib/site";

export const dynamic = "force-dynamic";

export function GET() {
  const base = siteURL();
  const entries = ["pages", "works", "performers", "studios"]
    .map((name) => `<sitemap><loc>${new URL(`/sitemaps/${name}`, base).toString()}</loc></sitemap>`)
    .join("");
  return new NextResponse(`<?xml version="1.0" encoding="UTF-8"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${entries}</sitemapindex>`, {
    headers: { "Content-Type": "application/xml; charset=utf-8", "Cache-Control": "public, s-maxage=300, stale-while-revalidate=60" },
  });
}
