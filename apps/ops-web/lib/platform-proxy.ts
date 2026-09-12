import { NextRequest, NextResponse } from "next/server";

const maximumBodyBytes = 2 * 1024 * 1024;
const requestHeaders = ["cf-connecting-ip", "content-type", "cookie", "idempotency-key", "origin", "x-request-id"];
const responseHeaders = ["cache-control", "content-disposition", "content-type", "retry-after", "x-request-id"];

function proxyError(code: string, message: string, status: number): NextResponse {
  const response = NextResponse.json({ error: { code, message } }, { status });
  response.headers.set("cache-control", "no-store");
  return response;
}

export async function proxyPlatform(request: NextRequest, prefix: string, segments: string[]): Promise<NextResponse> {
  const base = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  const path = segments.map(encodeURIComponent).join("/");
  const url = new URL(`${prefix}/${path}${request.nextUrl.search}`, base);
  const headers = new Headers();
  for (const name of requestHeaders) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }
  if (!headers.has("origin")) headers.set("origin", request.nextUrl.origin);

  const declared = Number(request.headers.get("content-length") ?? "0");
  if (!Number.isFinite(declared) || declared < 0 || declared > maximumBodyBytes) {
    return proxyError("BODY_TOO_LARGE", "请求内容过大", 413);
  }
  const body = request.method === "GET" || request.method === "HEAD" ? undefined : await request.arrayBuffer();
  if (body && body.byteLength > maximumBodyBytes) {
    return proxyError("BODY_TOO_LARGE", "请求内容过大", 413);
  }

  try {
    const upstream = await fetch(url, {
      method: request.method,
      headers,
      body,
      cache: "no-store",
      redirect: "manual",
      signal: AbortSignal.timeout(15_000),
    });
    const outgoing = new Headers();
    for (const name of responseHeaders) {
      const value = upstream.headers.get(name);
      if (value) outgoing.set(name, value);
    }
    const setCookies = upstream.headers.getSetCookie();
    for (const value of setCookies) outgoing.append("set-cookie", value);
    outgoing.set("cache-control", "no-store");
    return new NextResponse(upstream.body, { status: upstream.status, headers: outgoing });
  } catch {
    return proxyError("API_UNAVAILABLE", "服务暂时不可用", 503);
  }
}
