import type { NextConfig } from "next";
import path from "node:path";
import { mediaContentSecurityPolicy } from "./lib/media-policy";

const nextConfig: NextConfig = {
  agentRules: false,
  // The CLI capture path can return an empty --showConfig stream in detached
  // build environments. The compiler API performs the same type check without
  // skipping errors and keeps production builds reproducible here.
  experimental: { useTypeScriptCli: false },
  output: "standalone",
  outputFileTracingRoot: path.join(__dirname, "../.."),
  poweredByHeader: false,
  transpilePackages: ["@self-deepsearch/api-contracts", "@self-deepsearch/web-runtime"],
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
