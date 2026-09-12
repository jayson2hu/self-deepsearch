import type { RedirectTargetResponse } from "@self-deepsearch/api-contracts";
import { NextRequest, NextResponse } from "next/server";
import { mediaContentSecurityPolicy } from "./lib/media-policy";
import { readMediaRuntimePolicy } from "./lib/media-runtime";
import { createRequestNonce, isSensitivePublicPage } from "./lib/security-policy";

export async function proxy(request: NextRequest) {
  const mediaPolicy = await readMediaRuntimePolicy();
  const sensitivePage = isSensitivePublicPage(request.nextUrl.pathname);
  let response: NextResponse;

  if (sensitivePage) {
    const nonce = createRequestNonce();
    const policy = mediaContentSecurityPolicy(mediaPolicy.defaultOnly, nonce);
    const requestHeaders = new Headers(request.headers);
    // Next.js reads the request CSP and attaches the nonce to bootstrap, RSC,
    // and next/script output. Only account surfaces pay the dynamic-render cost;
    // public catalog ISR remains cacheable under the separate static policy.
    requestHeaders.set("Content-Security-Policy", policy);
    requestHeaders.set("x-nonce", nonce);
    response = NextResponse.next({ request: { headers: requestHeaders } });
    response.headers.set("Content-Security-Policy", policy);
    response.headers.set("Cache-Control", "private, no-store");
  } else {
    response = /^\/(works|performers|studios)\/[^/]+$/.test(request.nextUrl.pathname)
      ? await catalogRedirect(request)
      : NextResponse.next();
  }

  if (mediaPolicy.defaultOnly) {
    // Applies even when the Next page body comes from an older ISR artifact.
    // Keep the complete security policy; replacing it with img-src alone would
    // accidentally discard the existing script/frame/form protections.
    if (!sensitivePage) response.headers.set("Content-Security-Policy", mediaContentSecurityPolicy(true));
    response.headers.set("Cache-Control", "no-store");
    response.headers.set("X-Media-Delivery-Mode", "default_only");
  } else {
    response.headers.set("X-Media-Delivery-Mode", "normal");
  }
  return response;
}

async function catalogRedirect(request: NextRequest) {
  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  const lookupURL = new URL("/api/v1/site/redirect", baseURL);
  lookupURL.searchParams.set("path", request.nextUrl.pathname);
  try {
    const response = await fetch(lookupURL, { headers: { Accept: "application/json" }, cache: "no-store", signal: AbortSignal.timeout(1500) });
    if (response.status !== 200) return NextResponse.next();
    const target = (await response.json()) as RedirectTargetResponse;
    if (target.status_code !== 301 || !/^\/(works|performers|studios)\/[a-z0-9]+(?:-[a-z0-9]+)*$/.test(target.target_path)) {
      return NextResponse.next();
    }
    return NextResponse.redirect(new URL(target.target_path, request.url), 301);
  } catch {
    return NextResponse.next();
  }
}

export const config = { matcher: ["/((?!api/|_next/static/|_next/image|default-work.svg|default-performer.svg|favicon.ico).*)"] };
