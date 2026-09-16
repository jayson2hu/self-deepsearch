import type { RedirectTargetResponse } from "@self-deepsearch/api-contracts";
import { NextRequest, NextResponse } from "next/server";
import { mediaContentSecurityPolicy } from "./lib/media-policy";
import { readMediaRuntimePolicy } from "./lib/media-runtime";
import { alwaysNoIndexPath, seoReleaseEnabled } from "./lib/seo-policy";
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
    if (/^\/(works|performers|studios)\/[^/]+$/.test(request.nextUrl.pathname)) {
      response = await catalogResponse(request);
    } else {
      response = await publicListingResponse(request);
    }
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
  if (!seoReleaseEnabled()) {
    response.headers.set("X-Robots-Tag", "noindex, nofollow, noarchive");
  } else if (alwaysNoIndexPath(request.nextUrl.pathname)) {
    response.headers.set("X-Robots-Tag", "noindex, nofollow, noarchive");
  }
  return response;
}

async function catalogResponse(request: NextRequest) {
  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  const lookupURL = new URL("/api/v1/site/redirect", baseURL);
  lookupURL.searchParams.set("path", request.nextUrl.pathname);
  try {
    const response = await fetch(lookupURL, { headers: { Accept: "application/json" }, cache: "no-store", signal: AbortSignal.timeout(1500) });
    if (response.status === 200) {
      const target = (await response.json()) as RedirectTargetResponse;
      if (target.status_code === 301 && /^\/(works|performers|studios)\/[a-z0-9]+(?:-[a-z0-9]+)*$/.test(target.target_path)) {
        return NextResponse.redirect(new URL(target.target_path, request.url), 301);
      }
    }
    if (response.status >= 500) return catalogUnavailable();

    const detailURL = new URL(`/api/v1${request.nextUrl.pathname}`, baseURL);
    const detail = await fetch(detailURL, { headers: { Accept: "application/json" }, cache: "no-store", signal: AbortSignal.timeout(1800) });
    if (detail.status >= 500) return catalogUnavailable();
    if (detail.status === 200 && /^\/(performers|studios)\//.test(request.nextUrl.pathname)) {
      const relatedURL = new URL(`/api/v1${request.nextUrl.pathname}/works`, baseURL);
      const cursor = request.nextUrl.searchParams.get("cursor")?.trim();
      if (cursor) relatedURL.searchParams.set("cursor", cursor);
      const related = await fetch(relatedURL, { headers: { Accept: "application/json" }, cache: "no-store", signal: AbortSignal.timeout(1800) });
      if (related.status === 400) return catalogBadRequest();
      if (related.status >= 500) return catalogUnavailable();
    }
    return NextResponse.next();
  } catch {
    return catalogUnavailable();
  }
}

async function publicListingResponse(request: NextRequest) {
  const endpoint = ({
    "/": "/api/v1/site/home",
    "/latest": "/api/v1/works/latest",
    "/recent": "/api/v1/works/recent",
    "/performers": "/api/v1/performers",
    "/studios": "/api/v1/studios",
  } as Record<string, string>)[request.nextUrl.pathname];
  if (!endpoint) return NextResponse.next();
  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  const target = new URL(endpoint, baseURL);
  const cursor = request.nextUrl.searchParams.get("cursor")?.trim();
  if (cursor && request.nextUrl.pathname !== "/") target.searchParams.set("cursor", cursor);
  try {
    const response = await fetch(target, { headers: { Accept: "application/json" }, cache: "no-store", signal: AbortSignal.timeout(1800) });
    if (response.status === 400) return catalogBadRequest();
    if (response.status >= 500) return catalogUnavailable();
    return NextResponse.next();
  } catch {
    return catalogUnavailable();
  }
}

function catalogBadRequest() {
  return new NextResponse("<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><meta name=\"robots\" content=\"noindex,nofollow\"><title>分页参数无效</title><main><h1>分页参数无效</h1><p>请返回目录首页重新浏览。</p></main></html>", {
    status: 400,
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "X-Robots-Tag": "noindex, nofollow, noarchive" },
  });
}

function catalogUnavailable() {
  return new NextResponse("<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><meta name=\"robots\" content=\"noindex,nofollow\"><title>资料暂时无法加载</title><main><h1>资料暂时无法加载</h1><p>服务暂时不可用，请稍后重试。</p></main></html>", {
    status: 503,
    headers: { "Content-Type": "text/html; charset=utf-8", "Cache-Control": "no-store", "Retry-After": "60", "X-Robots-Tag": "noindex, nofollow, noarchive" },
  });
}

export const config = { matcher: ["/((?!api/|_next/static/|_next/image|default-work.svg|default-performer.svg|favicon.ico).*)"] };
