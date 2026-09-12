"use client";

import type { MediaUploadCommand, MediaUploadInput, MediaUploadOverview } from "@self-deepsearch/api-contracts";
import Link from "next/link";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { formatDateTime } from "../lib/date-time";
import { getUploadControl, isOpenUploadCommand, isUploadStateFresh, submitUploadCommand, UploadControlConflict } from "../lib/media-upload-client";

const statusLabels: Record<MediaUploadCommand["status"], string> = {
  pending: "待处理", running: "已派发 · 等待回执", uncertain: "结果待确认", applied: "已应用 · 回执确认", rejected: "已拒绝", superseded: "已被更新状态取代",
};
const failureLabels: Record<string, string> = {
  control_status_unavailable: "北京端状态暂不可读", control_apply_unconfirmed: "派发后未取得有效回执",
  control_request_expired: "请求已过有效期", control_expiry_confirmation_pending: "过期请求仍在等待确认",
  control_snapshot_changed: "执行前快照已变化", control_resume_guard_denied: "恢复未通过用量准入",
  control_actor_or_window_invalid: "操作者权限或请求期限不再有效", control_dispatch_window_short: "剩余执行窗口不足",
};

function CommandHistory({ commands }: { commands: MediaUploadCommand[] }) {
  return <section className="upload-history" aria-labelledby="upload-history-title">
    <div><h2 id="upload-history-title">最近上传控制请求</h2><p>最多展示 10 项。以下是请求回执，不代表此刻的完整网站状态；时间均为北京时间。</p></div>
    {commands.length === 0 ? <p className="upload-empty">暂无请求记录。</p> : <ol>{commands.map((item) => <li key={item.command_id}>
      <article>
        <header><h3>{item.mode === "paused" ? "暂停新上传" : "恢复新上传"}</h3><span className={`upload-badge upload-badge--${item.status}`}>{statusLabels[item.status]}</span></header>
        <p className="upload-reason">{item.reason}</p>
        <dl className="upload-record-facts">
          <div><dt>提交时间</dt><dd><time dateTime={item.created_at}>{formatDateTime(item.created_at)}</time></dd></div>
          <div><dt>请求有效至</dt><dd>{formatDateTime(item.expires_at)}</dd></div>
          <div><dt>派发次数</dt><dd>{item.dispatch_count} 次{item.first_dispatched_at ? ` · 首次 ${formatDateTime(item.first_dispatched_at)}` : " · 尚无派发记录"}</dd></div>
          <div><dt>处理时间</dt><dd>{item.completed_at ? formatDateTime(item.completed_at) : "尚未确认完成"}</dd></div>
        </dl>
        {item.error_code ? <p className="upload-record-error">{failureLabels[item.error_code] ?? "请按错误码核对执行状态"} <code>{item.error_code}</code></p> : null}
        <details><summary>查看指令与操作者标识</summary><dl>
          <dt>指令编号</dt><dd><code>{item.command_id}</code></dd><dt>操作者 ID</dt><dd><code>{item.actor_id}</code></dd>
          {item.receipt ? <><dt>回执状态 / 代次</dt><dd>{item.receipt.status} / {item.receipt.generation}</dd><dt>控制链 epoch</dt><dd><code>{item.receipt.epoch || "尚未初始化"}</code></dd></> : null}
        </dl></details>
      </article>
    </li>)}</ol>}
  </section>;
}

