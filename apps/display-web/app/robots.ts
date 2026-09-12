import type { MetadataRoute } from "next";
import { siteURL } from "../lib/site";

// SITE_URL is injected by the runtime deployment, not the image build.
export const dynamic = "force-dynamic";

export default function robots(): MetadataRoute.Robots {
  const base = siteURL();
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: ["/search", "/account/", "/login", "/logout", "/register", "/forgot-password", "/invite", "/auth/"],
    },
    sitemap: new URL("/sitemap.xml", base).toString(),
  };
}
