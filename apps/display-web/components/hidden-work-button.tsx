"use client";

import type { HomeItem } from "@self-deepsearch/api-contracts";
import { Eye, EyeOff, LoaderCircle, RefreshCw } from "lucide-react";
import { useEffect, useId, useState } from "react";

export function HiddenWorkButton({ workID }: { workID: string }) {
  const [hidden, setHidden] = useState(false);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const errorID = useId();

  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/hidden-works", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("推荐偏好暂时无法加载");
        const data = (await response.json()) as { items: HomeItem[] };
        if (!active) return;
        setHidden(data.items.some((item) => item.id === workID));
        setLoadState("ready");
      })
      .catch(() => {
        if (!active) return;
        setError("推荐偏好暂时无法加载");
        setLoadState("error");
      });
    return () => { active = false; };
  }, [loadAttempt, workID]);

  async function toggle() {
    setBusy(true);
    setError(""); setMessage("");
    try {
      const response = await fetch(`/api/v1/me/hidden-works/${encodeURIComponent(workID)}`, {
        method: hidden ? "DELETE" : "POST", credentials: "same-origin",
      });
      if (!response.ok) throw new Error("操作失败，请稍后重试");
      setHidden((current) => !current);
      setMessage(hidden ? "已恢复这部作品的推荐。" : "已减少这部作品的推荐，仍可搜索找到");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "操作失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  function retryLoad() {
    setError("");
    setLoadState("loading");
    setLoadAttempt((current) => current + 1);
  }

  return <div aria-busy={busy || loadState === "loading"} className="inline-action"><button aria-describedby={error ? errorID : undefined} className="button button--quiet" type="button" onClick={loadState === "error" ? retryLoad : toggle} disabled={busy || loadState === "loading"}>{loadState === "loading" ? <><LoaderCircle aria-hidden="true" size={16} />读取推荐偏好…</> : loadState === "error" ? <><RefreshCw aria-hidden="true" size={16} />重试推荐偏好</> : <>{hidden ? <Eye aria-hidden="true" size={16} /> : <EyeOff aria-hidden="true" size={16} />}{busy ? "处理中…" : hidden ? "恢复推荐" : "不再推荐"}</>}</button>{message ? <span role="status">{message}</span> : null}{error ? <span id={errorID} role="alert">{error}</span> : null}</div>;
}
