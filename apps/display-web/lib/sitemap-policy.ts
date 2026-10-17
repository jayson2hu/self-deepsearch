export const sitemapPageSize = 50000;
export const maximumSitemapBytes = 50 * 1024 * 1024;

export type SitemapEntity = "works" | "performers" | "studios";

export function sitemapShardPaths(totals: Record<SitemapEntity, number>): string[] {
  const entities = ["works", "performers", "studios"] as const;
  const shardCounts = entities.map((entity) => {
    const total = totals[entity];
    if (!Number.isSafeInteger(total) || total < 0) throw new RangeError("Invalid sitemap entity count");
    return Math.ceil(total / sitemapPageSize);
  });
  if (1 + shardCounts.reduce((sum, count) => sum + count, 0) > sitemapPageSize) {
    throw new RangeError("Sitemap index exceeds shard limit");
  }
  const paths = ["/sitemaps/pages"];
  for (const [index, entity] of entities.entries()) {
    for (let page = 1; page <= shardCounts[index]; page += 1) paths.push(`/sitemaps/${entity}/${page}`);
  }
  return paths;
}

export function sitemapWithinLimits(entryCount: number, xmlOrBytes: string | number): boolean {
  const bytes = typeof xmlOrBytes === "number" ? xmlOrBytes : Buffer.byteLength(xmlOrBytes, "utf8");
  return Number.isSafeInteger(entryCount) && entryCount >= 0 && entryCount <= sitemapPageSize &&
    Number.isSafeInteger(bytes) && bytes >= 0 && bytes <= maximumSitemapBytes;
}

export function sitemapEntityPath(entity: SitemapEntity, slug: string): string {
  if (typeof slug !== "string" || !/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(slug)) throw new TypeError("Invalid sitemap slug");
  return `/${entity}/${slug}`;
}
