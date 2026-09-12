import { cookies } from "next/headers";
import { NextResponse } from "next/server";

function requestOrigin(request: Request): string | null {
  // Next normalizes loopback URLs to localhost. Use the actual Host, not a
  // caller-controlled forwarded host, with the ingress-provided scheme.
  const host = request.headers.get("host");
  if (!host) return null;
  try {
    const candidate = new URL(`${new URL(request.url).protocol}//${host}`);
    if (!["http:", "https:"].includes(candidate.protocol) || candidate.host !== host.toLowerCase()) return null;
    return candidate.origin;
  } catch {
    return null;
  }
}

function privateRedirect(origin: string, path: string) {
  const response = NextResponse.redirect(new URL(path, origin), 303);
  response.headers.set("cache-control", "private, no-store");
  return response;
}

export async function POST(request: Request) {
  const origin = requestOrigin(request);
  // Native forms under no-referrer can carry Origin: null. Only accept that
  // case when browser-forbidden Fetch Metadata confirms same-origin navigation.
  const opaqueSameOriginNavigation = request.headers.get("origin") === "null"
    && request.headers.get("sec-fetch-site") === "same-origin"
    && request.headers.get("sec-fetch-mode") === "navigate";
  if (!origin || (request.headers.get("origin") !== origin && !opaqueSameOriginNavigation)) {
    return NextResponse.json({ error: { code: "ORIGIN_REJECTED", message: "请从本站发起退出操作" } }, {
      status: 403, headers: { "cache-control": "private, no-store" },
    });
  }
  const token = (await cookies()).get("sd_session")?.value;
  const base = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  if (token) {
    try {
      const upstream = await fetch(`${base}/api/v1/auth/logout`, {
        method: "POST", headers: { Cookie: `sd_session=${token}`, Origin: origin },
        cache: "no-store", redirect: "error", signal: AbortSignal.timeout(3000),
      });
      void upstream.body?.cancel().catch(() => undefined);
      if (upstream.status !== 204) return privateRedirect(origin, "/logout?error=unconfirmed");
    } catch {
      return privateRedirect(origin, "/logout?error=unconfirmed");
    }
  }
  const response = privateRedirect(origin, "/login");
  response.cookies.set("sd_session", "", { path: "/", maxAge: 0, httpOnly: true, sameSite: "lax" });
  response.cookies.set("sd_reauth", "", { path: "/", maxAge: 0, httpOnly: true, sameSite: "lax" });
  return response;
}
