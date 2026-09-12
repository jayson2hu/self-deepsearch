const MAX_OBJECT_BYTES = 10 * 1024 * 1024;
const MAX_POLICY_BODY_BYTES = 64;
const DEFAULT_POLICY_TTL_SECONDS = 5;
const MAX_POLICY_TTL_SECONDS = 30;
const UUID = "[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}";
const PUBLIC_KEY = new RegExp(`^media-public/[A-Za-z0-9_-]{1,100}/(works|performers)/(${UUID})/(${UUID})/v[1-9][0-9]{0,5}/(cover|gallery|avatar)-w(320|640|960)\\.webp$`, "i");

const workPlaceholder = placeholderSVG("作品图片暂不可用");
const performerPlaceholder = placeholderSVG("人物图片暂不可用");

export function createMediaGateway(dependencies = {}) {
  return {
    async fetch(request, env, ctx = {}) {
      let configuration;
      try {
        configuration = validateConfiguration(env);
      } catch {
        return serviceUnavailable();
      }

      let url;
      try {
        url = new URL(request.url);
      } catch {
        return notFound();
      }
      if (url.origin !== configuration.publicOrigin || url.search || url.hash) return notFound();
      if (request.method !== "GET" && request.method !== "HEAD") {
        return plainResponse(405, "method_not_allowed", { Allow: "GET, HEAD" });
      }

      const key = publicObjectKey(url.pathname);
      if (!key) return notFound();

      const cache = dependencies.cache ?? globalThis.caches?.default;
      const fetcher = dependencies.fetcher ?? globalThis.fetch;
      const policy = await readPolicy(configuration, cache, fetcher);
      if (policy.mode !== "normal") {
        return defaultImage(request.method, key, policy.available, "policy");
      }

      const objectCacheKey = new Request(`${configuration.publicOrigin}/${key}`, { method: "GET" });
      if (cache) {
        try {
          const cached = await cache.match(objectCacheKey);
          if (cached) return request.method === "HEAD" ? headFrom(cached) : cached;
        } catch {
          // Cache availability must not make the source object unavailable.
        }
      }

      let object;
      try {
        object = await configuration.bucket.get(key);
      } catch {
        return defaultImage(request.method, key, true, "origin_unavailable");
      }
      if (!object) return defaultImage(request.method, key, true, "missing");
      if (typeof object.size === "number" && (!Number.isSafeInteger(object.size) || object.size < 1 || object.size > MAX_OBJECT_BYTES)) {
        return defaultImage(request.method, key, true, "invalid_object");
      }

      const headers = new Headers();
      if (typeof object.writeHttpMetadata === "function") object.writeHttpMetadata(headers);
      headers.set("Content-Type", "image/webp");
      headers.set("Cache-Control", "public, max-age=31536000, immutable");
      headers.set("Cloudflare-CDN-Cache-Control", "public, max-age=31536000, immutable");
      headers.set("X-Content-Type-Options", "nosniff");
      headers.set("Cross-Origin-Resource-Policy", "cross-origin");
      headers.set("Access-Control-Allow-Origin", "*");
      headers.set("X-Media-Delivery-Mode", "normal");
      headers.set("X-Media-Policy-Available", "1");
      if (typeof object.httpEtag === "string" && object.httpEtag) headers.set("ETag", object.httpEtag);
      if (typeof object.size === "number") headers.set("Content-Length", String(object.size));

      if (request.method === "HEAD") return new Response(null, { status: 200, headers });
      if (!object.body) return defaultImage(request.method, key, true, "invalid_object");
      const response = new Response(object.body, { status: 200, headers });
      if (cache) schedule(ctx, Promise.resolve(cache.put(objectCacheKey, response.clone())).catch(() => undefined));
      return response;
    },
  };
}

