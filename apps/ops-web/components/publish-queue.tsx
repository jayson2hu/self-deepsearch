"use client";

import type { ReviewTask } from "@self-deepsearch/api-contracts";
import { Send } from "lucide-react";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";
import { adminFetch } from "../lib/reauth-client";

function suggestedPublicSlug(task: ReviewTask): string {
  const shortID = task.entity_id?.replaceAll("-", "").slice(-8) || "new-item";
  const label = task.entity_type === "work" ? task.target_label.split("/")[0] : task.target_label;
  const normalized = label.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
  const fallback = task.entity_type === "performer" ? "performer" : task.entity_type === "studio" ? "studio" : "work";
  const base = (normalized || fallback).slice(0, 200 - shortID.length - 1).replace(/-+$/g, "");
  return `${base}-${shortID}`;
}

export function PublishQueue({ initialTasks }: { initialTasks: ReviewTask[] }) {
  const [tasks, setTasks] = useState(initialTasks);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");

  async function publish(task: ReviewTask) {
    if (!task.entity_id || !task.entity_type || task.entity_type === "media") return;
    const suggested = suggestedPublicSlug(task);
    const slug = window.prompt("填写公开页面 slug（小写字母、数字和连字符）", suggested);
    if (!slug) return;
    const reason = window.prompt("填写发布理由", "审核通过后发布");
    if (!reason || reason.trim().length < 2) return;
    setBusy(task.id);
    setMessage("");
    try {
      const base = "";
      const response = await adminFetch(`${base}/admin/v1/entities/${task.entity_id}/publish`, {
        method: "POST", credentials: "include", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ entity_type: task.entity_type, canonical_slug: slug.trim(), reason: reason.trim() }),
      });
      const body = await response.json().catch(() => null) as { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body?.error?.message ?? "发布失败");
        return;
      }
      setTasks((current) => current.filter((item) => item.id !== task.id));
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  if (tasks.length === 0) return <div className="empty-state">当前没有已通过待发布资料</div>;
  return <>
    {message ? <p aria-live="polite" className="form-error queue-error">{message}</p> : null}
    <div className="table-wrap"><table><thead><tr><th>类型</th><th>对象</th><th>通过时间</th><th>操作</th></tr></thead>
      <tbody>{tasks.map((task) => <tr key={task.id}><td><span className="type-badge">{task.entity_type === "work" ? "作品" : task.entity_type === "performer" ? "人物" : "厂牌"}</span></td>
        <td><strong>{task.target_label}</strong></td><td><time dateTime={task.updated_at}>{formatDateTime(task.updated_at)}</time></td>
        <td><button className="publish-button" disabled={busy !== null} onClick={() => publish(task)} type="button"><Send size={15} />发布</button></td></tr>)}</tbody>
    </table></div>
  </>;
}
