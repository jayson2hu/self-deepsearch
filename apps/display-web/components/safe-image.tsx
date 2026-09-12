"use client";

import type { ImageRendition } from "@self-deepsearch/api-contracts";
import type { ComponentPropsWithoutRef } from "react";
import { useState } from "react";

type SafeImageProps = Omit<ComponentPropsWithoutRef<"img">, "src" | "srcSet" | "onError"> & {
  src: string;
  fallbackSrc: string;
  renditions?: readonly ImageRendition[];
};

/** Keep a fixed image box, use API-provided renditions, and fall back locally. */
export function SafeImage({ src, fallbackSrc, renditions = [], alt, sizes, loading = "lazy", decoding = "async", ...props }: SafeImageProps) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const fallback = normalizedImageSource(fallbackSrc) ?? fallbackSrc;
  const candidate = normalizedImageSource(src) ?? fallback;
  const source = failedSrc === candidate ? fallback : candidate;
  const srcSet = source === fallback ? undefined : renditionSrcSet(renditions);

  return (
    // API media URLs are already resized WebP objects; a native srcset preserves their exact widths.
    // eslint-disable-next-line @next/next/no-img-element
    <img
      {...props}
      ref={(element) => {
        // A cached SSR image may already have failed CSP before hydration.
        // onError alone cannot observe that earlier event.
        if (element?.complete && element.naturalWidth === 0 && source !== fallback) setFailedSrc(candidate);
      }}
      src={source}
      srcSet={srcSet}
      sizes={srcSet ? sizes : undefined}
      alt={alt}
      loading={loading}
      decoding={decoding}
      onError={() => {
        if (candidate !== fallback) setFailedSrc(candidate);
      }}
    />
  );
}

function renditionSrcSet(renditions: readonly ImageRendition[]): string | undefined {
  const sources = new Map<number, string>();
  for (const rendition of renditions) {
    const url = normalizedImageSource(rendition.url);
    if (url && Number.isInteger(rendition.width) && rendition.width > 0) {
      sources.set(rendition.width, `${url} ${rendition.width}w`);
    }
  }
  const values = [...sources.entries()]
    .sort(([left], [right]) => left - right)
    .map(([, value]) => value);
  return values.length > 0 ? values.join(", ") : undefined;
}

function normalizedImageSource(value: string): string | null {
  const trimmed = value.trim();
  if (trimmed.startsWith("/")) return trimmed;
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.href : null;
  } catch {
    return null;
  }
}