async function readPolicy(configuration, cache, fetcher) {
  const cacheKey = policyCacheRequest(configuration.publicOrigin);
  if (cache) {
    try {
      const cached = await cache.match(cacheKey);
      if (cached) {
        const mode = await cached.text();
        if (mode === "normal" || mode === "default_only") {
          return { mode, available: cached.headers.get("X-Media-Policy-Available") === "1" };
        }
      }
    } catch {
      // Continue to the authoritative policy endpoint.
    }
  }

  let result = { mode: "default_only", available: false };
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 1000);
  try {
    if (typeof fetcher !== "function") throw new Error("fetch unavailable");
    const response = await fetcher(configuration.policyURL, {
      method: "GET",
      headers: { Accept: "application/json", Authorization: `Bearer ${configuration.policyToken}` },
      redirect: "error",
      signal: controller.signal,
    });
    const declaredLength = response.headers.get("Content-Length");
    if (declaredLength !== null && (!/^\d+$/.test(declaredLength) || Number(declaredLength) > MAX_POLICY_BODY_BYTES)) {
      throw new Error("invalid policy length");
    }
    const body = await readLimitedBody(response.body, MAX_POLICY_BODY_BYTES);
    const contentType = response.headers.get("Content-Type") ?? "";
    const cacheControl = response.headers.get("Cache-Control") ?? "";
    const headerMode = response.headers.get("X-Media-Delivery-Mode") ?? "";
    if (response.status !== 200 || !contentType.toLowerCase().startsWith("application/json") || !/(^|,)\s*no-store\s*(,|$)/i.test(cacheControl)) {
      throw new Error("invalid policy response");
    }
    const decoded = new TextDecoder("utf-8", { fatal: true }).decode(body);
    const value = JSON.parse(decoded);
    if (!value || typeof value !== "object" || Array.isArray(value) || Object.keys(value).length !== 1 ||
        (value.mode !== "normal" && value.mode !== "default_only") || value.mode !== headerMode) {
      throw new Error("invalid policy contract");
    }
    result = { mode: value.mode, available: true };
  } catch {
    result = { mode: "default_only", available: false };
  } finally {
    clearTimeout(timeout);
  }

  if (cache) {
    const cached = new Response(result.mode, {
      headers: {
        "Cache-Control": `public, max-age=${configuration.policyTTL}`,
        "X-Media-Policy-Available": result.available ? "1" : "0",
      },
    });
    try {
      await cache.put(cacheKey, cached);
    } catch {
      // The policy decision remains usable for the current request.
    }
  }
  return result;
}

function validateConfiguration(env) {
  if (!env || env.MEDIA_EDGE_GATEWAY_MODE !== "enforce") throw new Error("gateway disabled");
  const publicOrigin = strictHTTPSOrigin(env.MEDIA_PUBLIC_ORIGIN);
  const policyRaw = String(env.MEDIA_POLICY_URL ?? "");
  if (policyRaw.trim() !== policyRaw) throw new Error("invalid policy URL");
  const policy = new URL(policyRaw);
  if (policy.protocol !== "https:" || policy.username || policy.password || policy.search || policy.hash ||
      policy.pathname !== "/edge/v1/media-delivery-policy" || policy.href !== policyRaw || policy.origin === publicOrigin) {
    throw new Error("invalid policy URL");
  }
  const rawPolicyToken = String(env.MEDIA_EDGE_POLICY_TOKEN ?? "");
  const policyToken = rawPolicyToken.trim();
  if (rawPolicyToken !== policyToken) throw new Error("invalid policy token");
  if (policyToken.length < 32 || policyToken.length > 4096) throw new Error("invalid policy token");
  const policyTTL = env.MEDIA_POLICY_CACHE_TTL_SECONDS === undefined || env.MEDIA_POLICY_CACHE_TTL_SECONDS === ""
    ? DEFAULT_POLICY_TTL_SECONDS : Number(env.MEDIA_POLICY_CACHE_TTL_SECONDS);
  if (!Number.isInteger(policyTTL) || policyTTL < 1 || policyTTL > MAX_POLICY_TTL_SECONDS) throw new Error("invalid policy TTL");
  if (!env.MEDIA_BUCKET || typeof env.MEDIA_BUCKET.get !== "function") throw new Error("missing R2 binding");
  return { publicOrigin, policyURL: policy.href, policyToken, policyTTL, bucket: env.MEDIA_BUCKET };
}

