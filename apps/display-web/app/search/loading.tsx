import { Search } from "lucide-react";

export default function SearchLoading() {
  return (
    <main>
      <section className="page-band">
        <div className="shell page-band__inner">
          <div><p className="eyebrow">作品 · 人物 · 厂牌</p><h1>资料搜索</h1></div>
          <p className="page-band__copy">正在读取公开资料，请稍候。</p>
        </div>
      </section>
      <div className="shell">
        <section className="section-block" aria-live="polite" aria-busy="true">
          <div className="empty-state"><Search size={30} aria-hidden="true" /><strong>正在查询资料…</strong><span>正在读取对应类型的已发布资料。</span></div>
        </section>
      </div>
    </main>
  );
}
