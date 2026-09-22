import type { CollectorReviewBatch, CollectorReviewCandidate } from "@self-deepsearch/api-contracts";
import { ArrowDown, ArrowRight, CheckCheck, Database, Search } from "lucide-react";
import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { getCollectorBatches, getCollectorCandidates, getOperator } from "../../lib/api";
import { candidateStatusLabel } from "../../lib/collector-labels";
import { formatDateTime } from "../../lib/date-time";
import { candidateStatuses, collectorCandidateHref, collectorListHref, readCollectorSearch, type CollectorSearch, type CollectorSearchInput } from "./navigation";
import styles from "./collector.module.css";

export default async function CollectorPage({ searchParams }: { searchParams: Promise<CollectorSearchInput> }) {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const search = readCollectorSearch(await searchParams);
  const query = new URLSearchParams({ limit: "25" });
  for (const key of ["q", "status", "batch_id", "offset"] as const) if (search[key]) query.set(key, search[key]!);
  const [batches, candidates] = await Promise.all([
    getCollectorBatches(new URLSearchParams({ limit: "10", offset: search.batch_offset ?? "0" }).toString()),
    getCollectorCandidates(query.toString()),
  ]);
  const filtered = Boolean(search.q || search.status || search.batch_id);
  const offset = Number(search.offset ?? 0);
  return <AdminShell active="采集验收" user={operator}><main className={`main-content ${styles.main}`}>
    <header className={styles.pageHeader}>
      <div><p className={styles.eyebrow}>资料接收 · 来源核对</p><h1>采集验收</h1><p className={styles.intro}>从人物候选开始，核对资料与来源证据。</p></div>
      <Link className={styles.secondaryButton} href={collectorListHref(search, {}, "batches")}><Database size={16} aria-hidden="true" />接收批次<ArrowDown size={15} aria-hidden="true" /></Link>
    </header>
    <div className={styles.overview} aria-label="采集验收概况">
      <div><span>{filtered ? "筛选结果" : "人物候选"}</span><strong>{candidates ? candidates.total.toLocaleString("zh-CN") : "暂不可用"}</strong><small>{filtered ? "符合当前条件" : "文字资料 · 暂存区"}</small></div>
      <div><span>接收批次</span><strong>{batches ? batches.total.toLocaleString("zh-CN") : "暂不可用"}</strong><small>查看输入、新增与重复</small></div>
      <div className={styles.boundarySummary}><CheckCheck size={20} aria-hidden="true" /><div><strong>仅接收与核对</strong><small>暂存资料尚未发布，头像尚未导入。</small></div></div>
    </div>
    <section id="candidates" className={styles.panel} aria-labelledby="candidates-title">
      <div className={styles.panelHeader}><div><h2 id="candidates-title">人物候选</h2><p>按姓名、别名或来源编号查找</p></div><span className={styles.scopeLabel}>来源文字资料</span></div>
      <form className={styles.filters} method="get" action="/collector#candidates">
        <label className={styles.searchField}><span>姓名、别名或来源编号</span><div><Search size={17} aria-hidden="true" /><input name="q" defaultValue={search.q ?? ""} maxLength={200} placeholder="输入姓名、别名或来源编号" /></div></label>
        <label><span id="candidate-status-label">候选状态</span><select name="status" aria-labelledby="candidate-status-label" defaultValue={search.status ?? ""}><option value="">全部状态</option>{candidateStatuses.map((status) => <option key={status} value={status}>{candidateStatusLabel(status)}</option>)}</select></label>
        {search.batch_id ? <input type="hidden" name="batch_id" value={search.batch_id} /> : null}
        {search.batch_offset ? <input type="hidden" name="batch_offset" value={search.batch_offset} /> : null}
        <button className={styles.primaryButton} type="submit">查询</button><Link className={styles.resetLink} href={collectorListHref({}, { batch_offset: search.batch_offset })}>重置</Link>
      </form>
      {search.batch_id ? <div className={styles.activeFilter}><span>当前批次 <code>{search.batch_id}</code></span><Link href={collectorListHref(search, { batch_id: undefined, offset: undefined })}>移除批次筛选</Link></div> : null}
      {candidates ? <>
        <div className={styles.resultSummary}><span>共 <strong>{candidates.total.toLocaleString("zh-CN")}</strong> 条</span><span>{candidates.items.length ? `第 ${offset + 1}–${offset + candidates.items.length} 条` : "当前没有匹配结果"}</span></div>
        {candidates.items.length ? <>
          <div className={styles.desktopTable}><table className={styles.table}><thead><tr><th scope="col">人物 / 别名</th><th scope="col">来源 / 来源编号</th><th scope="col">状态</th><th scope="col">采集时间</th><th scope="col"><span className="sr-only">操作</span></th></tr></thead><tbody>{candidates.items.map((item) => <tr key={item.candidate_id}><td><strong className={styles.personName}>{item.name}</strong><small>{item.aliases.join("、") || "无别名"}</small></td><td>{item.source_name}<small>{item.external_id}</small></td><td><CandidateBadge status={item.candidate_status} /><small>公开状态：{publicationLabel(item.publication_status)}</small></td><td><time dateTime={item.checked_at}>{formatDateTime(item.checked_at)}</time></td><td><Link className={styles.rowLink} href={collectorCandidateHref(item.candidate_id, search)}>核对资料<ArrowRight size={15} aria-hidden="true" /></Link></td></tr>)}</tbody></table></div>
          <div className={styles.mobileCards}>{candidates.items.map((item) => <CandidateCard key={item.candidate_id} item={item} search={search} />)}</div>
        </> : <div className={styles.emptyState}><Search size={28} aria-hidden="true" /><h3>没有找到候选资料</h3><p>{filtered ? "尝试其他姓名或别名，或清除筛选条件。" : "接收到人物批次后，候选资料将显示在这里。"}</p>{filtered ? <Link className={styles.secondaryButton} href={collectorListHref()}>清除筛选</Link> : null}</div>}
        <nav className={styles.pagination} aria-label="候选分页"><span>每页 25 条</span><div>{offset > 0 ? <Link className={styles.secondaryButton} href={collectorListHref(search, { offset: String(Math.max(0, offset - 25)) })}>上一页</Link> : null}{candidates.next_offset !== null ? <Link className={styles.secondaryButton} href={collectorListHref(search, { offset: String(candidates.next_offset) })}>下一页</Link> : <span>已到最后一页</span>}</div></nav>
      </> : <div className={styles.errorState} role="alert"><h3>暂时无法读取人物候选</h3><p>已保留当前筛选，请稍后重试。</p><RetryForm search={search} section="candidates" /></div>}
    </section>
    <section id="batches" className={styles.panel} aria-labelledby="batches-title">
      <div className={styles.panelHeader}><div><h2 id="batches-title">接收批次</h2><p>已接收表示传输和入库成功；重复观察保留在首次接收的批次中。</p></div>{batches ? <span className={styles.scopeLabel}>{batches.total} 个批次</span> : null}</div>
      {batches ? <>{batches.items.length ? <>
        <div className={styles.desktopTable}><table className={styles.table}><thead><tr><th scope="col">来源 / 批次</th><th scope="col">接收时间</th><th scope="col">状态</th><th scope="col">输入 / 新增 / 重复</th><th scope="col">候选 / 待审核</th><th scope="col"><span className="sr-only">操作</span></th></tr></thead><tbody>{batches.items.map((batch) => <tr key={batch.batch_id}><td><strong>{batch.source_name}</strong><small><code>{batch.batch_id}</code></small></td><td><time dateTime={batch.received_at}>{formatDateTime(batch.received_at)}</time></td><td><span className={`${styles.badge} ${styles.neutral}`}>{batch.status === "accepted" ? "已接收" : batch.status}</span></td><td>{batch.input_count} / {batch.inserted_count} / {batch.duplicate_count}</td><td>{batch.candidate_count} / {batch.pending_count}</td><td><Link className={styles.rowLink} href={collectorListHref(search, { batch_id: batch.batch_id, offset: undefined })}>查看候选<ArrowRight size={15} aria-hidden="true" /></Link></td></tr>)}</tbody></table></div>
        <div className={styles.mobileCards}>{batches.items.map((batch) => <BatchCard key={batch.batch_id} batch={batch} search={search} />)}</div>
      </> : <div className={styles.emptyState}><Database size={28} aria-hidden="true" /><h3>尚未接收到人物批次</h3><p>批次接收后可在此核对数量与传输状态。</p></div>}
      <nav className={styles.pagination} aria-label="批次分页"><span>每页 10 个批次</span><div>{Number(search.batch_offset ?? "0") > 0 ? <Link className={styles.secondaryButton} href={collectorListHref(search, { batch_offset: String(Math.max(0, Number(search.batch_offset) - 10)) }, "batches")}>上一批</Link> : null}{batches.next_offset !== null ? <Link className={styles.secondaryButton} href={collectorListHref(search, { batch_offset: String(batches.next_offset) }, "batches")}>下一批</Link> : <span>已到最后一批</span>}</div></nav></> : <div className={styles.errorState} role="alert"><h3>暂时无法读取接收批次</h3><RetryForm search={search} section="batches" /></div>}
    </section>
    <details className={styles.scopeNote}><summary>了解暂存资料与验收范围</summary><p>采集资料保存在暂存区。人物姓名、别名和来源信息尚待审核，成年状态尚未核验；本次接入仅包含文字资料，头像尚未导入。批次“已接收”表示传输和入库成功。正式人物映射、权利审核、成年核验、头像入库与发布仍需后续流程。</p></details>
  </main></AdminShell>;
}

