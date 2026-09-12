"use client";

import { LoaderCircle, LogIn, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import type { UserSummary } from "@self-deepsearch/api-contracts";
import { FavoriteButton } from "./favorite-button";
import { FeedbackForm } from "./feedback-form";
import { HiddenWorkButton } from "./hidden-work-button";

type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: UserSummary }
  | { status: "unavailable" };

export function WorkActions({ workID }: { workID: string }) {
  const [session, setSession] = useState<SessionState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    fetch("/api/v1/me", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!active) return;
        if (response.ok) {
          const data = (await response.json()) as { user: UserSummary };
          if (active) setSession({ status: "authenticated", user: data.user });
        } else if (response.status === 401 || response.status === 403) {
          setSession({ status: "anonymous" });
        } else {
          setSession({ status: "unavailable" });
        }
      })
      .catch(() => { if (active) setSession({ status: "unavailable" }); });
    return () => { active = false; };
  }, [attempt]);
  if (session.status === "loading") return <div className="client-action-state" role="status"><LoaderCircle aria-hidden="true" size={16} />正在加载用户操作…</div>;
  if (session.status === "unavailable") return <div className="client-action-state client-action-state--error" role="alert"><span>用户操作暂时不可用</span><button className="button button--quiet" onClick={() => { setSession({ status: "loading" }); setAttempt((current) => current + 1); }} type="button"><RefreshCw aria-hidden="true" size={15} />重试</button></div>;
  return session.status === "authenticated" ? <><FavoriteButton workID={workID} /><HiddenWorkButton workID={workID} /><FeedbackForm workID={workID} /></> : <Link className="button button--primary" href="/login"><LogIn size={16} />登录后收藏、隐藏或纠错</Link>;
}
