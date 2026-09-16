import type { MetadataRoute } from "next";
import { seoReleaseEnabled } from "../lib/seo-policy";
import { siteURL } from "../lib/site";

// SITE_URL is injected by the runtime deployment, not the image build.
export const dynamic = "force-dynamic";

export default function robots(): MetadataRoute.Robots {
  if (!seoReleaseEnabled()) {
    return { rules: { userAgent: "*", disallow: "/" } };
  }
  const base = siteURL();
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: ["/api/", "/auth/"],
    },
    sitemap: new URL("/sitemap.xml", base).toString(),
  };
}
