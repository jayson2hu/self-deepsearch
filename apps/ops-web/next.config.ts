import type { NextConfig } from "next";
import path from "node:path";

const nextConfig: NextConfig = {
  agentRules: false,
  // Detached builds can produce an empty TypeScript --showConfig stream.
  // Use the compiler API so production builds still perform type checking.
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
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "no-referrer" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
];

export default nextConfig;
