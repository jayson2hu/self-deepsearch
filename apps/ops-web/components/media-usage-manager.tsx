"use client";

import type { MediaUsageOverview, MediaUsageReviewInput } from "@self-deepsearch/api-contracts";
import { useEffect, useState, type FormEvent } from "react";
import { getMediaUsage, submitMediaUsageReview, UsageReviewConflict } from "../lib/media-usage-client";
import { formatDateTime } from "../lib/date-time";

const numberFormat = new Intl.NumberFormat("zh-CN");

export function MediaUsageManager() {
  const [overview, setOverview] = useState<MediaUsageOverview | null>(null);
  const [action, setAction] = useState<"acknowledge" | "rotate_period">("acknowledge");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState("");
  const [uncertain, setUncertain] = useState<MediaUsageReviewInput | null>(null);

  async function refresh(preserveMessage = false) {
    setBusy(true); setConfirmed(false);
    if (!preserveMessage) setMessage("");
    try { setOverview(await getMediaUsage()); }
    catch (error) { setOverview(null); setMessage(error instanceof Error ? error.message : "无法读取用量状态"); }
    finally { setBusy(false); }
  }
  useEffect(() => {
    let mounted = true;
    void getMediaUsage().then((data) => { if (mounted) setOverview(data); }).catch((error: unknown) => {
      if (mounted) setMessage(error instanceof Error ? error.message : "无法读取用量状态");
    }).finally(() => { if (mounted) setBusy(false); });
    return () => { mounted = false; };
  }, []);

  async function send(input: MediaUsageReviewInput) {
    setBusy(true); setMessage(""); setUncertain(input);
    try {
      const result = await submitMediaUsageReview(input);
      setUncertain(null); setConfirmed(false);
      setMessage(result.review.status === "pending" ? "复核请求已提交，等待 Worker 校验；尚未处理，更不代表已恢复图片。" : result.review.status === "applied" ? "该请求已处理，仅更新观察状态，不代表已恢复图片。" : "该请求已拒绝，请核对最新状态和拒绝原因。");
      await refresh(true);
    } catch (error) {
      if (error instanceof UsageReviewConflict) { setUncertain(null); setOverview(null); setConfirmed(false); }
      setMessage(error instanceof Error ? error.message : "提交结果未知，请保留同一请求编号核对。");
    } finally { setBusy(false); }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!overview?.state || !confirmed || busy || uncertain) return;
    void send({ idempotency_key: crypto.randomUUID(), action, expected_digest: overview.state.config_digest,
      expected_updated_at: overview.state.updated_at, next_period_start: action === "rotate_period" ? start.trim() : null,
      next_period_end: action === "rotate_period" ? end.trim() : null, reason: reason.trim(), confirmed: true });
  }
  const state = overview?.state;
  const pending = overview?.reviews.some((r) => r.status === "pending") ?? false;
  const canAcknowledge = state?.review_required && state.observation_fresh && !state.stop_recommended && ["low_estimate", "warning"].includes(state.last_status);

  return <div className="entity-form usage-manager">
    <p>这里只管理用量观察记录与复核建议。Analytics 不是账单，尚未连接自动停图或恢复；默认观察器关闭。</p>
    <div className="form-actions"><button type="button" disabled={busy} onClick={() => void refresh()}>{busy ? "读取或提交中…" : "刷新用量状态"}</button></div>
    <p role="status" aria-live="polite">{message}</p>
    {!busy && overview && !state ? <p>尚未初始化观察状态。请先按运维手册核对账户、账期和只读权限；此处不会启用观察器。</p> : null}
    {state ? <>
      <dl className="usage-facts">
        <div><dt>当前账期（开始包含，结束不包含；北京时间）</dt><dd>{formatDateTime(state.period_start)} → {formatDateTime(state.period_end)}</dd></div>
        <div><dt>A / B 类累计估计高水位</dt><dd>{state.last_success_at ? `${numberFormat.format(BigInt(state.class_a_highwater))} / ${numberFormat.format(BigInt(state.class_b_highwater))}` : "尚无有效统计，不按 0 计算"}</dd></div>
        <div><dt>观察状态</dt><dd>{state.last_status} · {state.observation_fresh ? "本地时间检查未过期（不是供应商完整水位）" : "缺失或已过期"}</dd></div>
        <div><dt>保护建议</dt><dd>{state.stop_recommended ? "已锁存停用建议" : state.review_required ? "需要人工复核" : "无持久复核标记（不代表可安全恢复）"} {state.review_reason ?? ""}</dd></div>
        <div><dt>快照更新时间 / 最后查询截至时间</dt><dd>{formatDateTime(state.updated_at)} / {state.last_until ? formatDateTime(state.last_until) : "暂无"}</dd></div>
      </dl>
      <form onSubmit={submit} className="entity-form">
        <fieldset disabled={busy || !!uncertain || pending || state.active_run}>
          <legend>提交人工复核</legend>
          <div className="form-grid">
            <label className="wide"><span>处理类型</span><select value={action} onChange={(e) => { setAction(e.target.value as typeof action); setStart(state.period_end); setConfirmed(false); }}>
              <option value="acknowledge">确认异常已核实，仅解除观察复核标记</option><option value="rotate_period">切换已确认的新账期，仍保持复核保护</option>
            </select></label>
            {action === "rotate_period" ? <><label><span>新账期开始（RFC3339，含时区）</span><input required value={start} onChange={(e) => { setStart(e.target.value); setConfirmed(false); }} placeholder="2026-10-01T00:00:00Z" /></label>
              <label><span>新账期结束（RFC3339，含时区）</span><input required value={end} onChange={(e) => { setEnd(e.target.value); setConfirmed(false); }} placeholder="2026-11-01T00:00:00Z" /></label></> : null}
            <label className="wide"><span>复核原因</span><textarea required minLength={2} maxLength={1000} value={reason} onChange={(e) => { setReason(e.target.value); setConfirmed(false); }} /></label>
            <label className="check-label wide"><input type="checkbox" required checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} /><span>已核对账户与统计；本操作不恢复图片，账期切换也不证明免费额度充足。</span></label>
          </div>
          {action === "rotate_period" ? <p>请先在日本 Worker 配置同一账户、同一策略的新账期。开始时间不得早于旧账期结束，且新账期必须包含当前时间；处理后还需新统计和再次复核。</p> : !canAcknowledge ? <p>仅允许对新鲜、非高风险且未锁存停用建议的统计解除复核；否则请先排查原因。</p> : null}
          <button className="primary" type="submit" disabled={!confirmed || reason.trim().length < 2 || (action === "acknowledge" && !canAcknowledge)}>提交复核请求</button>
        </fieldset>
      </form>
      {state.active_run ? <p>当前观察任务运行中，请稍后刷新。</p> : pending ? <p>已有待处理请求。请求五分钟有效；观察器关闭时不会处理，过期后由 Worker 记录拒绝。</p> : null}
    </> : null}
    {uncertain ? <div><p>尚未确认结果，保留请求编号：<code>{uncertain.idempotency_key}</code>。刷新不会丢弃此请求。</p><button type="button" disabled={busy} onClick={() => void send(uncertain)}>重试同一请求</button></div> : null}
    {overview ? <section aria-label="最近复核记录"><h2>最近复核记录</h2>{overview.reviews.length ? <ul>{overview.reviews.map((r) => <li key={r.review_id}>
      <strong>{({ pending: "待处理", applied: "已处理（仅观察状态）", rejected: "已拒绝" } as const)[r.status]}</strong> · {r.action === "rotate_period" ? "账期切换" : "异常复核"}<br />
      {r.reason}<br /><time dateTime={r.created_at}>{formatDateTime(r.created_at)}</time> · 有效至 {formatDateTime(r.expires_at)}{r.completed_at ? ` · 处理时间 ${formatDateTime(r.completed_at)}` : ""}{r.error_code ? ` · ${r.error_code}` : ""}
    </li>)}</ul> : <p>暂无复核请求。</p>}</section> : null}
  </div>;
}
