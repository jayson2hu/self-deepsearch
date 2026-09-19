import { Search } from "lucide-react";
import Link from "next/link";
import { AccountActions } from "./account-actions";
import { MainNav } from "./main-nav";
import { ThemeToggle } from "./theme-toggle";

export function SiteHeader() {
  return (
    <>
      <div className="notice-bar">
        <div className="shell notice-bar__inner">
          <span>18+ · 仅展示公开资料，不提供资源</span>
          <span className="status-dot"><i /> 测试环境</span>
        </div>
      </div>
      <header className="site-header">
        <div className="shell site-header__inner">
          <Link className="brand" href="/" aria-label="幕鉴首页">
            <strong>幕鉴</strong><span>资料索引</span>
          </Link>
          <MainNav />
          <Link className="header-search-link" href="/search" aria-label="打开搜索">
            <Search aria-hidden="true" size={19} /><span>搜索</span>
          </Link>
          <form className="header-search" action="/search" role="search">
            <label className="sr-only" htmlFor="header-query">页头搜索番号、标题、人物或厂牌</label>
            <input id="header-query" name="q" placeholder="番号、标题或人物艺名" />
            <button type="submit" aria-label="搜索"><Search size={18} /></button>
          </form>
          <ThemeToggle />
          <AccountActions />
        </div>
      </header>
    </>
  );
}
