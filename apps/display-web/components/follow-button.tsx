"use client";

import type { ItemListResponse } from "@self-deepsearch/api-contracts";
import { LoaderCircle, RefreshCw, UserCheck, UserPlus } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { apiMutation } from "../lib/auth-client";

export function FollowButton({ performerID }: { performerID: string }) {
  const [followed, setFollowed] = useState(false);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const errorID = useId();

  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/follows", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("关注状态暂时无法加载");
        const data = (await response.json()) as ItemListResponse;
        if (!active) return;
        setFollowed(data.items.some((item) => item.id === performerID));
        setLoadState("ready");
      })
      .catch(() => {
        if (!active) return;
        setError("关注状态暂时无法加载");
        setLoadState("error");
      });
    return () => { active = false; };
  }, [loadAttempt, performerID]);

  async function toggle() {
    setBusy(true);
    setError("");
    try {
      await apiMutation(`/api/v1/me/follows/${encodeURIComponent(performerID)}`, followed ? "DELETE" : "POST");
      setFollowed((current) => !current);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作失败");
    } finally {
      setBusy(false);
    }
  }

  function retryLoad() {
    setError("");
    setLoadState("loading");
    setLoadAttempt((current) => current + 1);
  }

  return <div aria-busy={busy || loadState === "loading"} className="inline-action"><button aria-describedby={error ? errorID : undefined} className={`button ${loadState === "ready" && !followed ? "button--primary" : "button--quiet"}`} type="button" onClick={loadState === "error" ? retryLoad : toggle} disabled={busy || loadState === "loading"}>{loadState === "loading" ? <><LoaderCircle aria-hidden="true" size={16} />读取关注状态…</> : loadState === "error" ? <><RefreshCw aria-hidden="true" size={16} />重试关注状态</> : <>{followed ? <UserCheck aria-hidden="true" size={16} /> : <UserPlus aria-hidden="true" size={16} />}{busy ? "处理中…" : followed ? "已关注" : "关注人物"}</>}</button>{error && <span id={errorID} role="alert">{error}</span>}</div>;
}
