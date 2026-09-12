import type { MediaManifestRequest, MediaManifestResponse, PrimaryMediaStateResponse, ReplacePrimaryMediaResponse } from "@self-deepsearch/api-contracts";
import { adminFetch } from "./reauth-client";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;
const STATUSES = new Set(["draft", "reviewing", "approved", "published", "hidden", "takedown", "merged"]);
function record(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}
function uuid(value: unknown): value is string { return typeof value === "string" && UUID.test(value); }
function sameID(value: unknown, expected: string): boolean { return uuid(value) && value.toLowerCase() === expected.toLowerCase(); }
function date(value: unknown): value is string { return typeof value === "string" && Number.isFinite(Date.parse(value)); }

async function bodyOf(response: Response): Promise<unknown> {
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    if (response.status === 409) throw new Error("主图或资产状态已变化，请刷新当前主图后重新确认；不会自动覆盖。");
    const detail = record(body) && record(body.error) && typeof body.error.message === "string" ? body.error.message : "图片操作未能确认";
    throw new Error(detail);
  }
  return body;
}

export async function getPrimaryMedia(manifest: MediaManifestRequest): Promise<PrimaryMediaStateResponse> {
  const query = new URLSearchParams({ entity_type: manifest.entity_type, entity_id: manifest.entity_id });
  const response = await adminFetch(`/admin/v1/media/primary?${query}`, { cache: "no-store" });
  const body = await bodyOf(response);
  if (response.status !== 200 || !record(body) || body.entity_type !== manifest.entity_type || !sameID(body.entity_id, manifest.entity_id) || typeof body.entity_status !== "string" || !STATUSES.has(body.entity_status) ||
    (body.primary !== null && (!record(body.primary) || !uuid(body.primary.asset_id) || !uuid(body.primary.entity_media_id) || !["cover", "gallery", "avatar"].includes(String(body.primary.purpose))))) {
    throw new Error("当前主图响应不完整，请刷新后再操作。");
  }
  return body as PrimaryMediaStateResponse;
}

function isPublished(value: unknown, manifest: MediaManifestRequest): value is MediaManifestResponse {
  return record(value) && sameID(value.asset_id, manifest.asset_id) && uuid(value.entity_media_id) && value.version === 1 && value.object_count === 4 && value.status === "published" && date(value.created_at);
}

export async function publishMedia(manifest: MediaManifestRequest, expectedAssetID: string | null): Promise<{ media: MediaManifestResponse; replacement: ReplacePrimaryMediaResponse | null }> {
  const replacement = expectedAssetID !== null;
  const response = await adminFetch(replacement ? "/admin/v1/media/primary/replace" : "/admin/v1/media/manifests", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify(replacement ? { expected_asset_id: expectedAssetID, manifest } : manifest),
  });
  const body = await bodyOf(response);
  const invalid = new Error("提交结果未能确认，清单已保留。请刷新当前主图核对是否已切换，不要直接重复提交。");
  if (response.status !== 201) throw invalid;
  if (!replacement) {
    if (!isPublished(body, manifest)) throw invalid;
    return { media: body, replacement: null };
  }
  if (!record(body) || !isPublished(body.media, manifest) || !sameID(body.previous_asset_id, expectedAssetID) ||
    typeof body.old_asset_retired !== "boolean" ||
    (body.old_asset_retired ? !date(body.private_retained_until) : body.private_retained_until !== null)) throw invalid;
  return { media: body.media, replacement: body as ReplacePrimaryMediaResponse };
}
