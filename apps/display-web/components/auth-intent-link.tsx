"use client";

import { Heart, UserPlus } from "lucide-react";
import Link from "next/link";
import { availableSessionStorage, createAuthIntent, saveAuthIntent, type AuthIntentKind } from "../lib/auth-intent";
import { authPath } from "../lib/return-target";

export function AuthIntentLink({ kind, entityId, targetPath }: { kind: AuthIntentKind; entityId: string; targetPath: string }) {
  const label = kind === "favorite" ? "登录后收藏作品" : "登录后关注人物";
  function rememberIntent() {
    const intent = createAuthIntent(kind, entityId, targetPath);
    if (!intent) return;
    const storage = availableSessionStorage();
    if (storage) saveAuthIntent(storage, intent);
  }
  return <Link className="button button--primary" href={authPath("/login", targetPath)} onClick={rememberIntent}>
    {kind === "favorite" ? <Heart aria-hidden="true" size={16} /> : <UserPlus aria-hidden="true" size={16} />}{label}
  </Link>;
}
