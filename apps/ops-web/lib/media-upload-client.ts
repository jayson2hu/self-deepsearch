import type { MediaUploadCommand, MediaUploadInput, MediaUploadOverview, MediaUploadReceipt, MediaUploadResult, MediaUploadState } from "@self-deepsearch/api-contracts";
import { adminFetch } from "./reauth-client";

const MAX_BODY = 128 * 1024;
const uuid = (v: unknown): v is string => typeof v === "string" && /^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/.test(v);
const record = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === "object" && !Array.isArray(v);
const nullable = (v: unknown, check: (v: unknown) => boolean) => v === null || check(v);
const code = (v: unknown) => typeof v === "string" && /^[a-z_]{1,64}$/.test(v);
const modes = new Set(["paused", "enabled"]);
const openStates = new Set(["pending", "running", "uncertain"]);

function timestamp(v: unknown): v is string {
  if (typeof v !== "string") return false;
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.exec(v);
  return !!m && +m[1] > 0 && +m[2] >= 1 && +m[2] <= 12 && +m[3] >= 1 &&
    +m[3] <= new Date(Date.UTC(+m[1], +m[2], 0)).getUTCDate() && +m[4] <= 23 && +m[5] <= 59 && +m[6] <= 59 && Number.isFinite(Date.parse(v));
}

function receipt(v: unknown): v is MediaUploadReceipt {
  if (!record(v) || Object.keys(v).length !== 6 || typeof v.status !== "string" || !["observed", "applied", "replayed"].includes(v.status) ||
    !Number.isSafeInteger(v.generation) || Number(v.generation) < 0 || typeof v.mode !== "string" || !modes.has(v.mode)) return false;
  if (v.generation === 0) return v.status === "observed" && v.mode === "paused" && v.epoch === "" && v.command_id === "" && v.applied_at === "";
  return uuid(v.epoch) && uuid(v.command_id) && timestamp(v.applied_at) && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/.test(v.applied_at);
}

function command(v: unknown): v is MediaUploadCommand {
  if (!record(v) || !uuid(v.command_id) || !uuid(v.actor_id) || typeof v.mode !== "string" || !modes.has(v.mode) || typeof v.status !== "string" ||
    ![...openStates, "applied", "rejected", "superseded"].includes(v.status) || !timestamp(v.created_at) || !timestamp(v.expires_at) ||
    !nullable(v.first_dispatched_at, timestamp) || !nullable(v.completed_at, timestamp) || !nullable(v.error_code, code) || !nullable(v.receipt, receipt) ||
    typeof v.reason !== "string" || [...v.reason].length < 2 || [...v.reason].length > 1000 || typeof v.idempotent_replay !== "boolean" ||
    !Number.isInteger(v.dispatch_count) || Number(v.dispatch_count) < 0 || Number(v.dispatch_count) > 1000000) return false;
  const started = v.first_dispatched_at !== null;
  if (started !== (Number(v.dispatch_count) > 0) || Date.parse(v.expires_at) <= Date.parse(v.created_at) ||
    Date.parse(v.expires_at) - Date.parse(v.created_at) > 300000 ||
    (started && (Date.parse(String(v.first_dispatched_at)) < Date.parse(v.created_at) || Date.parse(String(v.first_dispatched_at)) >= Date.parse(v.expires_at))) ||
    openStates.has(String(v.status)) !== (v.completed_at === null) || (v.completed_at !== null && Date.parse(String(v.completed_at)) < Date.parse(v.created_at))) return false;
  if (v.status === "pending" && (started || v.receipt !== null || v.error_code !== null)) return false;
  if (v.status === "running" && (!started || v.error_code !== null)) return false;
  if (v.status === "applied" && (!started || v.error_code !== null || !receipt(v.receipt) || v.receipt.generation < 1 || v.receipt.command_id !== v.command_id || v.receipt.mode !== v.mode)) return false;
  if (["rejected", "superseded"].includes(String(v.status)) && v.error_code === null) return false;
  if (v.status === "superseded" && (!started || !receipt(v.receipt))) return false;
  return true;
}

export function isUploadStateFresh(state: MediaUploadState | null | undefined, now = Date.now()): boolean {
  return !!state?.fresh && !!state.receipt && state.last_success_at !== null && state.error_code === null &&
    now >= Date.parse(state.last_success_at) && now - Date.parse(state.last_success_at) <= 120000;
}

