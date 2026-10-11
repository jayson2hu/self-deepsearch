const requestHeaders = ["cf-connecting-ip", "content-type", "cookie", "idempotency-key", "origin", "x-request-id"];
const responseHeaders = ["content-disposition", "content-type", "retry-after", "x-request-id"];

export type PlatformProxyOptions = {
  baseURL: string;
  prefix: string;
  segments: string[];
  maximumBytes?: number;
  timeoutMilliseconds?: number;
  fetcher?: typeof fetch;
};

class BodyTooLarge extends Error {}

function proxyError(code: string, message: string, status: number): Response {
  return Response.json({ error: { code, message } }, { status, headers: { "cache-control": "no-store" } });
}

// Check every chunk before retaining it; Content-Length is only an early-rejection hint.
// Cancellation is deliberately not awaited: a broken producer must not stall cleanup.
async function readBody(request: Request, maximumBytes: number, signal: AbortSignal): Promise<ArrayBuffer | undefined> {
  if (request.method === "GET" || request.method === "HEAD" || !request.body) return undefined;
  const reader = request.body.getReader();
  // Cancel closes pending reads even if the producer's cancel promise hangs.
  // One listener avoids retaining a Promise.race reaction for every body chunk.
  const onAbort = () => { void reader.cancel(signal.reason).catch(() => {}); };
  signal.addEventListener("abort", onAbort, { once: true });
  let buffer = new Uint8Array(Math.min(maximumBytes, 16 * 1024));
  let length = 0;
  let complete = false;
  try {
    signal.throwIfAborted();
    while (true) {
      const { done, value } = await reader.read();
      signal.throwIfAborted();
      if (done) {
        complete = true;
        break;
      }
      length += value.byteLength;
      if (length > maximumBytes) throw new BodyTooLarge();
      if (length > buffer.byteLength) {
        const expanded = new Uint8Array(Math.min(maximumBytes, Math.max(length, buffer.byteLength * 2)));
        expanded.set(buffer);
        buffer = expanded;
      }
      buffer.set(value, length - value.byteLength);
    }
    return length === buffer.byteLength ? buffer.buffer : buffer.buffer.slice(0, length);
  } finally {
    signal.removeEventListener("abort", onAbort);
    if (!complete) void reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

/** Shared same-origin gateway for both Next applications; no framework dependencies. */
export async function forwardPlatformRequest(request: Request, options: PlatformProxyOptions): Promise<Response> {
  const { baseURL, prefix, segments, maximumBytes = 2 * 1024 * 1024, timeoutMilliseconds = 15_000, fetcher = fetch } = options;
  if (!Number.isSafeInteger(maximumBytes) || maximumBytes < 0) throw new RangeError("Invalid proxy body limit");
  // URL normalizes literal dot segments even after encodeURIComponent; reject them explicitly.
  if (segments.some((segment) => segment === "." || segment === "..")) {
    return proxyError("INVALID_PATH", "请求路径无效", 400);
  }
  const declared = request.headers.get("content-length");
  if (declared !== null && (!/^\d+$/.test(declared) || !Number.isSafeInteger(Number(declared)) || Number(declared) > maximumBytes)) {
    if (request.body) void request.body.cancel().catch(() => {});
    return proxyError("BODY_TOO_LARGE", "请求内容过大", 413);
  }

  // The deadline covers both inbound body reading and the upstream request.
  const signal = AbortSignal.any([request.signal, AbortSignal.timeout(timeoutMilliseconds)]);
  let body: ArrayBuffer | undefined;
  try {
    body = await readBody(request, maximumBytes, signal);
    signal.throwIfAborted();
  } catch (error) {
    if (error instanceof BodyTooLarge) return proxyError("BODY_TOO_LARGE", "请求内容过大", 413);
    if (signal.aborted) return proxyError("API_UNAVAILABLE", "服务暂时不可用", 503);
    return proxyError("INVALID_BODY", "请求内容无法读取", 400);
  }

  try {
    const incomingURL = new URL(request.url);
    const path = segments.map(encodeURIComponent).join("/");
    const url = new URL(`${prefix}/${path}${incomingURL.search}`, baseURL);
    const headers = new Headers();
    for (const name of requestHeaders) {
      const value = request.headers.get(name);
      if (value) headers.set(name, value);
    }
    if (!headers.has("origin")) headers.set("origin", incomingURL.origin);
    const upstream = await fetcher(url, { method: request.method, headers, body, cache: "no-store", redirect: "manual", signal });
    const outgoing = new Headers({ "cache-control": "no-store" });
    for (const name of responseHeaders) {
      const value = upstream.headers.get(name);
      if (value) outgoing.set(name, value);
    }
    for (const cookie of upstream.headers.getSetCookie()) outgoing.append("set-cookie", cookie);
    return new Response(upstream.body, { status: upstream.status, headers: outgoing });
  } catch {
    return proxyError("API_UNAVAILABLE", "服务暂时不可用", 503);
  }
}
