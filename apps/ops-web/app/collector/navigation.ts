export type CollectorSearch = Partial<Record<"q" | "status" | "batch_id" | "offset" | "batch_offset", string>>;
export type CollectorSearchInput = Record<string, string | string[] | undefined>;

export const candidateStatuses = ["pending", "valid", "invalid", "duplicate", "conflict", "reviewing", "accepted", "rejected"] as const;
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const keys = ["q", "status", "batch_id", "offset", "batch_offset"] as const;

// Only known query fields may enter API queries or links back into the workspace.
export function readCollectorSearch(input: CollectorSearchInput): CollectorSearch {
  const result: CollectorSearch = {};
  const q = typeof input.q === "string" ? input.q.trim() : "";
  if (q && Array.from(q).length <= 200 && !q.includes("\0")) result.q = q;
  if (typeof input.status === "string" && candidateStatuses.some((value) => value === input.status)) result.status = input.status;
  if (typeof input.batch_id === "string" && uuid.test(input.batch_id)) result.batch_id = input.batch_id.toLowerCase();
  for (const key of ["offset", "batch_offset"] as const) {
    const value = input[key];
    if (typeof value === "string" && /^\d+$/.test(value) && Number(value) > 0 && Number(value) <= 1_000_000) result[key] = String(Number(value));
  }
  return result;
}

export function collectorSearchParams(search: CollectorSearch): URLSearchParams {
  const query = new URLSearchParams();
  const clean = readCollectorSearch(search);
  for (const key of keys) if (clean[key]) query.set(key, clean[key]!);
  return query;
}

export function collectorListHref(search: CollectorSearch = {}, changes: CollectorSearch = {}, section: "candidates" | "batches" = "candidates"): string {
  const query = collectorSearchParams({ ...search, ...changes }).toString();
  return `/collector${query ? `?${query}` : ""}#${section}`;
}

export function collectorCandidateHref(id: string, search: CollectorSearch): string {
  if (!uuid.test(id)) return collectorListHref(search);
  const query = collectorSearchParams(search).toString();
  return `/collector/${id.toLowerCase()}${query ? `?${query}` : ""}`;
}
