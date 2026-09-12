import type { MediaUsageOverview, MediaUsageReview, MediaUsageReviewInput, MediaUsageReviewResult } from "@self-deepsearch/api-contracts";
import { adminFetch } from "./reauth-client";

function record(v: unknown): v is Record<string, unknown> { return !!v && typeof v === "object" && !Array.isArray(v); }
function date(v: unknown): v is string { return typeof v === "string" && /^\d{4}-\d{2}-\d{2}T.*(?:Z|[+-]\d{2}:\d{2})$/.test(v) && Number.isFinite(Date.parse(v)); }
function uuid(v: unknown): v is string { return typeof v === "string" && /^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$/i.test(v); }
function count(v: unknown): boolean { return typeof v === "string" && /^[0-9]{1,20}$/.test(v) && BigInt(v) <= 18446744073709551615n; }
function nullable(v: unknown, check: (value: unknown) => boolean): boolean { return v === null || check(v); }
function review(v: unknown): v is MediaUsageReview {
  return record(v) && uuid(v.review_id) && ["acknowledge", "rotate_period"].includes(String(v.action)) && ["pending", "applied", "rejected"].includes(String(v.status)) &&
    date(v.created_at) && date(v.expires_at) && nullable(v.completed_at, date) && nullable(v.error_code, (value) => typeof value === "string") &&
    typeof v.reason === "string" && nullable(v.next_period_start, date) && nullable(v.next_period_end, date) && typeof v.idempotent_replay === "boolean" &&
    (v.status === "pending" ? v.completed_at === null && v.error_code === null : date(v.completed_at) && (v.status === "applied" ? v.error_code === null : typeof v.error_code === "string"));
}

export class UsageReviewConflict extends Error {}

async function bodyOf(response: Response): Promise<unknown> {
  const body: unknown = await response.json().catch(() => null);
  if (response.status === 409) throw new UsageReviewConflict("状态已变化、任务运行中或已有复核请求，请刷新核对后重新确认。");
  if (!response.ok) throw new Error(record(body) && record(body.error) && typeof body.error.message === "string" ? body.error.message : "用量请求未能确认，请稍后核对。");
  return body;
}

export async function getMediaUsage(): Promise<MediaUsageOverview> {
  const response = await adminFetch("/admin/v1/media/usage", { cache: "no-store", redirect: "error", signal: AbortSignal.timeout(5000) });
  const body = await bodyOf(response);
  if (response.status !== 200 || !record(body) || body.enforcement !== "not_connected" || !Array.isArray(body.reviews) || body.reviews.length > 10 || !body.reviews.every(review)) throw new Error("用量状态响应不完整，不能提交复核。");
  const s = body.state;
  if (s !== null && (!record(s) || typeof s.config_digest !== "string" || !/^[a-f0-9]{64}$/.test(s.config_digest) || !date(s.updated_at) || !date(s.period_start) || !date(s.period_end) ||
    !nullable(s.last_success_at, date) || !nullable(s.last_until, date) || !count(s.class_a_highwater) || !count(s.class_b_highwater) ||
    !nullable(s.review_reason, (v) => typeof v === "string") || !nullable(s.last_review_id, uuid) ||
    !["unknown", "low_estimate", "warning", "high", "stop_recommended"].includes(String(s.last_status)) ||
    [s.review_required, s.stop_recommended, s.active_run, s.observation_fresh].some((v) => typeof v !== "boolean"))) throw new Error("用量状态响应不完整，不能提交复核。");
  return body as MediaUsageOverview;
}

export async function submitMediaUsageReview(input: MediaUsageReviewInput): Promise<MediaUsageReviewResult> {
  const response = await adminFetch("/admin/v1/media/usage/reviews", {
    method: "POST", redirect: "error", headers: { "Content-Type": "application/json" }, body: JSON.stringify(input), signal: AbortSignal.timeout(15000),
  });
  const body = await bodyOf(response);
  if (![200, 202].includes(response.status) || !record(body) || body.enforcement !== "not_connected" || !review(body.review) ||
    body.review.action !== input.action || body.review.reason !== input.reason ||
    (response.status === 202 ? body.review.status !== "pending" || body.review.idempotent_replay : !body.review.idempotent_replay) ||
    (input.action === "rotate_period" && (Date.parse(body.review.next_period_start ?? "") !== Date.parse(input.next_period_start ?? "") || Date.parse(body.review.next_period_end ?? "") !== Date.parse(input.next_period_end ?? "")))) {
    throw new Error("提交结果不完整，保留同一请求编号；请刷新记录或重试同一请求，不要另建重复请求。");
  }
  return body as MediaUsageReviewResult;
}
