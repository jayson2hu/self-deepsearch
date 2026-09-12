import type { OperationsUser, SourceEvidence } from "@self-deepsearch/api-contracts";
import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { AdminShell } from "../../../../components/admin-shell";
import { RollbackButton } from "../../../../components/rollback-button";
import { RevisionEditor } from "../../../../components/revision-editor";
import { ReviewTaskActions } from "../../../../components/review-task-actions";
import { getContentRevisions, getOperationsUsers, getOperator, getReviewTasks } from "../../../../lib/api";
import { formatDateTime } from "../../../../lib/date-time";

export default async function VersionsPage({ params }: { params: Promise<{ entityType: string; entityID: string }> }) {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const { entityType, entityID } = await params;
  if (!(["work", "performer", "studio"] as string[]).includes(entityType) || !/^[0-9a-f-]{36}$/i.test(entityID)) notFound();
  const [revisions, tasks, users] = await Promise.all([getContentRevisions(entityType, entityID), getReviewTasks(), getOperationsUsers()]);
  const activeTask = tasks?.items.find((task) => task.entity_type === entityType && task.entity_id === entityID && (task.status === "pending" || task.status === "claimed"));
  const reviewingRevision = revisions?.items.find((revision) => revision.status === "reviewing");
  const reviewRevisionAvailable = Boolean(reviewingRevision);
  const reviewHasSources = Boolean(reviewingRevision?.sources.length);
  const reviewUnavailableReason = revisions === null
    ? "版本历史暂时无法读取，审核操作已停用。"
    : !reviewingRevision
      ? "未找到与任务对应的待审核版本，审核操作已停用。"
      : undefined;
  return <AdminShell active="资料目录" user={operator}><main className="main-content narrow-content">
    <Link className="back-link" href="/catalog">返回资料目录</Link>
    <div className="page-heading"><div><p><code>{entityID}</code></p><h1>版本历史</h1></div></div>
    {tasks === null ? <div className="service-warning dashboard-section" role="alert">无法读取审核任务，审核操作已停用。</div> : null}
    {activeTask ? <section className="phase-note"><div><strong>审核任务：{activeTask.status === "pending" ? "待领取" : "审核中"}</strong><p>核对版本内容和字段来源后再完成审核。</p></div><ReviewTaskActions canApprove={reviewHasSources} canInspectRevision={reviewRevisionAvailable} operator={operator} task={activeTask} reviewers={reviewers(users?.items)} unavailableReason={reviewUnavailableReason} /></section> : null}
    {revisions?.items[0] && (revisions.items[0].is_current || revisions.items[0].status === "rejected") ? <RevisionEditor latest={revisions.items[0]} /> : revisions?.items[0] ? <div className="phase-note"><div><strong>{revisions.items[0].status === "reviewing" ? "已有修订等待审核" : "已有修订等待发布"}</strong><p>当前流程完成前不能再提交新版本。</p></div></div> : null}
    {revisions ? <div className="revision-list">{revisions.items.map((revision, index) => {
      const older = revisions.items[index + 1];
      const changed = changedKeys(revision.payload, older?.payload);
      const sources = groupSources(revision.sources);
      return <article className="revision-item" key={revision.id}><header><div><strong>v{revision.version}</strong>{revision.is_current ? <span className="status ok">当前发布</span> : null}<span className="status neutral">{revision.status}</span></div><time dateTime={revision.created_at}>{formatDateTime(revision.created_at)}</time></header>
        <dl><div><dt>作者</dt><dd>{revision.author}</dd></div><div><dt>审核人</dt><dd>{revision.reviewer ?? "待审核"}</dd></div><div><dt>审核时间</dt><dd>{revision.reviewed_at ? <time dateTime={revision.reviewed_at}>{formatDateTime(revision.reviewed_at)}</time> : "待审核"}</dd></div><div><dt>理由</dt><dd>{revision.reason}</dd></div><div><dt>变化字段</dt><dd>{changed.length ? changed.join("、") : "初始快照"}</dd></div></dl>
        <pre>{JSON.stringify(revision.payload, null, 2)}</pre>
        <section className="revision-sources"><h3>字段来源</h3>{sources.length ? <div className="table-wrap"><table><thead><tr><th>字段</th><th>来源</th><th>核验人</th><th>核验时间</th><th>状态</th></tr></thead><tbody>{sources.map((source) => <tr key={source.key}><td>{source.fields.join("、")}</td><td><span>{sourceTypeLabel(source.source_type)}</span>{source.source_url ? <a href={source.source_url} rel="noreferrer" target="_blank">{source.source_title ?? source.source_url}</a> : <small>{source.source_title ?? "未提供 URL"}</small>}</td><td>{source.operator ?? "系统迁移"}</td><td><time dateTime={source.checked_at}>{formatDateTime(source.checked_at)}</time></td><td>{rightsStatusLabel(source.rights_status)}</td></tr>)}</tbody></table></div> : <div className="service-warning">该历史版本没有可用来源证据，不能发布。</div>}</section>
        {operator.role !== "editor" && revision.status === "approved" && !revision.is_current && revision.canonical_slug ? <RollbackButton revision={revision} /> : null}
      </article>;
    })}{!revisions.items.length ? <div className="empty-state">没有版本记录</div> : null}</div> : <div className="service-warning dashboard-section">无法读取版本历史。</div>}
  </main></AdminShell>;
}

function reviewers(users: OperationsUser[] | undefined) {
  return (users ?? []).filter((user) => user.account_status === "active" && (user.role === "admin" || user.role === "owner"));
}

function groupSources(sources: SourceEvidence[]) {
  const grouped = new Map<string, Omit<SourceEvidence, "field_name"> & { key: string; fields: string[] }>();
  for (const source of sources) {
    const key = JSON.stringify([source.source_type, source.source_url, source.source_title, source.checked_at, source.operator, source.rights_status]);
    const existing = grouped.get(key);
    if (existing) existing.fields.push(source.field_name);
    else grouped.set(key, { ...source, key, fields: [source.field_name] });
  }
  return Array.from(grouped.values());
}

function sourceTypeLabel(value: SourceEvidence["source_type"]) {
  return ({ web_page: "普通网页", search_result: "搜索结果", official: "官方资料", other: "其他资料", manual_csv: "人工 CSV", legacy_record: "迁移前资料" } as const)[value];
}

function rightsStatusLabel(value: SourceEvidence["rights_status"]) {
  return ({ needs_review: "待核验", allowed: "可用", restricted: "受限", takedown: "已下架" } as const)[value];
}

function changedKeys(current: Record<string, unknown>, older?: Record<string, unknown>) {
  if (!older) return [];
  return Array.from(new Set([...Object.keys(current), ...Object.keys(older)])).filter((key) => JSON.stringify(current[key]) !== JSON.stringify(older[key]));
}
