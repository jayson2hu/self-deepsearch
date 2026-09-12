import { AdSlot } from "./ad-slot";

export function SiteFooter() {
  const rightsEmail = configuredRightsEmail();

  return (
    <footer className="site-footer">
      <div className="shell footer-ad"><AdSlot label="页脚展示位" placement="footer" /></div>
      <div className="shell footer-grid">
        <div><strong className="footer-brand">幕鉴</strong><p>作品与人物公开资料索引。</p></div>
        <div><strong>资料说明</strong><p>资料和图片来自经审核的公开来源，可能存在误差或更新延迟。</p></div>
        <div><strong>权利与纠错</strong><p>本站不提供视频、音频、下载、磁力、网盘或资源跳转。如有疑问、纠错或下架需求，{rightsEmail ? <>请联系 <a href={`mailto:${rightsEmail}`}>{rightsEmail}</a>。</> : <span className="footer-contact-warning">权利联系邮箱尚未配置。</span>}</p></div>
        <div><strong>广告说明</strong><p>未来展示的广告为独立广告位，不代表广告主与本站资料来源、人物或制作方存在隶属、授权、推荐或背书关系。</p></div>
      </div>
      <div className="shell footer-bottom"><span>仅限 18 岁以上用户</span><span>Release A · 测试环境</span></div>
    </footer>
  );
}

function configuredRightsEmail(): string | null {
  const value = process.env.RIGHTS_CONTACT_EMAIL?.trim();
  return value && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value) && !value.toLowerCase().endsWith(".invalid") ? value : null;
}
