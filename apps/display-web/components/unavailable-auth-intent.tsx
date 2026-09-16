"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { availableSessionStorage, clearAuthIntent, loadAuthIntent } from "../lib/auth-intent";

export function UnavailableAuthIntent() {
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      const storage = availableSessionStorage();
      if (!storage) return;
      const intent = loadAuthIntent(storage);
      if (intent && intent.targetPath === window.location.pathname) {
        clearAuthIntent(storage);
        setUnavailable(true);
      }
    }, 0);
    return () => window.clearTimeout(timer);
  }, []);
  return unavailable ? <div className="resume-intent resume-intent--unavailable" role="status">
    <strong>原资料已不可用</strong><span>未执行收藏或关注，你可以重新搜索其他资料。</span><Link href="/search">前往搜索</Link>
  </div> : null;
}
