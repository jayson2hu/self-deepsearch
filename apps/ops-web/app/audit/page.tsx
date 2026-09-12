import { Search } from "lucide-react";
import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { formatDateTime } from "../../lib/date-time";
import { getAuditLogs, getOperator } from "../../lib/api";

const requestIDPattern = /^[A-Za-z0-9._:-]{1,128}$/;

export default async function AuditPage({ searchParams }: { searchParams: Promise<{ request_id?: string }> }) {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  if (operator.role === "editor") redirect("/");

  const parameters = await searchParams;
  const requestID = parameters.request_id?.trim() ?? "";
  const valid = requestID === "" || requestIDPattern.test(requestID);
  const timeline = requestID && valid ? await getAuditLogs(requestID) : null;

  return <AdminShell active="审计查询" user={operator}><main className="main-content narrow-content">
    <div className="page-heading"><div><p>使用 API 响应中的 X-Request-ID 关联运营操作</p><h1>审计查询</h1></div></div>
    <section className="form-surface audit-search">
      <form action="/audit" className="entity-form" method="get" role="search">
        <label><span>请求编号</span><input autoComplete="off" defaultValue={requestID} maxLength={128} name="request_id" pattern="[A-Za-z0-9._:-]+" placeholder="例如 release-a:publish-42" required /></label>
        <div className="form-actions"><button className="primary" type="submit"><Search aria-hidden="true" size={17} />查询审计链路</button></div>
      </form>
      <p className="audit-privacy-note">结果只展示操作者、动作、对象、理由和精确时间，不返回 before/after 原始载荷或 metadata。</p>
    </section>

    {!valid ? <p className="form-error dashboard-section">请求编号只能包含字母、数字、点、下划线、冒号和连字符，最长 128 个字符。</p> : null}
    {requestID && valid && !timeline ? <div className="service-warning dashboard-section">无法读取审计记录，请稍后重试。</div> : null}
    {timeline ? <section className="review-section dashboard-section" aria-labelledby="audit-results-title">
      <div className="section-heading"><div><h2 id="audit-results-title">审计时间线</h2><p><code>{requestID}</code> · 按发生时间排列</p></div></div>
      {timeline.items.length ? <ol className="audit-timeline">
        {timeline.items.map((item) => <li key={item.id}><article>
          <header><strong>{item.action}</strong><time dateTime={item.occurred_at}>{formatDateTime(item.occurred_at)}</time></header>
          <dl>
            <div><dt>操作者</dt><dd>{item.actor ?? `${item.actor_type}${item.actor_id ? ` · ${item.actor_id}` : ""}`}</dd></div>
            <div><dt>对象</dt><dd>{item.object_type}{item.object_id ? ` · ${item.object_id}` : ""}</dd></div>
            <div><dt>理由</dt><dd>{item.reason ?? "未填写"}</dd></div>
            <div><dt>审计序号</dt><dd>{item.id}</dd></div>
          </dl>
        </article></li>)}
      </ol> : <div className="empty-state">没有找到该请求编号对应的审计记录。</div>}
      {timeline.truncated ? <div className="phase-note"><div><strong>结果已截断</strong><p>当前仅展示前 200 条；请联系数据库负责人进行受控导出。</p></div></div> : null}
    </section> : null}
  </main></AdminShell>;
}
