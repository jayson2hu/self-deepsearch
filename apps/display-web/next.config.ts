import type { NextConfig } from "next";
import path from "node:path";
import { mediaContentSecurityPolicy } from "./lib/media-policy";

const nextConfig: NextConfig = {
  agentRules: false,
  output: "standalone",
  outputFileTracingRoot: path.join(__dirname, "../.."),
  poweredByHeader: false,
  transpilePackages: ["@self-deepsearch/api-contracts"],
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
};

const securityHeaders = [
  {
    key: "Content-Security-Policy",
    value: mediaContentSecurityPolicy(),
  },
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
];

export default nextConfig;
