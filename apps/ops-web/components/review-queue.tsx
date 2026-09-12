"use client";

import type { ReviewTask, UserSummary } from "@self-deepsearch/api-contracts";
import { Eye, Hand } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { formatDateTime } from "../lib/date-time";

export function ReviewQueue({ initialTasks, operator }: { initialTasks: ReviewTask[]; operator: UserSummary }) {
  const [tasks, setTasks] = useState(initialTasks);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState("");

  async function act(task: ReviewTask) {
    setBusy(`${task.id}:claim`);
    setMessage("");
    try {
      const base = "";
      const response = await fetch(`${base}/admin/v1/review-tasks/${task.id}/claim`, {
        method: "POST",
        credentials: "include",
      });
      const body = await response.json().catch(() => null) as ReviewTask | { error?: { message?: string } } | null;
      if (!response.ok) {
        setMessage(body && "error" in body ? body.error?.message ?? "操作失败" : "操作失败");
        return;
      }
      const updated = body as ReviewTask;
      setTasks((current) => updated.status === "approved" || updated.status === "rejected"
        ? current.filter((item) => item.id !== updated.id)
        : current.map((item) => item.id === updated.id ? updated : item));
    } catch {
      setMessage("无法连接运营服务");
    } finally {
      setBusy(null);
    }
  }

  if (tasks.length === 0) return <div className="empty-state">当前没有待处理审核任务</div>;
  return (
    <>
      {message ? <p aria-live="polite" className="form-error queue-error">{message}</p> : null}
      <div className="table-wrap">
        <table>
          <thead><tr><th>类型</th><th>对象</th><th>状态</th><th>领取人</th><th>创建时间</th><th>操作</th></tr></thead>
          <tbody>{tasks.map((task) => (
            <tr key={task.id}>
              <td><span className="type-badge">{typeLabel(task.entity_type)}</span></td>
              <td><strong>{task.target_label || task.entity_id || "待归一化资料"}</strong></td>
              <td>{task.status === "pending" ? "待领取" : "审核中"}</td>
              <td>{task.assignee ?? "未领取"}</td>
              <td><time dateTime={task.created_at}>{formatDateTime(task.created_at)}</time></td>
              <td><div className="row-actions">
                {task.entity_type && task.entity_id && task.entity_type !== "media" ? <Link aria-label="查看版本和来源" href={`/catalog/${task.entity_type}/${task.entity_id}`} title="查看版本和来源"><Eye size={15} /></Link> : null}
                {task.status === "pending" && operator.role !== "editor" ? <button aria-label="领取任务" disabled={busy !== null} onClick={() => act(task)} title="领取任务"><Hand size={15} /></button> : null}
              </div></td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </>
  );
}

function typeLabel(type: ReviewTask["entity_type"]) {
	if (!type) return "资料";
	return ({ work: "作品", performer: "人物", studio: "厂牌", media: "图片" } as const)[type];
}