export function isOpenUploadCommand(value: MediaUploadCommand): boolean { return openStates.has(value.status); }

export function validateUploadOverview(v: unknown): v is MediaUploadOverview {
  if (!record(v) || !Array.isArray(v.commands) || v.commands.length > 10 || !v.commands.every(command) ||
    new Set(v.commands.map((c) => c.command_id)).size !== v.commands.length || v.commands.filter(isOpenUploadCommand).length > 1) return false;
  const s = v.state;
  if (s === null) return v.commands.length === 0;
  return record(s) && typeof s.target_ref === "string" && /^[a-f0-9]{64}$/.test(s.target_ref) && timestamp(s.updated_at) &&
    nullable(s.last_success_at, timestamp) && nullable(s.receipt, receipt) && nullable(s.error_code, code) && typeof s.fresh === "boolean" &&
    (s.receipt === null) === (s.last_success_at === null) && (s.receipt === null || (receipt(s.receipt) && s.receipt.status === "observed")) &&
    (s.last_success_at === null || Date.parse(String(s.last_success_at)) <= Date.parse(s.updated_at)) &&
    (!s.fresh || (s.last_success_at !== null && s.error_code === null));
}

export class UploadControlConflict extends Error {}

async function bodyOf(response: Response): Promise<unknown> {
  if (response.status === 409) throw new UploadControlConflict("状态已变化或已有请求。请刷新状态并重新核对，不要沿用旧确认。");
  if (!response.ok) {
    const message = response.status === 401 ? "登录或近期密码验证未完成。请验证后重试同一请求。" : response.status === 403 ? "当前账号无权操作，请核对登录账号与权限。" :
      response.status === 404 ? "尚无可用的上传控制状态，请联系运维核对配置。" : "上传控制暂时无法确认，请刷新核对；已有请求请重试原编号。";
    throw new Error(message);
  }
  if (!/^application\/json(?:\s*;|$)/i.test(response.headers.get("content-type") ?? "") || !response.body) throw new Error("上传控制响应不完整，不能确认结果。");
  const reader = response.body.getReader();
  const decoder = new TextDecoder("utf-8", { fatal: true });
  let size = 0, text = "", complete = false;
  try {
    while (true) {
      const { value, done } = await reader.read();
      if (done) { complete = true; break; }
      size += value.byteLength;
      if (size > MAX_BODY) throw new Error("上传控制响应过大，不能确认结果。");
      text += decoder.decode(value, { stream: true });
    }
    return JSON.parse(text + decoder.decode()) as unknown;
  } catch {
    throw new Error("上传控制响应不完整或超过限制，不能确认结果。");
  } finally {
    if (!complete) await reader.cancel().catch(() => undefined);
    reader.releaseLock();
  }
}

export async function getUploadControl(signal?: AbortSignal): Promise<MediaUploadOverview> {
  const response = await adminFetch("/admin/v1/media/upload-control", { cache: "no-store", redirect: "error",
    signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(5000)]) : AbortSignal.timeout(5000) });
  const body = await bodyOf(response);
  if (response.status !== 200 || !validateUploadOverview(body)) throw new Error("上传状态响应不完整，已禁止新操作，请刷新或联系运维。");
  return body;
}

export async function submitUploadCommand(input: MediaUploadInput, operatorID: string): Promise<MediaUploadResult> {
  const response = await adminFetch("/admin/v1/media/upload-control/commands", { method: "POST", redirect: "error", cache: "no-store",
    headers: { "Content-Type": "application/json" }, body: JSON.stringify(input), signal: AbortSignal.timeout(15000) });
  const body = await bodyOf(response);
  if (![200, 202].includes(response.status) || !record(body) || !command(body.command) || body.command.actor_id !== operatorID ||
    body.command.mode !== input.mode || body.command.reason !== input.reason ||
    (response.status === 202 ? body.command.status !== "pending" || body.command.idempotent_replay : !body.command.idempotent_replay) ||
    (body.command.status === "applied" && (body.command.receipt?.epoch !== (input.expected_epoch || body.command.command_id) || body.command.receipt?.generation !== input.expected_generation + 1))) {
    throw new Error("提交回执不完整或不匹配。请保留原请求编号，刷新核对或重试同一请求。");
  }
  return body as MediaUploadResult;
}
