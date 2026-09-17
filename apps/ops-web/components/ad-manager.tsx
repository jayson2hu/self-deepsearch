"use client";

import type { AdCampaign, UserSummary } from "@self-deepsearch/api-contracts";
import { Pause, Pencil, Plus, Radio, Save, X } from "lucide-react";
import { FormEvent, useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

type Placement = "banner" | "rail" | "infeed";
type Draft = {
  id: string | null;
  expected_version: number;
  name: string;
  advertiser: string;
  slot_code: string;
  placement: Placement;
  page_scope: AdCampaign["page_scope"];
  device_scope: AdCampaign["device_scope"];
  title: string;
  sponsor: string;
  image_url: string;
  image_alt: string;
  target_url: string;
  width: number;
  height: number;
  priority: number;
  starts_at: string;
  ends_at: string;
};

const sizes: Record<Placement, [number, number]> = { banner: [960, 90], rail: [160, 600], infeed: [640, 120] };

function emptyDraft(): Draft {
  const start = new Date();
  const end = new Date(start.getTime() + 7 * 24 * 60 * 60 * 1000);
  return { id: null, expected_version: 0, name: "", advertiser: "", slot_code: "home.after-latest", placement: "banner",
    page_scope: "home", device_scope: "all", title: "", sponsor: "", image_url: "", image_alt: "", target_url: "",
    width: 960, height: 90, priority: 0, starts_at: toLocalInput(start.toISOString()), ends_at: toLocalInput(end.toISOString()) };
}

export function AdManager({ initialItems, operatorRole }: { initialItems: AdCampaign[]; operatorRole: UserSummary["role"] }) {
  const [items, setItems] = useState(initialItems);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const isOwner = operatorRole === "owner";

  function edit(item: AdCampaign) {
    setDraft({ id: item.id, expected_version: item.version, name: item.name, advertiser: item.advertiser,
      slot_code: item.slot_code, placement: item.placement, page_scope: item.page_scope, device_scope: item.device_scope,
      title: item.title, sponsor: item.sponsor, image_url: item.image_url, image_alt: item.image_alt,
      target_url: item.target_url ?? "", width: item.width, height: item.height, priority: item.priority,
      starts_at: toLocalInput(item.starts_at), ends_at: toLocalInput(item.ends_at) });
    setMessage("");
  }

  function changePlacement(placement: Placement) {
    const [width, height] = sizes[placement];
    setDraft((current) => ({ ...current, placement, width, height }));
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setMessage("");
    try {
      const payload = { ...draft, target_url: draft.target_url.trim() || null,
        starts_at: new Date(draft.starts_at).toISOString(), ends_at: new Date(draft.ends_at).toISOString() };
      const response = await adminFetch(draft.id ? `/admin/v1/ads/${encodeURIComponent(draft.id)}` : "/admin/v1/ads", {
        method: draft.id ? "PATCH" : "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(payload),
      });
      const body = await response.json().catch(() => null) as AdCampaign | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "保存失败" : "保存失败");
        return;
      }
      const saved = body as AdCampaign;
      setItems((current) => [saved, ...current.filter((item) => item.id !== saved.id)]);
      setDraft(emptyDraft());
      setMessage("广告草稿已保存；发布前不会出现在公开站。");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  async function action(item: AdCampaign, operation: "publish" | "pause") {
    const verb = operation === "publish" ? "发布" : "紧急暂停";
    if (!window.confirm(`确认${verb}广告“${item.name}”？`)) return;
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/ads/${encodeURIComponent(item.id)}/${operation}`, {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: operation === "publish" ? JSON.stringify({ expected_version: item.version }) : JSON.stringify({}),
      });
      const body = await response.json().catch(() => null) as AdCampaign | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? `${verb}失败` : `${verb}失败`);
        return;
      }
      const saved = body as AdCampaign;
      setItems((current) => current.map((value) => value.id === saved.id ? saved : value));
      if (draft.id === saved.id) edit(saved);
      setMessage(operation === "publish" ? "广告已发布，公开站会在 30 秒内同步。" : "广告已暂停，公开站会在 30 秒内隐藏。");
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  return <div className="ad-manager">
    <form className="entity-form ad-form" onSubmit={submit}>
      <div className="form-grid">
        <label><span>活动名称</span><input maxLength={120} required value={draft.name} onChange={(event) => setDraft({ ...draft, name: event.target.value })} /></label>
        <label><span>广告主</span><input maxLength={120} required value={draft.advertiser} onChange={(event) => setDraft({ ...draft, advertiser: event.target.value })} /></label>
        <label><span>页面范围</span><select value={draft.page_scope} onChange={(event) => setDraft({ ...draft, page_scope: event.target.value as Draft["page_scope"] })}>
          {(["home", "work", "performer", "studio", "latest", "recent", "popular"] as const).map((value) => <option key={value} value={value}>{value}</option>)}
        </select></label>
        <label><span>稳定广告位代码</span><input pattern="[a-z][a-z0-9_.]{2,79}" required value={draft.slot_code} onChange={(event) => setDraft({ ...draft, slot_code: event.target.value })} /></label>
        <label><span>形态</span><select value={draft.placement} onChange={(event) => changePlacement(event.target.value as Placement)}><option value="banner">横幅 960×90</option><option value="rail">侧栏 160×600</option><option value="infeed">信息流 640×120</option></select></label>
        <label><span>设备</span><select value={draft.device_scope} onChange={(event) => setDraft({ ...draft, device_scope: event.target.value as Draft["device_scope"] })}><option value="all">全部</option><option value="desktop">桌面</option><option value="mobile">移动</option></select></label>
        <label><span>展示标题</span><input maxLength={120} required value={draft.title} onChange={(event) => setDraft({ ...draft, title: event.target.value })} /></label>
        <label><span>赞助方署名</span><input maxLength={120} required value={draft.sponsor} onChange={(event) => setDraft({ ...draft, sponsor: event.target.value })} /></label>
        <label className="wide"><span>素材地址（HTTPS 或 /ads/）</span><input required value={draft.image_url} onChange={(event) => setDraft({ ...draft, image_url: event.target.value })} /></label>
        <label><span>图片替代文本</span><input maxLength={160} value={draft.image_alt} onChange={(event) => setDraft({ ...draft, image_alt: event.target.value })} /></label>
        <label><span>落地页（可留空）</span><input value={draft.target_url} onChange={(event) => setDraft({ ...draft, target_url: event.target.value })} /></label>
        <label><span>开始时间</span><input required type="datetime-local" value={draft.starts_at} onChange={(event) => setDraft({ ...draft, starts_at: event.target.value })} /></label>
        <label><span>结束时间</span><input required type="datetime-local" value={draft.ends_at} onChange={(event) => setDraft({ ...draft, ends_at: event.target.value })} /></label>
        <label><span>优先级</span><input min={-1000} max={1000} type="number" value={draft.priority} onChange={(event) => setDraft({ ...draft, priority: Number(event.target.value) })} /></label>
        <label><span>固定尺寸</span><input disabled value={`${draft.width} × ${draft.height}`} /></label>
      </div>
      {message ? <p aria-live="polite" className={message.includes("失败") || message.includes("无法") ? "form-error" : "form-success"}>{message}</p> : null}
      <div className="form-actions">{draft.id ? <button disabled={busy} type="button" onClick={() => setDraft(emptyDraft())}><X size={17} />取消编辑</button> : null}<button className="primary" disabled={busy} type="submit">{draft.id ? <Save size={17} /> : <Plus size={17} />}{busy ? "处理中" : draft.id ? "保存新草稿" : "创建草稿"}</button></div>
    </form>
    <div className="table-wrap"><table className="ad-table"><thead><tr><th>活动</th><th>广告位</th><th>排期</th><th>状态</th><th>版本</th><th>操作</th></tr></thead><tbody>
      {items.map((item) => <tr key={item.id}><td><strong>{item.name}</strong><small>{item.advertiser} · {item.title}</small></td><td><code>{item.slot_code}</code><small>{item.placement} / {item.device_scope} / {item.width}×{item.height}</small></td><td>{formatDateTime(item.starts_at)}<small>至 {formatDateTime(item.ends_at)}</small></td><td><span className={`status ${item.status === "live" ? "ok" : item.status === "paused" || item.status === "expired" ? "neutral" : "waiting"}`}>{statusLabel(item.status)}</span></td><td>v{item.version}<small>{item.version_state}</small></td><td><div className="row-actions"><button aria-label="编辑广告" title="编辑草稿" disabled={busy} type="button" onClick={() => edit(item)}><Pencil size={15} /></button>{isOwner && item.version_state === "draft" ? <button aria-label="发布广告" className="approve" title="发布" disabled={busy} type="button" onClick={() => action(item, "publish")}><Radio size={15} /></button> : null}{isOwner && (item.status === "live" || item.status === "scheduled") ? <button aria-label="暂停广告" className="reject" title="紧急暂停" disabled={busy} type="button" onClick={() => action(item, "pause")}><Pause size={15} /></button> : null}</div></td></tr>)}
      {!items.length ? <tr><td className="empty-state" colSpan={6}>暂无广告活动；无填充时公开站不会展示占位。</td></tr> : null}
    </tbody></table></div>
  </div>;
}

function toLocalInput(value: string) {
  const date = new Date(value);
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

function statusLabel(status: AdCampaign["status"]) {
  return ({ draft: "草稿", scheduled: "待生效", live: "投放中", paused: "已暂停", expired: "已结束" } as const)[status];
}
