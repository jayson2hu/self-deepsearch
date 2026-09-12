import { NextRequest } from "next/server";
import { proxyPlatform } from "../../../../lib/platform-proxy";

type Context = { params: Promise<{ path: string[] }> };

async function handle(request: NextRequest, context: Context) {
  return proxyPlatform(request, "/admin/v1", (await context.params).path);
}

export const GET = handle;
export const POST = handle;
export const PUT = handle;
export const DELETE = handle;
