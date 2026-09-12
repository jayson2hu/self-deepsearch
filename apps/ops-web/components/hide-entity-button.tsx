"use client";

import type { CatalogEntity } from "@self-deepsearch/api-contracts";
import { EyeOff } from "lucide-react";
import { useState } from "react";
import { adminFetch } from "../lib/reauth-client";

export function HideEntityButton({ entity }: { entity: CatalogEntity }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");

  async function hide() {
    if (!window.confirm(`确认隐藏“${entity.label}”？公开页面、搜索结果和站点地图将撤下该资料。`)) return;
    const reason = window.prompt("填写隐藏理由");
    if (!reason || reason.trim().length < 2) return;
    setBusy(true);
    setMessage("");
    try {
      const response = await adminFetch(`/admin/v1/entities/${encodeURIComponent(entity.id)}/hide`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entity_type: entity.entity_type, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body?.error?.message ?? "隐藏失败");
        return;
      }
      window.location.reload();
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(false);
    }
  }

  return <span className="inline-operation"><button className="button compact-button danger-outline" disabled={busy} onClick={hide} type="button"><EyeOff size={14} />{busy ? "处理中" : "隐藏"}</button>{message ? <small role="alert">{message}</small> : null}</span>;
}
