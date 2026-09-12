import { NextRequest, NextResponse } from "next/server";
import { createRequestNonce, opsContentSecurityPolicy } from "./lib/security-policy";

export function proxy(request: NextRequest) {
  const nonce = createRequestNonce();
  const policy = opsContentSecurityPolicy(nonce);
  const requestHeaders = new Headers(request.headers);
  // Next.js reads the request CSP to attach this nonce to its bootstrap and
  // RSC scripts. x-nonce is retained for future explicit Script components.
  requestHeaders.set("Content-Security-Policy", policy);
  requestHeaders.set("x-nonce", nonce);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", policy);
  response.headers.set("Cache-Control", "private, no-store");
  return response;
}

export const config = {
  matcher: ["/((?!api/|admin/|auth/|_next/static/|_next/image|favicon.ico).*)"],
};
