"use client";

import type { ClearHistoryResponse } from "@self-deepsearch/api-contracts";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { apiMutation } from "../lib/auth-client";

export function ClearHistoryButton() {
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function clear() {
    setBusy(true);
    setError("");
    try { await apiMutation<ClearHistoryResponse>("/api/v1/me/history", "DELETE"); router.refresh(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "清除历史失败，请稍后重试"); }
    finally { setBusy(false); }
  }
  return <div className="inline-action"><button aria-describedby={error ? "clear-history-error" : undefined} className="button button--quiet" type="button" onClick={clear} disabled={busy}>{busy ? "处理中…" : "清除可见历史"}</button>{error ? <span id="clear-history-error" role="alert">{error}</span> : null}</div>;
}
