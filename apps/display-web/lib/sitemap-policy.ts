export const sitemapPageSize = 50000;
export const maximumSitemapBytes = 50 * 1024 * 1024;

export type SitemapEntity = "works" | "performers" | "studios";

export function sitemapShardPaths(totals: Record<SitemapEntity, number>): string[] {
  const paths = ["/sitemaps/pages"];
  for (const entity of ["works", "performers", "studios"] as const) {
    const total = Math.max(0, Math.floor(totals[entity]));
    for (let page = 1; page <= Math.ceil(total / sitemapPageSize); page += 1) paths.push(`/sitemaps/${entity}/${page}`);
  }
  return paths;
}

export function sitemapWithinLimits(entryCount: number, xmlOrBytes: string | number): boolean {
  const bytes = typeof xmlOrBytes === "number" ? xmlOrBytes : Buffer.byteLength(xmlOrBytes, "utf8");
  return entryCount <= sitemapPageSize && bytes <= maximumSitemapBytes;
}