function strictHTTPSOrigin(value) {
  const raw = String(value ?? "").trim();
  const parsed = new URL(raw);
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/" || parsed.origin !== raw) {
    throw new Error("invalid public origin");
  }
  return parsed.origin;
}

function publicObjectKey(pathname) {
  let decoded;
  try {
    decoded = decodeURIComponent(pathname);
  } catch {
    return null;
  }
  if (!decoded.startsWith("/") || decoded.includes("\\") || decoded.includes("//") || decoded.includes("\0")) return null;
  const key = decoded.slice(1);
  const match = PUBLIC_KEY.exec(key);
  if (!match) return null;
  const collection = match[1].toLowerCase();
  const purpose = match[4].toLowerCase();
  if ((collection === "works" && purpose === "avatar") || (collection === "performers" && purpose !== "avatar")) return null;
  return key;
}

function policyCacheRequest(publicOrigin) {
  const host = new URL(publicOrigin).hostname.toLowerCase();
  return new Request(`https://media-policy-cache.invalid/v1/${encodeURIComponent(host)}`, { method: "GET" });
}

async function readLimitedBody(body, maximum) {
  if (!body) return new Uint8Array();
  const reader = body.getReader();
  const chunks = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > maximum) {
      await reader.cancel();
      throw new Error("policy body too large");
    }
    chunks.push(value);
  }
  const result = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    result.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return result;
}

function defaultImage(method, key, available, reason) {
  const body = key.includes("/performers/") ? performerPlaceholder : workPlaceholder;
  const headers = new Headers({
    "Content-Type": "image/svg+xml; charset=utf-8",
    "Content-Length": String(new TextEncoder().encode(body).byteLength),
    "Cache-Control": "private, no-store",
    "Cloudflare-CDN-Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff",
    "Cross-Origin-Resource-Policy": "cross-origin",
    "Access-Control-Allow-Origin": "*",
    "X-Media-Delivery-Mode": "default_only",
    "X-Media-Policy-Available": available ? "1" : "0",
    "X-Media-Fallback-Reason": reason,
    "Content-Security-Policy": "default-src 'none'; style-src 'unsafe-inline'; sandbox",
  });
  return new Response(method === "HEAD" ? null : body, { status: 200, headers });
}

function placeholderSVG(label) {
  return `<svg xmlns="http://www.w3.org/2000/svg" width="960" height="1350" viewBox="0 0 960 1350" role="img" aria-label="${label}"><rect width="960" height="1350" fill="#171820"/><rect x="48" y="48" width="864" height="1254" rx="32" fill="#232632"/><path d="M300 780l150-180 105 120 90-105 135 165H300z" fill="#444b60"/><circle cx="630" cy="480" r="72" fill="#444b60"/><text x="480" y="940" fill="#b9bfd0" font-size="42" text-anchor="middle" font-family="sans-serif">${label}</text></svg>`;
}

function headFrom(response) {
  return new Response(null, { status: response.status, headers: new Headers(response.headers) });
}

function notFound() {
  return plainResponse(404, "not_found");
}

function serviceUnavailable() {
  return plainResponse(503, "gateway_unavailable");
}

function plainResponse(status, message, extraHeaders = {}) {
  return new Response(message, {
    status,
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "private, no-store",
      "Cloudflare-CDN-Cache-Control": "no-store",
      "X-Content-Type-Options": "nosniff",
      ...extraHeaders,
    },
  });
}

function schedule(ctx, promise) {
  if (ctx && typeof ctx.waitUntil === "function") ctx.waitUntil(promise);
}

const gateway = createMediaGateway();
export default { fetch: (request, env, ctx) => gateway.fetch(request, env, ctx) };
