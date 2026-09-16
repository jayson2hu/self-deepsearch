import Link from "next/link";
import { siteURL } from "../lib/site";
import { JsonLD } from "./json-ld";

type BreadcrumbItem = { label: string; href?: string };

export function Breadcrumbs({ items }: { items: BreadcrumbItem[] }) {
  const base = siteURL();
  return <>
    <JsonLD data={{
      "@context": "https://schema.org",
      "@type": "BreadcrumbList",
      itemListElement: items.map((item, index) => ({
        "@type": "ListItem",
        position: index + 1,
        name: item.label,
        item: item.href ? new URL(item.href, base).toString() : undefined,
      })),
    }} />
    <nav className="breadcrumbs" aria-label="面包屑">
      <ol>{items.map((item, index) => <li key={`${item.label}-${index}`}>
        {item.href ? <Link href={item.href}>{item.label}</Link> : <span aria-current="page">{item.label}</span>}
      </li>)}</ol>
    </nav>
  </>;
}
