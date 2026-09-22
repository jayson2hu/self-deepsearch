import type { Metadata } from "next";
import { cookies } from "next/headers";
import type { ReactNode } from "react";
import { SiteFooter } from "../components/site-footer";
import { SiteHeader } from "../components/site-header";
import {VisitorChoice} from "../components/visitor-choice";
import { siteURL } from "../lib/site";
import "./globals.css";

export const dynamic = "force-dynamic";

export function generateMetadata(): Metadata {
  return {
    other:{"mujian-visitor-metrics":process.env.VISITOR_METRICS_ENABLED==="true"?"enabled":"disabled"},
    metadataBase: siteURL(),
    title: { default: "幕鉴 · 作品与人物资料索引", template: "%s · 幕鉴" },
    description: "作品番号、发行资料和人物公开资料索引。仅提供资料展示，不提供资源。",
    openGraph: { siteName: "幕鉴", type: "website", locale: "zh_CN" },
  };
}

export default async function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  const preference = (await cookies()).get("mujian_theme")?.value;
  const theme = preference === "light" || preference === "dark" ? preference : undefined;
  return (
    <html data-theme={theme} lang="zh-CN" suppressHydrationWarning>
      <body>
        <SiteHeader />
        {children}
        <SiteFooter />
        <VisitorChoice enabled={process.env.VISITOR_METRICS_ENABLED==="true"}/>
      </body>
    </html>
  );
}
