import { NextResponse } from "next/server";
import { fixedPagesSitemapResponse, type SitemapEntity } from "../../../lib/sitemap-response";
import { siteURL } from "../../../lib/site";

type Context = { params: Promise<{ entity: string }> };

export const revalidate = 300;

export async function GET(_request: Request, context: Context) {
  const entity = (await context.params).entity;
  if (entity === "pages") return await fixedPagesSitemapResponse();
  if (!(["works", "performers", "studios"] as string[]).includes(entity)) return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store" } });
  return NextResponse.redirect(new URL(`/sitemaps/${entity as SitemapEntity}/1`, siteURL()), 301);
}
