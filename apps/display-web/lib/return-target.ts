const MAX_RETURN_TARGET_LENGTH = 2048;
const forbiddenCharacterPattern = /[\\\u0000-\u001f\u007f]/;
const malformedOrStructuralEscapePattern = /%(?![0-9a-f]{2})|%(?:0[0-9a-f]|1[0-9a-f]|2[35ef]|3[af]|5c|7f)/i;
const safeSlugPattern = /^[a-zA-Z0-9][a-zA-Z0-9._~-]{0,199}$/;

const exactPaths = new Set([
  "/",
  "/latest",
  "/performers",
  "/studios",
  "/account",
  "/account/favorites",
  "/account/follows",
  "/account/hidden",
  "/account/history",
  "/account/feedback",
  "/account/settings",
]);

const detailPathPattern = /^\/(?:works|performers|studios)\/([^/]+)$/;
const searchQueryKeys = new Set(["q", "type", "cursor"]);

export function sanitizeReturnTo(value: unknown): string | null {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_RETURN_TARGET_LENGTH) return null;
  if (!value.startsWith("/") || value.startsWith("//") || forbiddenCharacterPattern.test(value) || malformedOrStructuralEscapePattern.test(value)) return null;

  let parsed: URL;
  try {
    parsed = new URL(value, "https://mujian.invalid");
  } catch {
    return null;
  }
  if (parsed.origin !== "https://mujian.invalid" || parsed.username || parsed.password || parsed.hash) return null;
  if ([...parsed.searchParams.keys()].some((key) => key.toLowerCase() === "returnto")) return null;

  const detailMatch = parsed.pathname.match(detailPathPattern);
  if (detailMatch) {
    if (parsed.search || !safeSlugPattern.test(detailMatch[1])) return null;
    return parsed.pathname;
  }
  if (parsed.pathname === "/search") {
    if ([...parsed.searchParams.keys()].some((key) => !searchQueryKeys.has(key))) return null;
    return `${parsed.pathname}${parsed.search}`;
  }
  if (!exactPaths.has(parsed.pathname) || parsed.search) return null;
  return parsed.pathname;
}

export function returnTargetFromQuery(value: string | string[] | undefined): string | null {
  return sanitizeReturnTo(Array.isArray(value) ? null : value);
}

export function authPath(path: "/login" | "/register" | "/forgot-password", returnTo?: string | null): string {
  const safeTarget = sanitizeReturnTo(returnTo);
  return safeTarget ? `${path}?returnTo=${encodeURIComponent(safeTarget)}` : path;
}
