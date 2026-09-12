/** Runtime emergency delivery mode; not an account-usage meter. */
export function mediaDefaultOnly(mode = process.env.MEDIA_DELIVERY_MODE): boolean {
  return mode !== undefined && mode !== "normal";
}

// Only call on public/account catalog DTOs, never operator evidence/manifests.
// Copy instead of mutating Next's shared cached response objects.
export function mediaProjection<T>(value: T, explicitDefaultOnly = false): T {
	if (!mediaDefaultOnly() && !explicitDefaultOnly) return value;
	return stripMedia(value) as T;
}

function stripMedia(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stripMedia);
  if (value === null || typeof value !== "object") return value;
  return Object.fromEntries(Object.entries(value).map(([key, child]) => {
    if (key === "image" || key === "image_url") return [key, null];
    if (key === "images") return [key, []];
    return [key, stripMedia(child)];
  }));
}

const noncePattern = /^[A-Za-z0-9_-]{22,128}$/;

export function mediaContentSecurityPolicy(defaultOnly = false, nonce = ""): string {
  if (nonce && !noncePattern.test(nonce)) throw new Error("invalid CSP nonce");
  const scriptSource = nonce
    ? `script-src 'self' 'nonce-${nonce}' 'strict-dynamic' https://challenges.cloudflare.com`
    : "script-src 'self' 'unsafe-inline' https://challenges.cloudflare.com";
  return [
    "default-src 'self'", "base-uri 'self'", "object-src 'none'", "frame-ancestors 'none'", "form-action 'self'",
    scriptSource, "script-src-attr 'none'", "style-src 'self' 'unsafe-inline'",
    defaultOnly ? "img-src 'self' data: blob:" : "img-src 'self' data: blob: https:",
    "font-src 'self' data:", "connect-src 'self' https://challenges.cloudflare.com",
    "frame-src https://challenges.cloudflare.com", "worker-src 'self' blob:",
  ].join("; ");
}
