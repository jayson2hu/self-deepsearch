import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { HideEntityButton } from "../../components/hide-entity-button";
import { MergeEntityButton } from "../../components/merge-entity-button";
import { getCatalogEntities, getOperator } from "../../lib/api";
import { formatDateTime } from "../../lib/date-time";

export default async function CatalogPage() {
  const operator = await getOperator();
  if (!operator) redirect("/login");
  const catalog = await getCatalogEntities();
  return <AdminShell active="资料目录" user={operator}><main className="main-content">
    <div className="page-heading"><div><p>草稿、审核、发布、隐藏和下架状态统一查看</p><h1>资料目录</h1></div><div className="heading-actions"><Link className="button" href="/studios/new">新建厂牌</Link><Link className="button" href="/performers/new">新建人物</Link><Link className="button primary" href="/works/new">新建作品</Link></div></div>
    <section className="review-section dashboard-section"><div className="section-heading"><div><h2>全部资料</h2><p>版本历史保留作者、审核人、理由和精确时间。</p></div></div>
      {catalog ? <div className="table-wrap"><table><thead><tr><th>类型</th><th>资料</th><th>状态</th><th>公开 slug</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
        {catalog.items.map((item) => <tr key={`${item.entity_type}:${item.id}`}><td><span className="type-badge">{typeLabel(item.entity_type)}</span></td><td><strong>{item.label}</strong><br /><code>{item.id}</code></td><td><span className={`status ${item.status === "published" ? "ok" : item.status === "takedown" ? "waiting" : "neutral"}`}>{statusLabel(item.status)}</span></td><td>{item.canonical_slug ?? "-"}</td><td><time dateTime={item.updated_at}>{formatDateTime(item.updated_at)}</time></td><td><div className="row-actions"><Link className="button compact-button" href={`/catalog/${item.entity_type}/${item.id}`}>查看版本</Link>{operator.role !== "editor" && item.status === "published" ? <><HideEntityButton entity={item} /><MergeEntityButton source={item} targets={catalog.items.filter((target) => target.entity_type === item.entity_type && target.id !== item.id && target.status === "published")} /></> : null}</div></td></tr>)}
        {!catalog.items.length ? <tr><td className="empty-state" colSpan={6}>暂无资料</td></tr> : null}
      </tbody></table></div> : <div className="service-warning">无法读取资料目录。</div>}
    </section>
  </main></AdminShell>;
}

function typeLabel(type: "work" | "performer" | "studio") { return ({ work: "作品", performer: "人物", studio: "厂牌" } as const)[type]; }
function statusLabel(status: string) { return ({ draft: "草稿", reviewing: "审核中", approved: "已通过", published: "已发布", hidden: "已隐藏", takedown: "已下架", merged: "已合并" } as Record<string, string>)[status] ?? status; }
