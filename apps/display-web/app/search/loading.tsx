import { Search } from "lucide-react";

export default function SearchLoading() {
  return (
    <main>
      <section className="page-band">
        <div className="shell page-band__inner">
          <p className="eyebrow">作品资料查询</p>
          <h1>资料搜索</h1>
        </div>
      </section>
      <div className="shell">
        <section className="section-block" aria-live="polite" aria-busy="true">
          <div className="empty-state"><Search size={30} /><strong>正在查询作品资料…</strong><span>正在保留当前查询条件并读取已发布作品。</span></div>
        </section>
      </div>
    </main>
  );
}
