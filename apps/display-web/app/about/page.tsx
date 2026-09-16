import type { Metadata } from "next";
import Link from "next/link";
import { configuredRightsEmail } from "../../lib/public-contact";

export const metadata: Metadata = {
  title: "关于幕鉴",
  description: "了解幕鉴公开资料索引的内容边界、使用方式、纠错和权利联系渠道。",
};
export const dynamic = "force-dynamic";

export default function AboutPage() {
  const rightsEmail = configuredRightsEmail();
  return <main className="shell information-page">
    <header><p className="eyebrow">公开说明</p><h1>关于幕鉴</h1><p>幕鉴是面向 18 岁以上用户的作品、人物与厂牌公开资料索引。</p></header>
    <section><h2>本站提供什么</h2><p>你可以按番号、标题、人物或厂牌名称查询已经公开的资料，并沿作品、人物和厂牌之间的关系继续浏览。资料可能存在缺失、误差或更新延迟，页面上的未知字段会保持未知，不以推测补齐。</p></section>
    <section><h2>本站不提供什么</h2><p>本站不提供、存储或跳转到视频、音频、磁力、种子、网盘、下载或播放资源。广告如以后启用，也不会被表述为资料来源、制作方授权或本站对广告主的背书。</p></section>
    <section><h2>纠错与权利联系</h2><p>登录用户可以从作品页提交资料纠错。权利下架或无需登录的联系请求可使用公开邮箱。</p>{rightsEmail ? <p><a href={`mailto:${rightsEmail}`}>{rightsEmail}</a></p> : <p className="information-warning" role="status">本环境尚未配置公开联系邮箱，不应据此开放真实注册或对外发布。</p>}</section>
    <section><h2>账号与数据</h2><p>收藏、关注、推荐偏好、浏览历史和反馈属于登录后的个人功能。具体记录、清除和关闭账号行为见<Link href="/privacy">数据与隐私说明</Link>。</p></section>
    <footer><p>说明更新时间：2026-09-16</p></footer>
  </main>;
}
