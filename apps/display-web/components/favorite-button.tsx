"use client";

import { Heart, LoaderCircle, RefreshCw } from "lucide-react";
import { useEffect, useId, useState } from "react";
import type { ItemListResponse } from "@self-deepsearch/api-contracts";
import { apiMutation } from "../lib/auth-client";

export function FavoriteButton({ workID }: { workID: string }) {
  const [saved, setSaved] = useState(false);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const errorID = useId();
  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/favorites", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("收藏状态暂时无法加载");
        const data = (await response.json()) as ItemListResponse;
        if (!active) return;
        setSaved(data.items.some((item) => item.id === workID));
        setLoadState("ready");
      })
      .catch(() => {
        if (!active) return;
        setError("收藏状态暂时无法加载");
        setLoadState("error");
      });
    return () => { active = false; };
  }, [loadAttempt, workID]);
  async function toggle() {
    setBusy(true); setError("");
    try {
      await apiMutation(`/api/v1/me/favorites/${encodeURIComponent(workID)}`, saved ? "DELETE" : "POST");
      setSaved((current) => !current);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "操作失败"); }
    finally { setBusy(false); }
  }
  function retryLoad() {
    setError("");
    setLoadState("loading");
    setLoadAttempt((current) => current + 1);
  }
  return <div aria-busy={busy || loadState === "loading"} className="inline-action"><button aria-describedby={error ? errorID : undefined} className={`button ${loadState === "ready" && !saved ? "button--primary" : "button--quiet"}`} type="button" onClick={loadState === "error" ? retryLoad : toggle} disabled={busy || loadState === "loading"}>{loadState === "loading" ? <><LoaderCircle aria-hidden="true" size={16} />读取收藏状态…</> : loadState === "error" ? <><RefreshCw aria-hidden="true" size={16} />重试收藏状态</> : <><Heart aria-hidden="true" size={16} fill={saved ? "currentColor" : "none"} />{busy ? "处理中…" : saved ? "已收藏" : "收藏作品"}</>}</button>{error && <span id={errorID} role="alert">{error}</span>}</div>;
}
