"use client";

import type { HomeItem } from "@self-deepsearch/api-contracts";
import { useEffect, useMemo, useState } from "react";
import { WorkCard } from "./work-card";

export type WorkGridContext = "recommendation" | "catalog";

export function PersonalizedWorkGrid({ context, items, emptyText }: { context: WorkGridContext; items: HomeItem[]; emptyText: string }) {
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [preferenceState, setPreferenceState] = useState<"inactive" | "loading" | "anonymous" | "ready" | "unavailable">(context === "recommendation" ? "loading" : "inactive");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busyWorkID, setBusyWorkID] = useState<string | null>(null);
  const [lastHiddenID, setLastHiddenID] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);

  useEffect(() => {
    if (context !== "recommendation") return;
    let active = true;
    fetch("/api/v1/me/hidden-works", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!active) return;
        if (response.status === 401 || response.status === 403) {
          setPreferenceState("anonymous");
          return;
        }
        if (!response.ok) {
          setPreferenceState("unavailable");
          return;
        }
        const data = (await response.json()) as { items: HomeItem[] };
        if (!active) return;
        setHidden(new Set(data.items.map((item) => item.id)));
        setPreferenceState("ready");
      })
      .catch(() => { if (active) setPreferenceState("unavailable"); });
    return () => { active = false; };
  }, [context, loadAttempt]);

  const visible = useMemo(() => context === "recommendation" ? items.filter((item) => !hidden.has(item.id)) : items, [context, hidden, items]);
  async function hide(workID: string) {
    if (busyWorkID) return;
    setBusyWorkID(workID);
    setMessage("");
    try {
      const response = await fetch(`/api/v1/me/hidden-works/${encodeURIComponent(workID)}`, {
        method: "POST", credentials: "same-origin",
      });
      if (!response.ok) throw new Error("暂时无法更新推荐偏好，请稍后重试");
      setHidden((current) => new Set(current).add(workID));
      setLastHiddenID(workID);
      setMessageIsError(false);
      setMessage("已减少这部作品的推荐，仍可搜索找到");
    } catch (cause) {
      setMessageIsError(true);
      setMessage(cause instanceof Error ? cause.message : "暂时无法更新推荐偏好，请稍后重试");
    } finally {
      setBusyWorkID(null);
    }
  }

  async function undoHide() {
    if (!lastHiddenID || busyWorkID) return;
    const workID = lastHiddenID;
    setBusyWorkID(workID); setMessage("");
    try {
      const response = await fetch(`/api/v1/me/hidden-works/${encodeURIComponent(workID)}`, { method: "DELETE", credentials: "same-origin" });
      if (!response.ok) throw new Error("暂时无法恢复推荐，请稍后重试");
      setHidden((current) => { const next = new Set(current); next.delete(workID); return next; });
      setLastHiddenID(null); setMessageIsError(false); setMessage("已恢复这部作品的推荐。");
    } catch (cause) {
      setMessageIsError(true); setMessage(cause instanceof Error ? cause.message : "暂时无法恢复推荐，请稍后重试");
    } finally { setBusyWorkID(null); }
  }

  function retryPreferences() {
    setPreferenceState("loading");
    setLoadAttempt((current) => current + 1);
  }

  return <div aria-busy={preferenceState === "loading" || busyWorkID !== null}>
    {visible.length ? <div className="work-grid">{visible.map((item, index) => <WorkCard key={item.id} item={item} position={index + 1} onHide={context === "recommendation" && preferenceState === "ready" ? hide : undefined} />)}</div> : <div className="empty-copy">{emptyText}</div>}
    {preferenceState === "loading" ? <p className="preference-message" role="status">正在加载推荐偏好…</p> : null}
    {preferenceState === "unavailable" ? <div className="preference-message preference-message--error" role="alert"><span>推荐偏好暂时不可用，当前展示完整列表。</span><button className="preference-retry" onClick={retryPreferences} type="button">重试</button></div> : null}
    {message ? <div className={`preference-message${messageIsError ? " preference-message--error" : ""}`} role={messageIsError ? "alert" : "status"}><span>{message}</span>{lastHiddenID && !messageIsError ? <button className="preference-retry" onClick={undoHide} type="button">撤销</button> : null}</div> : null}
  </div>;
}
