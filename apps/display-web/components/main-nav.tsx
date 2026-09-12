"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/", label: "发现" },
  { href: "/latest", label: "最新发行" },
  { href: "/recent", label: "最近收录" },
  { href: "/popular", label: "热门浏览" },
  { href: "/performers", label: "人物" },
  { href: "/studios", label: "厂牌" },
] as const;

/** Keep the current section discoverable for keyboard and assistive-technology users. */
export function MainNav() {
  const pathname = usePathname() ?? "/";

  return (
    <nav className="main-nav" aria-label="主导航">
      {links.map(({ href, label }) => {
        const active = href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);
        return (
          <Link aria-current={active ? "page" : undefined} className={active ? "active" : undefined} href={href} key={href}>
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
