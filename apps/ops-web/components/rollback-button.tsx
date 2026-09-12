"use client";

import type { ContentRevision } from "@self-deepsearch/api-contracts";
import { RotateCcw } from "lucide-react";
import { useState } from "react";
import { adminFetch } from "../lib/reauth-client";

export function RollbackButton({ revision }: { revision: ContentRevision }) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  async function rollback() {
    if (!revision.canonical_slug || !window.confirm(`确认把公开资料回滚到 v${revision.version}？该动作会生成发布审计和缓存刷新事件。`)) return;
    const reason = window.prompt("填写回滚理由");
    if (!reason || reason.trim().length < 2) return;
    setBusy(true); setMessage("");
    try {
      const base = "";
      const response = await adminFetch(`${base}/admin/v1/entities/${revision.entity_id}/publish`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entity_type: revision.entity_type, revision_id: revision.id, canonical_slug: revision.canonical_slug, reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      if (!response.ok) { setMessage(body?.error?.message ?? "回滚失败"); return; }
      window.location.reload();
    } catch { setMessage("无法连接运营服务"); }
    finally { setBusy(false); }
  }
  return <div><button className="icon-text-button" disabled={busy} onClick={rollback} type="button"><RotateCcw size={15} />{busy ? "回滚中" : "回滚到此版"}</button>{message ? <p className="form-error">{message}</p> : null}</div>;
}
