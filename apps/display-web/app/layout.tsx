import type { Metadata } from "next";
import type { ReactNode } from "react";
import { SiteFooter } from "../components/site-footer";
import { SiteHeader } from "../components/site-header";
import "./globals.css";

export const metadata: Metadata = {
  title: { default: "幕鉴 · 作品与人物资料索引", template: "%s · 幕鉴" },
  description: "作品番号、发行资料和人物公开资料索引。仅提供资料展示，不提供资源。",
  openGraph: { siteName: "幕鉴", type: "website", locale: "zh_CN" },
};

export default function RootLayout({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <html lang="zh-CN">
      <body>
        <SiteHeader />
        {children}
        <SiteFooter />
      </body>
    </html>
  );
}
