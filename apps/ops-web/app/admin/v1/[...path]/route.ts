import { NextRequest } from "next/server";
import { proxyPlatform } from "../../../../lib/platform-proxy";

type Context = { params: Promise<{ path: string[] }> };

async function handle(request: NextRequest, context: Context) {
  const path = (await context.params).path;
  const maximumBytes = path.join("/") === "seo/reports/import" ? 5 * 1024 * 1024 + 128 * 1024 : undefined;
  return proxyPlatform(request, "/admin/v1", path, maximumBytes);
}

export const GET = handle;
export const POST = handle;
export const PUT = handle;
export const PATCH = handle;
export const DELETE = handle;