export function MediaUploadManager({ operatorID }: { operatorID: string }) {
  const [overview, setOverview] = useState<MediaUploadOverview | null>(null);
  const [mode, setMode] = useState<MediaUploadInput["mode"]>("paused");
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(true);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState<MediaUploadInput | null>(null);
  const [now, setNow] = useState(Date.now);
  const busyRef = useRef(true);

  useEffect(() => {
    const controller = new AbortController();
    let mounted = true;
    void getUploadControl(controller.signal).then((data) => { if (mounted) setOverview(data); }).catch((e: unknown) => {
      if (mounted) setError(e instanceof Error ? e.message : "无法读取上传状态");
    }).finally(() => { if (mounted) { setBusy(false); busyRef.current = false; } });
    const timer = setInterval(() => setNow(Date.now()), 1000);
    const resetConfirmation = () => { setConfirmed(false); setNow(Date.now()); };
    document.addEventListener("visibilitychange", resetConfirmation);
    return () => { mounted = false; controller.abort(); clearInterval(timer); document.removeEventListener("visibilitychange", resetConfirmation); };
  }, []);

  async function load() {
    try { setOverview(await getUploadControl()); setNow(Date.now()); }
    catch (e) { setOverview(null); setError(e instanceof Error ? e.message : "刷新失败，不能确认当前上传状态"); }
  }
  async function refresh() {
    if (busyRef.current) return;
    busyRef.current = true; setBusy(true); setConfirmed(false); setNotice(""); setError("");
    try { await load(); } finally { setBusy(false); busyRef.current = false; }
  }
  async function send(input: MediaUploadInput) {
    if (busyRef.current) return;
    busyRef.current = true; setBusy(true); setNotice(""); setError(""); setConfirmed(false);
    try {
      const { command } = await submitUploadCommand(input, operatorID);
      setUncertain(null); setMode("paused");
      setNotice(isOpenUploadCommand(command) ? "请求已受理，尚未确认完成。请刷新查看北京端回执，不要重复创建请求。" :
        command.status === "applied" ? "该请求已有有效执行回执。请以最新观察为准；其他用量、未知上传和页面保护不会自动解除。" : "该请求已结束，请查看拒绝或被取代的原因，再核对最新状态。");
      await load();
    } catch (e) {
      if (e instanceof UploadControlConflict) { setUncertain(null); setOverview(null); }
      else setUncertain(input);
      setError(e instanceof Error ? e.message : "提交结果未知，请保留原请求核对。");
    } finally { setBusy(false); busyRef.current = false; }
  }

  const state = overview?.state;
  const fresh = isUploadStateFresh(state, now);
  const open = overview?.commands.find(isOpenUploadCommand);
  const initialized = (state?.receipt?.generation ?? 0) > 0;
  const formDisabled = busy || !!uncertain || !!open || !fresh;
  const validReason = [...reason.trim()].length >= 2 && [...reason.trim()].length <= 1000 && !/[\u0000-\u001f\u007f]/.test(reason);

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (formDisabled || !confirmed || !validReason || !state?.receipt || !state.last_success_at || !isUploadStateFresh(state) || (mode === "enabled" && !initialized)) return;
    void send({ idempotency_key: crypto.randomUUID(), target_ref: state.target_ref, expected_epoch: state.receipt.epoch,
      expected_generation: state.receipt.generation, expected_observed_at: state.last_success_at, mode, reason: reason.trim(), confirmed: true });
  }

  return <div className="upload-manager">
    <aside className="upload-scope"><strong>仅控制新图片上传</strong><p>暂停影响新上传准入、Listing 和 PUT；不删除已有图片，不关闭网站展示，也不阻断权利下架。恢复仍需 Worker 核对用量，不会清除未确认上传或其他保护。</p><Link href="/media/usage">查看用量复核 →</Link></aside>
    <div className="upload-toolbar"><p>执行器默认关闭 · 状态按需刷新</p><button type="button" disabled={busy} onClick={() => void refresh()}>{busy ? "读取或提交中…" : "刷新上传状态"}</button></div>
    <p role="status" aria-live="polite" className="upload-notice">{notice}</p>
    {error ? <p role="alert" aria-label="上传控制错误" className="upload-error">{error}</p> : null}
    {!busy && overview && !state ? <div className="upload-empty"><h2>尚未连接上传执行器</h2><p>请先按运维手册检查日本队列、北京 enforce、独立密钥和共享卷。此页面不能直接启用服务。</p></div> : null}
    {state ? <>
      <section className={`upload-snapshot ${fresh ? "" : "upload-snapshot--stale"}`} aria-labelledby="upload-state-title">
        <div><p id="upload-state-title">北京执行器 · 最近确认状态</p><h2>{!state.receipt ? "尚无可信回执" : !initialized ? "控制链尚未初始化" : state.receipt.mode === "paused" ? "最近确认：新上传已暂停" : "最近确认：新上传已允许"}</h2>
          <span className="upload-badge">{fresh ? "观察未过期 · 不代表此刻在线" : "状态缺失、异常或已过期 · 禁止新操作"}</span></div>
        <dl className="upload-record-facts"><div><dt>最近观察（北京时间）</dt><dd>{state.last_success_at ? formatDateTime(state.last_success_at) : "暂无"}</dd></div>
          <div><dt>控制代次</dt><dd>{state.receipt?.generation ?? "未知"}</dd></div></dl>
        {state.error_code ? <p>{failureLabels[state.error_code] ?? "执行状态异常"} <code>{state.error_code}</code></p> : null}
        <details><summary>查看目标和控制链标识</summary><dl><dt>目标摘要（不是访问地址）</dt><dd><code>{state.target_ref}</code></dd><dt>控制链 epoch</dt><dd><code>{state.receipt?.epoch || "尚未初始化"}</code></dd></dl></details>
      </section>
      {open ? <p className="upload-pending">已有{statusLabels[open.status]}请求。有效期届满不等于执行失败；请刷新等待有效回执，勿清理底层记录或另行重复操作。</p> : null}
      <form className="entity-form" onSubmit={submit}>
        <fieldset disabled={formDisabled}>
          <legend>提交上传控制请求</legend>
          <div className="form-grid">
            <label className="wide"><span id="upload-mode-label">操作类型</span><select aria-labelledby="upload-mode-label" value={mode} onChange={(e) => { setMode(e.target.value as MediaUploadInput["mode"]); setConfirmed(false); }}>
              <option value="paused">暂停新上传{initialized ? "" : "（初始化控制链）"}</option><option value="enabled" disabled={!initialized}>恢复新上传（需用量准入）</option>
            </select></label>
            <label className="wide"><span id="upload-reason-label">操作原因</span><textarea aria-labelledby="upload-reason-label" required minLength={2} maxLength={1000} rows={3} value={reason} onChange={(e) => { setReason(e.target.value); setConfirmed(false); }} placeholder="记录核对依据、操作目的或关联工单" /></label>
          </div>
          <div className={mode === "enabled" ? "upload-confirm upload-confirm--resume" : "upload-confirm"}>
            <h3>{mode === "enabled" ? "恢复前请再次核对" : "确认暂停的影响范围"}</h3>
            <p>{mode === "enabled" ? "已核对真实用量和未确认上传？复核通过或账期切换不代表额度充足。本请求只申请恢复新上传，可能被 Worker 拒绝。" : "当前 SDK 调用可能仍在执行。收到北京端有效回执后才算暂停完成；已有图片与必要删除不受影响。"}</p>
            <label className="check-label"><input type="checkbox" required checked={confirmed} onChange={(e) => setConfirmed(e.target.checked)} /><span>{mode === "enabled" ? "我已核对用量和未确认上传，明确申请恢复新上传，并知悉其他保护不会解除。" : "我确认申请暂停新上传，并知悉提交成功不等于已经暂停。"}</span></label>
          </div>
          <button className={mode === "enabled" ? "upload-resume" : "primary"} type="submit" disabled={formDisabled || !confirmed || !validReason || (mode === "enabled" && !initialized)}>{mode === "enabled" ? "提交恢复请求" : "提交暂停请求"}</button>
          <p className="upload-hint">请求最多五分钟有效；近期密码验证由 API 要求。刷新、修改原因或切换操作后，需要重新确认。</p>
        </fieldset>
      </form>
    </> : null}
    {uncertain ? <aside className="upload-uncertain"><h2>保留原请求，先确认结果</h2><p>本次受理结果未知。刷新不会丢弃此请求，离开页面也不会撤销已经受理的操作。</p>
      <dl><dt>幂等请求编号（不是执行指令编号）</dt><dd><code>{uncertain.idempotency_key}</code></dd><dt>原操作</dt><dd>{uncertain.mode === "paused" ? "暂停新上传" : "恢复新上传"}</dd></dl>
      <button type="button" disabled={busy} onClick={() => void send(uncertain)}>重试同一上传请求</button></aside> : null}
    {overview ? <CommandHistory commands={overview.commands} /> : null}
  </div>;
}
