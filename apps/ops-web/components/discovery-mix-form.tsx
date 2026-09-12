"use client";

import type { DiscoveryMixRule } from "@self-deepsearch/api-contracts";
import { Save } from "lucide-react";
import { FormEvent, useState } from "react";
import { adminFetch } from "../lib/reauth-client";

export function DiscoveryMixForm({ initialRule }: { initialRule: DiscoveryMixRule }) {
  const [rule, setRule] = useState(initialRule);
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch("/admin/v1/site-settings/discovery-mix", {
        method: "PUT", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ ...rule, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as DiscoveryMixRule | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败");
        return;
      }
      setRule(body as DiscoveryMixRule);
      setReason("");
      setMessage("首页混排规则已保存，公开缓存将自动失效");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  return <form className="entity-form" onSubmit={submit}>
    <div className="form-grid">
      <label><span>每轮作品数</span><input type="number" min={1} max={8} required value={rule.work_slots} onChange={(event) => setRule({ ...rule, work_slots: Number(event.target.value) })} /></label>
      <label><span>每轮人物数</span><input type="number" min={1} max={4} required value={rule.performer_slots} onChange={(event) => setRule({ ...rule, performer_slots: Number(event.target.value) })} /></label>
      <label><span>混排窗口条数</span><input type="number" min={5} max={40} required value={rule.repeat_window} onChange={(event) => setRule({ ...rule, repeat_window: Number(event.target.value) })} /></label>
      <label className="check-label"><input type="checkbox" checked={rule.enabled} onChange={(event) => setRule({ ...rule, enabled: event.target.checked })} /><span>启用自定义规则</span></label>
      <label className="wide"><span>本次修改理由</span><textarea minLength={2} maxLength={1000} rows={2} required value={reason} onChange={(event) => setReason(event.target.value)} /></label>
    </div>
    {message ? <p aria-live="polite" className={message.includes("失败") || message.includes("无法") ? "form-error" : "form-success"}>{message}</p> : null}
    <div className="form-actions"><button className="primary" disabled={busy} type="submit"><Save size={17} />{busy ? "保存中" : "保存规则"}</button></div>
  </form>;
}
