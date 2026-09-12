"use client";

import type { HomeItem } from "@self-deepsearch/api-contracts";
import { useEffect, useMemo, useState } from "react";
import { PerformerCard } from "./performer-card";
import { WorkCard } from "./work-card";

export function DiscoveryGrid({ items }: { items: HomeItem[] }) {
  const [hidden, setHidden] = useState<Set<string>>(new Set());
  const [preferenceState, setPreferenceState] = useState<"loading" | "anonymous" | "ready" | "unavailable">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busyWorkID, setBusyWorkID] = useState<string | null>(null);
  const [message, setMessage] = useState("");
  const [messageIsError, setMessageIsError] = useState(false);

  useEffect(() => {
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
  }, [loadAttempt]);

  const visible = useMemo(() => items.filter((item) => item.type === "performer" || !hidden.has(item.id)), [hidden, items]);
  async function hide(workID: string) {
    if (busyWorkID) return;
    setBusyWorkID(workID);
    setMessage("");
    try {
      const response = await fetch(`/api/v1/me/hidden-works/${encodeURIComponent(workID)}`, {
        method: "POST", credentials: "same-origin",
      });
      if (!response.ok) throw new Error("暂时无法隐藏该作品，请稍后重试");
      setHidden((current) => new Set(current).add(workID));
      setMessageIsError(false);
      setMessage("已从你的发现列表中隐藏，可在账号菜单中恢复");
    } catch (cause) {
      setMessageIsError(true);
      setMessage(cause instanceof Error ? cause.message : "暂时无法隐藏该作品，请稍后重试");
    } finally {
      setBusyWorkID(null);
    }
  }

  function retryPreferences() {
    setPreferenceState("loading");
    setLoadAttempt((current) => current + 1);
  }

  return <div aria-busy={preferenceState === "loading" || busyWorkID !== null}>
    {visible.length ? <div className="discovery-grid">{visible.map((item, index) => item.type === "performer"
      ? <PerformerCard key={item.id} item={item} />
      : <WorkCard key={item.id} item={item} position={index + 1} onHide={preferenceState === "ready" ? hide : undefined} />)}</div>
      : <div className="empty-copy">暂无已发布资料</div>}
    {preferenceState === "loading" ? <p className="preference-message" role="status">正在加载个性化设置…</p> : null}
    {preferenceState === "unavailable" ? <div className="preference-message preference-message--error" role="alert"><span>个性化设置暂时不可用，当前展示完整列表。</span><button className="preference-retry" onClick={retryPreferences} type="button">重试</button></div> : null}
    {message ? <p className={`preference-message${messageIsError ? " preference-message--error" : ""}`} role={messageIsError ? "alert" : "status"}>{message}</p> : null}
  </div>;
}
