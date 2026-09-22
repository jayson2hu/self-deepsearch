import type { Metadata } from "next";
import { configuredRightsEmail } from "../../lib/public-contact";
import {VisitorChoice} from "../../components/visitor-choice";

export const metadata: Metadata = {
  title: "数据与隐私说明",
  description: "幕鉴当前账号、浏览历史、清除、归档和关闭账号行为说明。",
  robots: { index: false, follow: true },
};
export const dynamic = "force-dynamic";

export default function PrivacyPage() {
  const rightsEmail = configuredRightsEmail();
  return <main className="shell information-page">
    <header><p className="eyebrow">当前实际行为</p><h1>数据与隐私说明</h1><p>本页说明 Release A 当前已经实现的行为，不承诺尚未开发的历史开关、统一保留期限或永久删除能力。</p></header>
    <section><h2>不登录浏览</h2><p>公开目录和详情可匿名访问。详情浏览会按资料和小时累计公开内容的浏览次数；匿名浏览不会写入某个账号的个人浏览历史。</p></section>
    <section id="visitor-metrics"><h2>独立访客统计（可选）</h2><p>仅在你允许后，浏览器才为后续详情访问生成第一方随机标识，放在本站本地存储，最长使用 30 天；不使用 IP、设备指纹，不连接第三方分析服务。未选择、拒绝、GPC/DNT 开启或存储不可用时不发送标识，但原有匿名浏览次数仍会累计。</p><p>服务端仅记录标识的 SHA-256、上海日期和当日次数，不在该表保存账号或所看资料。保留最近 30 个自然日，每小时清理过期行，可能有最多一小时清理延迟。UV 只代表同意统计的浏览器数量，不是所有访客人数；清空存储、换浏览器或标识到期会影响去重。不能用今天的 UV 回填过去。</p><p>关闭后停止未来 UV 并清除本地标识，已有去标识记录到期清理；这不等于清除账号浏览历史。</p><VisitorChoice enabled={process.env.VISITOR_METRICS_ENABLED==="true"} inline/></section>
    <section><h2>注册、登录与安全</h2><p>注册和登录会处理邮箱、密码的安全派生凭据、邮箱验证码、登录会话，以及限流和防滥用所需的安全信息。密码不会以明文保存。账号私有页面和关系接口需要有效登录，会话在退出、密码重置或关闭账号后撤销。</p></section>
    <section><h2>个人功能记录</h2><p>登录后使用收藏、关注、推荐偏好和反馈时，系统会保存对应关系、提交内容和处理状态。账号私有页不进入公开目录缓存；运营访问按账号角色限制。</p></section>
    <section><h2>浏览历史与“清除可见历史”</h2><p>当前默认行为是：登录用户打开作品、人物或厂牌详情并完成浏览上报时，会追加一条带时间的账号浏览历史。当前没有“停止未来记录”的用户开关。</p><p>“清除可见历史”只把现有记录标记为不再向该账号展示，不会物理删除底层记录。系统具备把较早的已清除记录写入受控归档的能力，但部署默认关闭；即使启用，当前也不会删除数据库原行。</p></section>
    <section><h2>关闭账号</h2><p>关闭账号需要邮箱验证码和明确确认。完成后账号状态变为关闭、现有会话被撤销，无法继续登录，原收藏、关注、推荐偏好和历史不再通过账号页面展示。</p><p>关闭账号不等于全量物理删除。账号、关闭事件、反馈、安全和审计相关记录，以及已经标记清除或可能归档的历史，仍可能继续保存。</p></section>
    <section><h2>保留期限与恢复边界</h2><p>当前产品尚未实施适用于全部账号数据的统一有限保留期限，也没有对用户提供“永久删除全部数据”的按钮。备份、归档和恢复不能把已经清除的记录重新显示为可见历史；未来如改变期限或提供新的删除能力，应先更新本说明并完成对应数据和恢复流程。</p></section>
    <section><h2>你的现有选择</h2><ul><li>在历史页清除可见历史；</li><li>在推荐偏好页恢复或减少作品推荐；</li><li>在账号设置中关闭账号；</li><li>通过作品页提交纠错，或使用公开联系渠道提出权利请求。</li></ul>{rightsEmail ? <p>联系邮箱：<a href={`mailto:${rightsEmail}`}>{rightsEmail}</a></p> : <p className="information-warning" role="status">本环境尚未配置公开联系邮箱，不应据此开放真实注册或对外发布。</p>}</section>
    <footer><p>说明版本：2026-09-19；更新时间：2026-09-19</p></footer>
  </main>;
}
