"use client";

import type { ItemListResponse } from "@self-deepsearch/api-contracts";
import { LoaderCircle, RefreshCw, UserCheck, UserPlus } from "lucide-react";
import { useEffect, useId, useState } from "react";
import { apiMutation, AuthRequestError } from "../lib/auth-client";
import { availableSessionStorage, clearAuthIntent, matchingAuthIntent } from "../lib/auth-intent";

export function FollowButton({ performerID, targetPath }: { performerID: string; targetPath: string }) {
  const [followed, setFollowed] = useState(false);
  const [loadState, setLoadState] = useState<"loading" | "ready" | "error">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [resume, setResume] = useState(false);
  const [resumeMessage, setResumeMessage] = useState("");
  const errorID = useId();

  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/follows", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!response.ok) throw new Error("关注状态暂时无法加载");
        const data = (await response.json()) as ItemListResponse;
        if (!active) return;
        const isFollowed = data.items.some((item) => item.id === performerID);
        setFollowed(isFollowed);
        const storage = availableSessionStorage();
        const intent = storage ? matchingAuthIntent(storage, { kind: "follow", entityId: performerID, targetPath }) : null;
        if (storage && intent && isFollowed) {
          clearAuthIntent(storage);
          setResumeMessage("你已关注这位人物，无需重复操作。");
        } else if (intent) {
          setResume(true);
        }
        setLoadState("ready");
      })
      .catch(() => {
        if (!active) return;
        setError("关注状态暂时无法加载");
        setLoadState("error");
      });
    return () => { active = false; };
  }, [loadAttempt, performerID, targetPath]);

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
  async function confirmResume() {
    setBusy(true); setError("");
    try {
      await apiMutation(`/api/v1/me/follows/${encodeURIComponent(performerID)}`, "POST");
      const storage = availableSessionStorage();
      if (storage) clearAuthIntent(storage);
      setResume(false); setFollowed(true); setResumeMessage("已关注这位人物。");
    } catch (cause) {
      if (cause instanceof AuthRequestError && (cause.code === "CONTENT_NOT_FOUND" || cause.code === "PERFORMER_NOT_FOUND")) {
        const storage = availableSessionStorage();
        if (storage) clearAuthIntent(storage);
        setResume(false); setResumeMessage("原资料已不可用，未执行关注。");
      } else {
        setError(cause instanceof Error ? cause.message : "关注失败，请重试");
      }
    } finally { setBusy(false); }
  }
  function cancelResume() {
    const storage = availableSessionStorage();
    if (storage) clearAuthIntent(storage);
    setResume(false); setResumeMessage("已取消继续关注。");
  }

  return <div aria-busy={busy || loadState === "loading"} className="inline-action">
    {resume && <div className="resume-intent" role="status"><strong>继续关注这位人物？</strong><span>登录不会自动执行操作，请确认后继续。</span><div><button className="button button--primary" disabled={busy} onClick={confirmResume} type="button">{busy ? "关注中…" : "确认关注"}</button><button className="button button--quiet" disabled={busy} onClick={cancelResume} type="button">取消</button></div></div>}
    {resumeMessage && <span role="status">{resumeMessage}</span>}
    <button aria-describedby={error ? errorID : undefined} className={`button ${loadState === "ready" && !followed ? "button--primary" : "button--quiet"}`} type="button" onClick={loadState === "error" ? retryLoad : toggle} disabled={busy || loadState === "loading" || resume}>{loadState === "loading" ? <><LoaderCircle aria-hidden="true" size={16} />读取关注状态…</> : loadState === "error" ? <><RefreshCw aria-hidden="true" size={16} />重试关注状态</> : <>{followed ? <UserCheck aria-hidden="true" size={16} /> : <UserPlus aria-hidden="true" size={16} />}{busy ? "处理中…" : followed ? "已关注" : "关注人物"}</>}</button>{error && <span id={errorID} role="alert">{error}</span>}
  </div>;
}