function publicationLabel(status: string) { return status === "staging" ? "暂存" : status; }

function RetryForm({ search, section }: { search: CollectorSearch; section: "candidates" | "batches" }) {
  return <form method="get" action={`/collector#${section}`}>{Object.entries(readCollectorSearch(search)).map(([name, value]) => <input key={name} type="hidden" name={name} value={value} />)}<button className={styles.secondaryButton} type="submit">重新读取</button></form>;
}

function CandidateBadge({ status }: { status: string }) {
  const tone = status === "rejected" || status === "invalid" || status === "conflict" ? styles.negative : status === "accepted" || status === "valid" ? styles.positive : status === "duplicate" ? styles.neutral : styles.pending;
  return <span className={`${styles.badge} ${tone}`}>{candidateStatusLabel(status)}</span>;
}

function CandidateCard({ item, search }: { item: CollectorReviewCandidate; search: CollectorSearch }) {
  return <article className={styles.candidateCard}><header><h3>{item.name}</h3><CandidateBadge status={item.candidate_status} /></header><p className={styles.aliases}>{item.aliases.join("、") || "无别名"}</p><dl><div><dt>来源</dt><dd>{item.source_name}</dd></div><div><dt>来源编号</dt><dd>{item.external_id}</dd></div><div><dt>采集时间</dt><dd><time dateTime={item.checked_at}>{formatDateTime(item.checked_at)}</time></dd></div></dl><footer><span>公开状态：{publicationLabel(item.publication_status)}</span><Link className={styles.rowLink} href={collectorCandidateHref(item.candidate_id, search)}>核对资料<ArrowRight size={16} aria-hidden="true" /></Link></footer></article>;
}

function BatchCard({ batch, search }: { batch: CollectorReviewBatch; search: CollectorSearch }) {
  return <article className={styles.batchCard}><header><h3>{batch.source_name}</h3><span className={`${styles.badge} ${styles.neutral}`}>{batch.status === "accepted" ? "已接收" : batch.status}</span></header><p className={styles.batchID}><code>{batch.batch_id}</code></p><dl className={styles.batchCounts}><div><dt>输入</dt><dd>{batch.input_count}</dd></div><div><dt>新增</dt><dd>{batch.inserted_count}</dd></div><div><dt>重复</dt><dd>{batch.duplicate_count}</dd></div><div><dt>候选</dt><dd>{batch.candidate_count}</dd></div><div><dt>待审核</dt><dd>{batch.pending_count}</dd></div></dl><footer><time dateTime={batch.received_at}>{formatDateTime(batch.received_at)}</time><Link className={styles.rowLink} href={collectorListHref(search, { batch_id: batch.batch_id, offset: undefined })}>查看候选<ArrowRight size={16} aria-hidden="true" /></Link></footer></article>;
}
