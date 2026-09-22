import { ArrowLeft, ExternalLink, FileText, Fingerprint } from "lucide-react";
import Link from "next/link";
import { notFound, redirect } from "next/navigation";
import { AdminShell } from "../../../components/admin-shell";
import { getCollectorCandidate, getOperator } from "../../../lib/api";
import { candidateStatusLabel } from "../../../lib/collector-labels";
import { formatDateTime } from "../../../lib/date-time";
import { collectorListHref, readCollectorSearch, type CollectorSearchInput } from "../navigation";
import styles from "../collector.module.css";

export default async function CollectorCandidatePage({ params, searchParams }: {
  params: Promise<{ candidateID: string }>;
  searchParams: Promise<CollectorSearchInput>;
}) {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const { candidateID } = await params;
  const search = readCollectorSearch(await searchParams);
  const { item, missing } = await getCollectorCandidate(candidateID);
  if (missing) notFound();
  const candidateTone = item && ["invalid", "rejected", "conflict"].includes(item.candidate_status) ? styles.negative : item && ["accepted", "valid"].includes(item.candidate_status) ? styles.positive : styles.pending;
  return <AdminShell active="采集验收" user={operator}><main className={`main-content ${styles.main} ${styles.detailMain}`}>
    <Link className={styles.backLink} href={collectorListHref(search)}><ArrowLeft size={16} aria-hidden="true" />返回候选列表</Link>
    <header className={styles.pageHeader}><div><p className={styles.eyebrow}>采集验收 · 人物候选</p><h1>{item?.name ?? "人物候选"}</h1>{item ? <p className={styles.intro}>{item.source_name} <span aria-hidden="true">·</span> {item.external_id}</p> : null}</div>{item ? <span className={`${styles.badge} ${candidateTone}`}>{candidateStatusLabel(item.candidate_status)}</span> : null}</header>
    {item ? <>
      <dl className={styles.statusOverview} aria-label="候选资料状态">
        <div><dt>候选状态</dt><dd>{candidateStatusLabel(item.candidate_status)}</dd></div>
        <div><dt>权利状态</dt><dd>{item.rights_status === "needs_review" ? "待审核" : item.rights_status}</dd></div>
        <div><dt>成年状态</dt><dd>{item.adult_status === "unknown" ? "尚未核验" : item.adult_status}</dd></div>
        <div><dt>公开状态</dt><dd>{item.publication_status === "staging" ? "暂存" : item.publication_status}</dd></div>
      </dl>
      <section className={styles.panel} aria-labelledby="source-profile-title">
        <div className={styles.panelHeader}><div><h2 id="source-profile-title"><FileText size={19} aria-hidden="true" />来源资料</h2><p>逐项核对文字信息和原始来源</p></div><span className={styles.scopeLabel}>文字资料</span></div>
        <dl className={styles.facts}>
          <div><dt>姓名</dt><dd className={styles.detailName}>{item.name}</dd></div><div><dt>别名</dt><dd>{item.aliases.length ? <ul className={styles.aliasList}>{item.aliases.map((alias, index) => <li key={index}>{alias}</li>)}</ul> : "无别名"}</dd></div>
          <div><dt>来源</dt><dd>{item.source_name}</dd></div><div><dt>来源编号</dt><dd>{item.external_id}</dd></div>
          <div className={styles.wideFact}><dt>来源页面</dt><dd>{/^https?:\/\//i.test(item.profile_url) ? <a className={styles.sourceLink} href={item.profile_url} target="_blank" rel="noopener noreferrer">{item.profile_url}<ExternalLink size={15} aria-hidden="true" /><span className="sr-only">（新窗口打开）</span></a> : item.profile_url}</dd></div>
          <div><dt>采集时间</dt><dd><time dateTime={item.checked_at}>{formatDateTime(item.checked_at)}</time></dd></div><div><dt>平台接收时间</dt><dd><time dateTime={item.received_at}>{formatDateTime(item.received_at)}</time></dd></div>
          <div className={styles.wideFact}><dt>头像</dt><dd>本次文字资料接入未包含头像</dd></div>
        </dl>
      </section>
      <details className={`${styles.panel} ${styles.evidence}`}>
        <summary><span><Fingerprint size={19} aria-hidden="true" /><strong>技术证据</strong></span><span>批次、记录编号与内容摘要</span></summary>
        <dl className={styles.facts}>
          <div><dt>采集器版本</dt><dd>{item.connector_version}</dd></div><div><dt>批次编号</dt><dd><code>{item.batch_id}</code><Link className={styles.factLink} href={collectorListHref(search, { batch_id: item.batch_id, offset: undefined })}>查看此批次候选</Link></dd></div>
          <div><dt>候选编号</dt><dd><code>{item.candidate_id}</code></dd></div><div><dt>来源记录编号</dt><dd><code>{item.source_record_id}</code></dd></div>
          <div><dt>来源标识</dt><dd><code>{item.source_id}</code></dd></div><div><dt>上游内容摘要</dt><dd><code>{item.source_content_hash}</code></dd></div>
          <div className={styles.wideFact}><dt>平台内容摘要</dt><dd><code>{item.content_hash}</code></dd></div>
        </dl>
      </details>
      <aside className={styles.detailNotice}><strong>当前资料仅用于采集验收和核对</strong><p>正式人物映射、权利审核、成年核验、头像入库与发布仍需后续流程。</p></aside>
    </> : <div className={styles.errorState} role="alert"><h2>暂时无法读取人物候选</h2><p>当前筛选已保留，可返回列表后重试。</p><Link className={styles.secondaryButton} href={collectorListHref(search)}>返回列表重试</Link></div>}
  </main></AdminShell>;
}
