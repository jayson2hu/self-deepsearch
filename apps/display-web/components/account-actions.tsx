"use client";

import { LoaderCircle, LogIn, RefreshCw, UserPlus, UserRound } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import type { UserSummary } from "@self-deepsearch/api-contracts";

type SessionState =
  | { status: "loading" }
  | { status: "anonymous" }
  | { status: "authenticated"; user: UserSummary }
  | { status: "unavailable" };

export function AccountActions() {
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

  if (session.status === "loading") return <GuestActions status="loading" />;
  if (session.status === "unavailable") return <GuestActions status="unavailable" onRetry={() => { setSession({ status: "loading" }); setAttempt((current) => current + 1); }} />;
  if (session.status === "anonymous") return <GuestActions status="anonymous" />;
  return <details className="account-menu"><summary className="button button--quiet"><UserRound size={16} />我的账号</summary><nav aria-label="账号菜单"><span>{session.user.email}</span><Link href="/account/favorites">收藏作品</Link><Link href="/account/follows">关注人物</Link><Link href="/account/hidden">已隐藏作品</Link><Link href="/account/history">浏览历史</Link><Link href="/account/feedback">我的反馈</Link><Link href="/account/settings">账号设置</Link><form action="/auth/logout" method="post"><button type="submit">退出登录</button></form></nav></details>;
}

function GuestActions({ status, onRetry }: { status: "loading" | "anonymous" | "unavailable"; onRetry?: () => void }) {
  return <div className={`account-actions account-actions--${status}`}>
    <Link className="button button--quiet" href="/login"><LogIn aria-hidden="true" size={16} />登录</Link>
    <Link className="button button--primary" href="/register"><UserPlus aria-hidden="true" size={16} />注册</Link>
    {status === "loading" ? <span aria-label="正在检查登录状态" className="account-status-indicator" role="status"><LoaderCircle aria-hidden="true" size={16} /></span> : null}
    {status === "unavailable" ? <button aria-label="账号服务暂不可用，重试" className="account-status-indicator account-status-retry" onClick={onRetry} title="账号服务暂不可用，重试" type="button"><RefreshCw aria-hidden="true" size={16} /></button> : null}
  </div>;
}
