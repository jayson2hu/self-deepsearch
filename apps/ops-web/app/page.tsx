import { Activity, AlertTriangle, Cloud, Database, FileCheck2, Inbox, RefreshCcw, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../components/admin-shell";
import { ReviewQueue } from "../components/review-queue";
import { PublishQueue } from "../components/publish-queue";
import { getOperationsDashboard, getOperator } from "../lib/api";
import { formatDateTime } from "../lib/date-time";

const mediaStorageLimitBytes = 10 * 1024 * 1024 * 1024;

export default async function DashboardPage() {
  const user = await getOperator();
  if (!user) redirect("/login");
  const canViewSystemHealth = user.role !== "editor";
  const { tasks, approved, health } = await getOperationsDashboard(canViewSystemHealth);
  const taskItems = tasks?.items ?? [];

  return (
    <AdminShell active="运营概览" user={user}>
      <main className="main-content">
        <div className="page-heading">
          <div><p>{new Intl.DateTimeFormat("zh-CN", { dateStyle: "long", timeZone: "Asia/Shanghai" }).format(new Date())} · 日本主库{health ? <> · 状态检查 <time dateTime={health.checked_at}>{formatDateTime(health.checked_at)}</time></> : null}</p><h1>运营概览</h1></div>
          <div className="heading-actions"><Link className="button primary" href="/works/new">新建作品</Link></div>
        </div>

        <section aria-label="运营指标" className="metric-grid">
          <article><span className="metric-icon amber"><FileCheck2 size={18} /></span><div><small>待处理审核</small><strong>{health?.pending_review_tasks ?? taskItems.length}</strong><p>包含待领取与审核中任务</p></div></article>
          <article><span className="metric-icon blue"><Activity size={18} /></span><div><small>待审核版本</small><strong>{health?.reviewing_revisions ?? "-"}</strong><p>版本通过后才允许发布</p></div></article>
          <article><span className="metric-icon green"><Database size={18} /></span><div><small>数据库版本</small><strong>{health ? `v${health.schema_version}` : "-"}</strong><p>{health ? "运营数据连接正常" : "当前无法读取状态"}</p></div></article>
          <article><span className="metric-icon red"><AlertTriangle size={18} /></span><div><small>异常信号</small><strong>{health ? health.failed_outbox_events + health.failed_media_reconciles + health.media_reconcile_issues + health.media_publication_issues + health.default_image_failures + health.failed_media_inspections : "-"}</strong><p>任务与图片检查问题；同一资产可能重复计数</p></div></article>
          {canViewSystemHealth ? <>
            <article><span className="metric-icon blue"><Inbox size={18} /></span><div><small>Outbox 积压</small><strong>{health?.pending_outbox_events ?? "-"}</strong><p>{health ? "发布、缓存和删除事件等待处理" : "当前无法读取状态"}</p></div></article>
            <article><span className={`metric-icon ${mediaLevel(health?.media_bytes)}`}><Cloud size={18} /></span><div><small>媒体存储</small><strong>{health ? formatBytes(health.media_bytes) : "-"}</strong><p>{health ? `${formatPercent(health.media_bytes, mediaStorageLimitBytes)} / 10 GiB；${mediaGuidance(health.media_bytes)}` : "当前无法读取状态"}</p></div></article>
            <article><span className={`metric-icon ${ageLevel(health?.verified_backup_age_seconds, 8 * 24 * 60 * 60)}`}><ShieldCheck size={18} /></span><div><small>已验证备份</small><strong>{formatAge(health?.verified_backup_age_seconds)}</strong><p>{!health ? "当前无法读取状态" : health.verified_backup_age_seconds === null ? "尚无复制和恢复均通过的备份" : "超过 8 天按严重故障处理"}</p></div></article>
            <article><span className={`metric-icon ${ageLevel(health?.media_reconcile_age_seconds, 2 * 24 * 60 * 60)}`}><RefreshCcw size={18} /></span><div><small>媒体对账</small><strong>{formatAge(health?.media_reconcile_age_seconds)}</strong><p>{!health ? "当前无法读取状态" : health.media_reconcile_age_seconds === null ? "尚无完成的媒体对账" : `${health.media_reconcile_issues} 个问题，24 小时内失败 ${health.failed_media_reconciles} 次`}</p></div></article>
            <article><span className={`metric-icon ${health && (health.media_publication_issues > 0 || health.default_image_failures > 0 || health.failed_media_inspections > 0) ? "red" : ageLevel(health?.media_inspection_age_seconds, 2 * 24 * 60 * 60)}`}><ShieldCheck size={18} /></span><div><small>每日图片检查</small><strong>{formatAge(health?.media_inspection_age_seconds)}</strong><p>{!health ? "当前无法读取状态" : health.media_inspection_age_seconds === null ? "尚未完成检查，不代表图片正常" : `发布状态问题 ${health.media_publication_issues} 项；默认图失败 ${health.default_image_failures}/2`}</p>{health ? <p>24 小时内检查任务失败 {health.failed_media_inspections} 次；只检查，不自动修复</p> : null}</div></article>
          </> : null}
        </section>

        <section className="review-section dashboard-section" aria-labelledby="review-title">
          <div className="section-heading"><div><h2 id="review-title">待处理审核</h2><p>数据来自运营 API，状态变化会写入审计记录</p></div></div>
          {tasks ? <ReviewQueue initialTasks={taskItems} operator={user} /> : <div className="service-warning">无法读取审核队列，请检查 API、数据库与登录状态。</div>}
        </section>

        {user.role !== "editor" ? <section className="review-section dashboard-section" aria-labelledby="publish-title">
          <div className="section-heading"><div><h2 id="publish-title">已通过待发布</h2><p>发布会同步更新正式资料、搜索文档、outbox 与审计记录</p></div></div>
          {approved ? <PublishQueue initialTasks={approved.items} /> : <div className="service-warning">无法读取待发布队列。</div>}
        </section> : null}

        <section className="phase-note"><AlertTriangle size={18} /><div><strong>Release A 范围</strong><p>当前支持人工录入、审核和发布。自动采集、广告联盟与头像识别均未启用。</p></div></section>
      </main>
    </AdminShell>
  );
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB"];
  let size = value;
  let unit = -1;
  do {
    size /= 1024;
    unit += 1;
  } while (size >= 1024 && unit < units.length - 1);
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(size)} ${units[unit]}`;
}

function formatPercent(value: number, limit: number) {
  return new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1, style: "percent" }).format(value / limit);
}

function formatAge(value: number | null | undefined) {
  if (value === null) return "未记录";
  if (value === undefined) return "-";
  const seconds = Math.max(0, Math.floor(value));
  if (seconds < 60) return "刚刚";
  if (seconds < 60 * 60) return `${Math.floor(seconds / 60)} 分钟`;
  if (seconds < 24 * 60 * 60) return `${Math.floor(seconds / (60 * 60))} 小时`;
  return `${Math.floor(seconds / (24 * 60 * 60))} 天`;
}

function mediaLevel(value: number | undefined) {
  if (value === undefined) return "blue";
  const ratio = value / mediaStorageLimitBytes;
  if (ratio >= 0.95) return "red";
  if (ratio >= 0.7) return "amber";
  return "green";
}

function mediaGuidance(value: number) {
  const ratio = value / mediaStorageLimitBytes;
  if (ratio >= 1) return "已满额，停止新图片上传";
  if (ratio >= 0.95) return "仅处理关键替换与释放空间";
  if (ratio >= 0.85) return "暂停非必要重处理和大图导入";
  if (ratio >= 0.7) return "检查孤儿与重复对象";
  return "容量正常";
}

function ageLevel(value: number | null | undefined, limit: number) {
  if (value === null) return "red";
  if (value === undefined) return "blue";
  return value > limit ? "red" : "green";
}
