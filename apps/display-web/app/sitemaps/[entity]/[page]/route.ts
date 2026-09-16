import { NextResponse } from "next/server";
import { entitySitemapResponse, type SitemapEntity } from "../../../../lib/sitemap-response";

type Context = { params: Promise<{ entity: string; page: string }> };

export const revalidate = 300;

export async function GET(_request: Request, context: Context) {
  const { entity, page } = await context.params;
  if (!(["works", "performers", "studios"] as string[]).includes(entity) || !/^\d+$/.test(page)) {
    return new NextResponse("Not found", { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  return entitySitemapResponse(entity as SitemapEntity, Number(page));
}
