"use client";

import { useEffect, useRef } from "react";
import { apiMutation } from "../lib/auth-client";

type PageViewContentType = "work" | "performer" | "studio";

export function PageViewReporter({ contentType, contentID }: { contentType: PageViewContentType; contentID: string }) {
  // Keep one report per content entry for this mounted route instance. React
  // Strict Mode replays effects on the same instance, so the ref is enough to
  // suppress that duplicate without using a time window that could swallow a
  // real quick return to the same detail page.
  const reportedKey = useRef<string | null>(null);
  useEffect(() => {
    const key = `${contentType}:${contentID}`;
    if (reportedKey.current === key) return;
    reportedKey.current = key;
    void apiMutation<void>("/api/v1/metrics/page-view", "POST", { content_type: contentType, content_id: contentID }).catch(() => undefined);
  }, [contentID, contentType]);
  return null;
}
