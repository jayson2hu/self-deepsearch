"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const links = [
  { href: "/search", label: "查询", matches: ["/search"] },
  { href: "/latest", label: "浏览", matches: ["/latest", "/recent", "/popular", "/studios"] },
  { href: "/performers", label: "人物", matches: ["/performers"] },
  { href: "/account/favorites", label: "我的", matches: ["/account"] },
] as const;

/** Keep the current section discoverable for keyboard and assistive-technology users. */
export function MainNav() {
  const pathname = usePathname() ?? "/";

  return (
    <nav className="main-nav" aria-label="主导航">
      {links.map(({ href, label, matches }) => {
        const active = matches.some((prefix) => pathname === prefix || pathname.startsWith(`${prefix}/`));
        return (
          <Link aria-current={active ? "page" : undefined} className={active ? "active" : undefined} href={href} key={href}>
            {label}
          </Link>
        );
      })}
    </nav>
  );
}
