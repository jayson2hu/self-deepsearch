import Link from "next/link";
import { redirect } from "next/navigation";
import { AdminShell } from "../../components/admin-shell";
import { getOperator, getTrafficReport } from "../../lib/api";
import { formatDateTime } from "../../lib/date-time";

export const dynamic = "force-dynamic";
const ranges = [{value:"7d",label:"近 7 天"},{value:"30d",label:"近 30 天"},{value:"all",label:"历史累计"}] as const;
const number = (value: number) => new Intl.NumberFormat("zh-CN").format(value);

export default async function TrafficPage({searchParams}:{searchParams:Promise<{range?:string}>}) {
  const user=await getOperator();
  if(!user)redirect("/login");
  if(user.role!=="owner" && user.role!=="admin")redirect("/");
  const query=await searchParams;
  const range=query.range==="7d" || query.range==="all" ? query.range : "30d";
  const data=await getTrafficReport(range);
  return <AdminShell active="访问统计" user={user}><main className="main-content">
    <div className="page-heading"><div><p>详情访问 · 用户访问 · 热门排名</p><h1>访问统计</h1></div><div className="heading-actions">{ranges.map(item=><Link className={`button ${item.value===range?"primary":""}`} key={item.value} href={`/traffic?range=${item.value}`} aria-current={item.value===range?"page":undefined}>{item.label}</Link>)}</div></div>
    <section className="phase-note"><div><strong>进入详情才计一次，列表打开、搜索和翻页不计入</strong><p>PV 是访问次数；UV 是同意统计的第一方浏览器标识去重，不是自然人数，也不包括拒绝或未选择统计的访客。不同浏览器、清空存储或标识轮换会影响去重；不使用 IP 或设备指纹。</p><p>登录用户指标来自现有详情历史记录，排除管理账号、测试账号及已关闭账号；清除可见历史不会回减聚合次数。内部采集预览不计入正式流量。UV 仅保留近 30 个自然日，累计视图也不例外，不回填上线前数据。</p></div></section>
    {!data?<div className="service-warning dashboard-section" role="alert">访问统计暂时不可用，请稍后重试。读取失败不显示为 0。</div>:<>
      <p>更新于 <time dateTime={data.checked_at}>{formatDateTime(data.checked_at)}</time> · 上海时区自然日；今日与所选范围分别计算。</p>
      <p>访客采集：{data.visitor_collection_enabled?"已启用，尊重访客选择":"已关闭，以下为现存记录"} · UV 范围从 {data.uv_window_start??"暂无数据"} 开始。</p>
      <section className="metric-grid" aria-label="访问指标">{[["今日详情访问",number(data.today_page_views),"今日 00:00 至今"],["所选范围详情访问",number(data.page_views),"匿名与登录详情访问次数"],["登录用户访问次数",number(data.logged_in_user_views),"普通注册用户已记录的详情访问"],["访问过的注册用户",number(data.active_registered_users),"所选范围内去重账号数，不是 UV"],["同意统计的浏览器 UV",data.unique_visitors===null?"暂未统计":number(data.unique_visitors),"范围去重；累计视图仅近 30 天"],["今日浏览器 UV",number(data.unique_visitor_today??0),"今日同意统计的浏览器标识数"],["UV 已覆盖的详情次数",number(data.consented_page_views??0),"UV 范围内携带已同意标识的 PV，不代表所有访问"]].map(([title,value,note])=><article key={title}><div><small>{title}</small><strong>{value}</strong><p>{note}</p></div></article>)}</section>
      <section className="review-section dashboard-section"><div className="section-heading"><div><h2>热门内容排名</h2><p>所选范围详情浏览次数降序，最多 20 条；仅包含当前已发布内容，同次数按类型和 ID 稳定排序。</p></div></div><div className="table-wrap"><table><thead><tr><th>排名</th><th>类型</th><th>资料</th><th>访问次数</th></tr></thead><tbody>{data.hot_content.map(item=><tr key={`${item.entity_type}:${item.id}`}><td>#{item.rank}</td><td>{{work:"作品",performer:"人物",studio:"厂牌"}[item.entity_type]}</td><td>{item.title}<br/><small>{item.path}</small></td><td>{number(item.page_views)}</td></tr>)}{!data.hot_content.length?<tr><td colSpan={4} className="empty-state">暂无已发布内容的浏览数据，不使用模拟排名补位。</td></tr>:null}</tbody></table></div></section>
      <section className="review-section dashboard-section"><div className="section-heading"><div><h2>每日访问趋势</h2><p>{range==="all"?"累计范围下，趋势展示最近 30 个自然日。":"展示所选范围的自然日，未发生访问的日期显示 0。"}</p></div></div><div className="table-wrap"><table><thead><tr><th>日期</th><th>访问次数</th><th>相对趋势</th></tr></thead><tbody>{data.daily.map(day=><tr key={day.date}><td>{day.date}</td><td>{number(day.page_views)}</td><td><progress aria-label={`${day.date} 详情访问`} value={day.page_views} max={Math.max(1,...data.daily.map(item=>item.page_views))}/></td></tr>)}</tbody></table></div></section>
      <section className="review-section dashboard-section"><div className="section-heading"><div><h2>用户访问次数</h2><p>仅管理员可见；按所选范围访问次数展示前 50 个普通注册用户。不展示个人访问过哪些内容。</p></div></div><div className="table-wrap"><table><thead><tr><th>用户邮箱</th><th>访问次数</th></tr></thead><tbody>{data.users.map(item=><tr key={item.id}><td>{item.email}</td><td>{number(item.page_views)}</td></tr>)}{!data.users.length?<tr><td colSpan={2} className="empty-state">暂无普通注册用户的详情访问记录。</td></tr>:null}</tbody></table></div></section>
    </>}
  </main></AdminShell>;
}
