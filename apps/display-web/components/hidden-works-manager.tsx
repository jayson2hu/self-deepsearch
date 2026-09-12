"use client";

import type { HomeItem } from "@self-deepsearch/api-contracts";
import { Eye } from "lucide-react";
import { useId, useState } from "react";
import { WorkCard } from "./work-card";

export function HiddenWorksManager({ initialItems }: { initialItems: HomeItem[] }) {
  const [items, setItems] = useState(initialItems);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [error, setError] = useState("");
  const errorID = useId();
  async function restore(workID: string) {
    setBusyID(workID);
    setError("");
    try {
      const response = await fetch(`/api/v1/me/hidden-works/${encodeURIComponent(workID)}`, { method: "DELETE", credentials: "same-origin" });
      if (!response.ok) throw new Error("暂时无法恢复该作品，请稍后重试");
      setItems((current) => current.filter((item) => item.id !== workID));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "暂时无法恢复该作品，请稍后重试");
    } finally {
      setBusyID(null);
    }
  }
  if (!items.length) return <div className="empty-state"><strong>没有隐藏的作品</strong></div>;
  return <><div aria-busy={busyID !== null} className="work-grid private-grid">{items.map((item, index) => <div className="managed-work" key={item.id}><WorkCard item={item} position={index + 1} /><button aria-describedby={error ? errorID : undefined} className="button button--quiet" disabled={busyID !== null} type="button" onClick={() => restore(item.id)}><Eye aria-hidden="true" size={15} />{busyID === item.id ? "恢复中…" : "恢复展示"}</button></div>)}</div>{error ? <p className="form-error" id={errorID} role="alert">{error}</p> : null}</>;
}
