import type { UserSummary } from "@self-deepsearch/api-contracts";
import { BookOpen, FileCheck2, FileUp, Gauge, Gavel, GitCompareArrows, Image as ImageIcon, Library, LogOut, MessageSquare, PauseCircle, ScrollText, Settings2, ShieldCheck, Sparkles } from "lucide-react";
import Link from "next/link";
import type { ReactNode } from "react";

const navigation: Array<{ href: string; label: string; icon: typeof Gauge; roles?: UserSummary["role"][] }> = [
  { href: "/", label: "运营概览", icon: Gauge },
  { href: "/works/new", label: "新建作品", icon: BookOpen },
  { href: "/imports", label: "CSV 导入", icon: FileUp },
  { href: "/catalog", label: "资料目录", icon: Library },
  { href: "/editorial", label: "编辑推荐", icon: Sparkles },
  { href: "/settings", label: "首页配置", icon: Settings2 },
  { href: "/#review-title", label: "审核队列", icon: FileCheck2 },
  { href: "/conflicts", label: "字段冲突", icon: GitCompareArrows },
  { href: "/feedback", label: "反馈审核", icon: MessageSquare },
  { href: "/media", label: "图片资产", icon: ImageIcon },
  { href: "/media/usage", label: "用量复核", icon: Gauge, roles: ["admin", "owner"] },
  { href: "/media/upload-control", label: "上传控制", icon: PauseCircle, roles: ["admin", "owner"] },
  { href: "/rights", label: "权利下架", icon: Gavel },
  { href: "/audit", label: "审计查询", icon: ScrollText, roles: ["admin", "owner"] },
  { href: "/users", label: "用户与权限", icon: ShieldCheck },
];

export function AdminShell({ user, active, children }: { user: UserSummary; active: string; children: ReactNode }) {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Link className="sidebar__brand" href="/"><strong>幕鉴</strong><span>运营后台</span></Link>
        <nav aria-label="后台导航">
          {navigation.filter((item) => !item.roles || item.roles.includes(user.role)).map(({ href, label, icon: Icon }) => (
            <Link aria-current={active === label ? "page" : undefined} aria-label={label} className={active === label ? "active" : ""} href={href} key={label}>
              <Icon aria-hidden="true" size={17} /><span>{label}</span>
            </Link>
          ))}
        </nav>
        <div className="sidebar__foot">
          <span>Release A · 测试环境</span>
          <form action="/auth/logout" method="post"><button aria-label="退出登录" title="退出登录" type="submit"><LogOut size={17} /><span>退出</span></button></form>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <div className="environment"><i /> 测试环境</div>
          <div className="operator"><span>{user.email.slice(0, 1).toUpperCase()}</span><b>{user.email}</b><small>{roleLabel(user.role)}</small></div>
        </header>
        {children}
      </div>
    </div>
  );
}

function roleLabel(role: UserSummary["role"]) {
  return ({ owner: "所有者", admin: "管理员", editor: "编辑", user: "用户" } as const)[role];
}
