"use client";

import { Heart, LoaderCircle, RefreshCw } from "lucide-react";
import { useEffect, useId, useState } from "react";
import type { ItemListResponse } from "@self-deepsearch/api-contracts";
import { apiMutation, AuthRequestError } from "../lib/auth-client";
import { availableSessionStorage, clearAuthIntent, matchingAuthIntent } from "../lib/auth-intent";

export function FavoriteButton({ targetPath, workID }: { targetPath: string; workID: string }) {
  const [saved, setSaved] = useState(false);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [resume, setResume] = useState(false);
  const [resumeMessage, setResumeMessage] = useState("");
  const errorID = useId();
  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/favorites", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("收藏状态暂时无法加载");
        const data = (await response.json()) as ItemListResponse;
        if (!active) return;
        const isSaved = data.items.some((item) => item.id === workID);
        setSaved(isSaved);
        const storage = availableSessionStorage();
        const intent = storage ? matchingAuthIntent(storage, { kind: "favorite", entityId: workID, targetPath }) : null;
        if (storage && intent && isSaved) {
          clearAuthIntent(storage);
          setResumeMessage("这部作品已在收藏中，无需重复操作。");
        } else if (intent) {
          setResume(true);
        }
        setLoadState("ready");
      })
      .catch(() => {
        if (!active) return;
        setError("收藏状态暂时无法加载");
        setLoadState("error");
      });
    return () => { active = false; };
  }, [loadAttempt, targetPath, workID]);
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
  async function confirmResume() {
    setBusy(true); setError("");
    try {
      await apiMutation(`/api/v1/me/favorites/${encodeURIComponent(workID)}`, "POST");
      const storage = availableSessionStorage();
      if (storage) clearAuthIntent(storage);
      setResume(false); setSaved(true); setResumeMessage("已收藏这部作品。");
    } catch (cause) {
      if (cause instanceof AuthRequestError && (cause.code === "CONTENT_NOT_FOUND" || cause.code === "WORK_NOT_FOUND")) {
        const storage = availableSessionStorage();
        if (storage) clearAuthIntent(storage);
        setResume(false); setResumeMessage("原资料已不可用，未执行收藏。");
      } else {
        setError(cause instanceof Error ? cause.message : "收藏失败，请重试");
      }
    } finally { setBusy(false); }
  }
  function cancelResume() {
    const storage = availableSessionStorage();
    if (storage) clearAuthIntent(storage);
    setResume(false); setResumeMessage("已取消继续收藏。");
  }
  return <div aria-busy={busy || loadState === "loading"} className="inline-action">
    {resume && <div className="resume-intent" role="status"><strong>继续收藏这部作品？</strong><span>登录不会自动执行操作，请确认后继续。</span><div><button className="button button--primary" disabled={busy} onClick={confirmResume} type="button">{busy ? "收藏中…" : "确认收藏"}</button><button className="button button--quiet" disabled={busy} onClick={cancelResume} type="button">取消</button></div></div>}
    {resumeMessage && <span role="status">{resumeMessage}</span>}
    <button aria-describedby={error ? errorID : undefined} className={`button ${loadState === "ready" && !saved ? "button--primary" : "button--quiet"}`} type="button" onClick={loadState === "error" ? retryLoad : toggle} disabled={busy || loadState === "loading" || resume}>{loadState === "loading" ? <><LoaderCircle aria-hidden="true" size={16} />读取收藏状态…</> : loadState === "error" ? <><RefreshCw aria-hidden="true" size={16} />重试收藏状态</> : <><Heart aria-hidden="true" size={16} fill={saved ? "currentColor" : "none"} />{busy ? "处理中…" : saved ? "已收藏" : "收藏作品"}</>}</button>{error && <span id={errorID} role="alert">{error}</span>}
  </div>;
}
