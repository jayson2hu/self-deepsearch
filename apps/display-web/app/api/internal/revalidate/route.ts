import { createHmac, timingSafeEqual } from "node:crypto";
import { revalidatePath, revalidateTag } from "next/cache";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const MAX_BODY_BYTES = 16 * 1024;
const MAX_CLOCK_SKEW_SECONDS = 5 * 60;
const allowedEventTypes = new Set([
  "publication_changed",
  "publication_hidden",
  "publication_takedown",
  "search_refresh",
  "cache_purge",
  "sitemap_refresh",
]);

export async function POST(request: Request): Promise<Response> {
  const secret = process.env.CACHE_HMAC_SECRET?.trim();
  if (!secret || secret.length < 32) return json({ error: "not_found" }, 404);

  const declaredLength = request.headers.get("content-length");
  if (declaredLength && (!/^\d+$/.test(declaredLength) || Number(declaredLength) > MAX_BODY_BYTES)) {
    return json({ error: "payload_too_large" }, 413);
  }
  const bodyBytes = await readLimitedBody(request);
  if (!bodyBytes) return json({ error: "payload_too_large" }, 413);
  let body: string;
  try {
    body = new TextDecoder("utf-8", { fatal: true }).decode(bodyBytes);
  } catch {
    return json({ error: "invalid_json" }, 400);
  }

  const eventID = request.headers.get("x-sd-event-id")?.trim() ?? "";
  const timestamp = request.headers.get("x-sd-timestamp")?.trim() ?? "";
  const signature = request.headers.get("x-sd-signature")?.trim() ?? "";
  if (!/^\d+$/.test(timestamp) || !isUUID(eventID) || !verifySignature(secret, timestamp, eventID, bodyBytes, signature)) {
    return json({ error: "unauthorized" }, 401);
  }
  const timestampSeconds = Number(timestamp);
  if (!Number.isSafeInteger(timestampSeconds) || Math.abs(Math.floor(Date.now() / 1000) - timestampSeconds) > MAX_CLOCK_SKEW_SECONDS) {
    return json({ error: "stale_request" }, 401);
  }

  let payload: unknown;
  try {
    payload = JSON.parse(body);
  } catch {
    return json({ error: "invalid_json" }, 400);
  }
  if (!isRevalidationEvent(payload) || payload.event_id !== eventID || !allowedEventTypes.has(payload.event_type)) {
    return json({ error: "invalid_event" }, 400);
  }

  revalidateTag("public-catalog", { expire: 0 });
  revalidatePath("/", "layout");
  return json({ revalidated: true, event_id: eventID }, 200);
}

function verifySignature(secret: string, timestamp: string, eventID: string, body: Uint8Array, provided: string): boolean {
  if (!/^v1=[a-f0-9]{64}$/.test(provided)) return false;
  const expected = createHmac("sha256", secret).update(`${timestamp}\n${eventID}\n`).update(body).digest();
  const actual = Buffer.from(provided.slice(3), "hex");
  return actual.length === expected.length && timingSafeEqual(actual, expected);
}

function isRevalidationEvent(value: unknown): value is {
  event_id: string; event_type: string; aggregate_type: string; aggregate_id: string; payload: Record<string, unknown>;
} {
  return typeof value === "object" && value !== null
    && isUUID((value as { event_id?: unknown }).event_id)
    && typeof (value as { event_type?: unknown }).event_type === "string"
    && typeof (value as { aggregate_type?: unknown }).aggregate_type === "string"
    && (value as { aggregate_type: string }).aggregate_type.length >= 1
    && (value as { aggregate_type: string }).aggregate_type.length <= 100
    && isUUID((value as { aggregate_id?: unknown }).aggregate_id)
    && typeof (value as { payload?: unknown }).payload === "object"
    && (value as { payload?: unknown }).payload !== null
    && !Array.isArray((value as { payload?: unknown }).payload);
}

function isUUID(value: unknown): value is string {
  return typeof value === "string" && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(value);
}

async function readLimitedBody(request: Request): Promise<Uint8Array | null> {
  if (!request.body) return new Uint8Array();
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    total += value.byteLength;
    if (total > MAX_BODY_BYTES) {
      await reader.cancel();
      return null;
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

function json(value: unknown, status: number): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8", "Cache-Control": "no-store" },
  });
}
