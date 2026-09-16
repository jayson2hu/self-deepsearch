#!/usr/bin/env node

import { promises as fs } from "node:fs";
import {
  ensureArtifacts,
  findFreePort,
  prepareStandaloneAssets,
  REPOSITORY_ROOT,
  startChild,
  startMockUpstream,
  stopChild,
  waitForHTTP,
} from "./release_a_frontend_smoke.mjs";

const syntheticCreative = {
  slot: "banner:正文后横幅",
  placement: "banner",
  title: "DEV-05 合成广告",
  sponsor: "自动化夹具",
  image_url: "/ads/dev05-synthetic.svg",
  image_alt: "DEV-05 合成广告横幅",
  target_url: "https://offer.ads.example/dev05",
  width: 960,
  height: 90,
  starts_at: "2026-01-01T00:00:00Z",
  ends_at: "2027-01-01T00:00:00Z",
};

async function main() {
  const paths = await ensureArtifacts({ repositoryRoot: REPOSITORY_ROOT });
  const staged = await prepareStandaloneAssets(paths.display);
  const ports = new Set();
  const displayPort = await findFreePort({ avoid: ports });
  ports.add(displayPort);
  const apiPort = await findFreePort({ avoid: ports });
  let upstream;
  let display;
  try {
    upstream = await startMockUpstream(apiPort, { accountMode: "stateful" });
    const origin = `http://127.0.0.1:${displayPort}`;
    display = startChild({
      name: "display-web-dev05-ad-smoke",
      server: paths.display.server,
      port: displayPort,
      apiURL: `http://127.0.0.1:${apiPort}`,
      repositoryRoot: REPOSITORY_ROOT,
      extraEnv: {
        SITE_URL: origin,
        NEXT_PUBLIC_SITE_URL: origin,
        DISPLAY_ADS_ENABLED: "true",
        DISPLAY_AD_SLOTS_JSON: JSON.stringify([syntheticCreative]),
        DISPLAY_AD_ASSET_HOSTS: "",
        DISPLAY_AD_TARGET_HOSTS: "offer.ads.example",
      },
    });
    const anonymous = await waitForHTTP(display, "/", 30_000);
    const login = await fetch(`${origin}/api/v1/auth/login`, {
      method: "POST",
      headers: { "content-type": "application/json", origin },
      body: JSON.stringify({ email: "browser-user@example.test", password: "Release-A-browser-pass" }),
    });
    if (login.status !== 200) throw new Error(`login failed: ${login.status}`);
    const cookie = login.headers.get("set-cookie")?.split(";", 1)[0];
    if (!cookie) throw new Error("login did not return a session cookie");
    const authenticatedResponse = await fetch(`${origin}/`, { headers: { cookie } });
    const authenticated = await authenticatedResponse.text();
    for (const [name, body] of [["anonymous", anonymous.body], ["authenticated", authenticated]]) {
      if (!body.includes('data-ad-state="ready"') || !body.includes("DEV-05 合成广告") || !body.includes("noopener noreferrer sponsored")) {
        throw new Error(`${name} response did not render the reviewed ready creative`);
      }
      if (body.includes('data-ad-state="preview"') || body.includes("adsbygoogle") || body.includes("<script src=\"https://offer.ads.example")) {
        throw new Error(`${name} response crossed the preview or third-party script boundary`);
      }
    }
    const count = (body) => body.split('data-ad-state="ready"').length - 1;
    if (count(anonymous.body) !== count(authenticated)) throw new Error("ad visibility differs by authentication state");
    process.stdout.write(`${JSON.stringify({ status: "passed", readySlots: count(anonymous.body), anonymous: true, authenticated: true }, null, 2)}\n`);
  } finally {
    if (display) await stopChild(display);
    if (upstream) {
      upstream.closeAllConnections?.();
      await new Promise((resolve) => upstream.close(() => resolve()));
    }
    await Promise.all(staged.map((target) => fs.rm(target, { recursive: true, force: true })));
  }
}

main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
  process.exitCode = 1;
});
