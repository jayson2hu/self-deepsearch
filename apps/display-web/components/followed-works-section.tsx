"use client";

import type { ItemListResponse } from "@self-deepsearch/api-contracts";
import { Bell, LoaderCircle, LogIn, RefreshCw } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { PersonalizedWorkGrid } from "./personalized-work-grid";

export function FollowedWorksSection() {
  const [items, setItems] = useState<ItemListResponse["items"]>([]);
  const [loadState, setLoadState] = useState<"loading" | "anonymous" | "ready" | "unavailable">("loading");
  const [loadAttempt, setLoadAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    fetch("/api/v1/me/followed-works", { credentials: "same-origin", cache: "no-store" })
      .then(async (response) => {
        if (!active) return;
        if (response.status === 401 || response.status === 403) {
          setLoadState("anonymous");
          return;
        }
        if (!response.ok) {
          setLoadState("unavailable");
          return;
        }
        const data = (await response.json()) as ItemListResponse;
        if (!active) return;
        setItems(data.items);
        setLoadState("ready");
      })
      .catch(() => { if (active) setLoadState("unavailable"); });
    return () => { active = false; };
  }, [loadAttempt]);

  return <section aria-busy={loadState === "loading"} className="section-block" aria-labelledby="followed-works-title">
    <div className="section-heading"><div><p className="section-kicker"><Bell size={15} /> 关注人物动态</p><h2 id="followed-works-title">关注人物的新作品</h2></div></div>
    {loadState === "loading" ? <div className="personalized-state" role="status"><LoaderCircle aria-hidden="true" size={17} />正在加载关注动态…</div> : null}
    {loadState === "anonymous" ? <div className="personalized-state"><span>登录后查看关注人物的新作品</span><Link className="button button--quiet" href="/login"><LogIn aria-hidden="true" size={15} />登录</Link></div> : null}
    {loadState === "unavailable" ? <div className="personalized-state personalized-state--error" role="alert"><span>关注动态暂时无法加载</span><button className="button button--quiet" onClick={() => { setLoadState("loading"); setLoadAttempt((current) => current + 1); }} type="button"><RefreshCw aria-hidden="true" size={15} />重试</button></div> : null}
    {loadState === "ready" ? <PersonalizedWorkGrid items={items} emptyText="关注人物暂时没有新的已发布作品" /> : null}
  </section>;
}
