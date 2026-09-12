import type { FeedbackListResponse, HistoryResponse, HomeResponse, ItemListResponse, PerformerDetailResponse, SessionResponse, SitemapResponse, StudioDetailResponse, StudioListResponse, UserSummary, WorkDetailResponse, WorkSearchResponse } from "@self-deepsearch/api-contracts";
import { cookies } from "next/headers";
import { mediaProjection } from "./media-policy";

const unavailableHome: HomeResponse = {
  generated_at: "1970-01-01T00:00:00Z",
  sections: [
    { key: "discovery", title: "发现", items: [] },
    { key: "latest", title: "最新发行", items: [] },
    { key: "editorial", title: "编辑推荐", items: [] },
    { key: "performers", title: "人物资料", items: [] },
    { key: "trending", title: "近期热门", items: [] },
    { key: "most_viewed", title: "浏览最多", items: [] }
  ]
};

export async function getHome(): Promise<{ data: HomeResponse; degraded: boolean }> {
  try {
    return { data: await fetchPlatform<HomeResponse>("/api/v1/site/home", 300), degraded: false };
  } catch {
    return { data: unavailableHome, degraded: true };
  }
}

export async function getSitemapEntries(entityType: "work" | "performer" | "studio"): Promise<SitemapResponse | null> {
  try {
    return await fetchPlatform<SitemapResponse>(`/api/v1/site/sitemap?entity_type=${entityType}`, 300);
  } catch {
    return null;
  }
}

export async function searchWorks(query: string, cursor = ""): Promise<{ data: WorkSearchResponse | null; degraded: boolean }> {
  try {
    const path = `/api/v1/search/works?q=${encodeURIComponent(query)}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`;
    return { data: await fetchPlatformNoStore<WorkSearchResponse>(path), degraded: false };
  } catch {
    return { data: null, degraded: true };
  }
}

export async function getPopularWorks(range: "7d" | "30d" | "all", cursor = ""): Promise<{ data: ItemListResponse | null; degraded: boolean }> {
  try {
    return { data: await fetchPlatform<ItemListResponse>(`/api/v1/works/popular?range=${range}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ""}`, 60), degraded: false };
  } catch {
    return { data: null, degraded: true };
  }
}

export async function getLatestWorks(cursor = ""): Promise<{ data: ItemListResponse | null; degraded: boolean }> {
  try {
    return { data: await fetchPlatform<ItemListResponse>(`/api/v1/works/latest${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, 300), degraded: false };
  } catch {
    return { data: null, degraded: true };
  }
}

export async function getRecentlyAddedWorks(cursor = ""): Promise<{ data: ItemListResponse | null; degraded: boolean }> {
  try {
    return { data: await fetchPlatform<ItemListResponse>(`/api/v1/works/recent${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, 300), degraded: false };
  } catch {
    return { data: null, degraded: true };
  }
}

export async function getWork(slug: string): Promise<
  | { status: "ok"; data: WorkDetailResponse }
  | { status: "not_found" }
  | { status: "degraded" }
> {
  try {
    const response = await platformRequest(`/api/v1/works/${encodeURIComponent(slug)}`, 300);
    if (response.status === 404) return { status: "not_found" };
    if (!response.ok) return { status: "degraded" };
    return { status: "ok", data: mediaProjection((await response.json()) as WorkDetailResponse) };
  } catch {
    return { status: "degraded" };
  }
}

export async function getPerformers(cursor = ""): Promise<{ data: ItemListResponse | null; degraded: boolean }> {
  try {
    return { data: await fetchPlatform<ItemListResponse>(`/api/v1/performers${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, 300), degraded: false };
  } catch {
    return { data: null, degraded: true };
  }
}

export async function getPerformer(slug: string): Promise<
  | { status: "ok"; data: PerformerDetailResponse }
  | { status: "not_found" }
  | { status: "degraded" }
> {
  try {
    const response = await platformRequest(`/api/v1/performers/${encodeURIComponent(slug)}`, 300);
    if (response.status === 404) return { status: "not_found" };
    if (!response.ok) return { status: "degraded" };
    return { status: "ok", data: mediaProjection((await response.json()) as PerformerDetailResponse) };
  } catch {
    return { status: "degraded" };
  }
}

export async function getStudios(cursor = ""): Promise<{ data: StudioListResponse | null; degraded: boolean }> {
  try { return { data: await fetchPlatform<StudioListResponse>(`/api/v1/studios${cursor ? `?cursor=${encodeURIComponent(cursor)}` : ""}`, 300), degraded: false }; }
  catch { return { data: null, degraded: true }; }
}

export async function getStudio(slug: string): Promise<
  | { status: "ok"; data: StudioDetailResponse }
  | { status: "not_found" }
  | { status: "degraded" }
> {
  try {
    const response = await platformRequest(`/api/v1/studios/${encodeURIComponent(slug)}`, 300);
    if (response.status === 404) return { status: "not_found" };
    if (!response.ok) return { status: "degraded" };
    return { status: "ok", data: mediaProjection((await response.json()) as StudioDetailResponse) };
  } catch { return { status: "degraded" }; }
}

async function fetchPlatform<T>(path: string, revalidate: number): Promise<T> {
  const response = await platformRequest(path, revalidate);
  if (!response.ok) throw new Error(`platform API returned ${response.status}`);
  return mediaProjection((await response.json()) as T);
}

async function fetchPlatformNoStore<T>(path: string): Promise<T> {
  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  const response = await fetch(`${baseURL}${path}`, {
    headers: { Accept: "application/json" },
    cache: "no-store",
    signal: AbortSignal.timeout(2500),
  });
  if (!response.ok) throw new Error(`platform API returned ${response.status}`);
  return mediaProjection((await response.json()) as T);
}

function platformRequest(path: string, revalidate: number): Promise<Response> {
  const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
  return fetch(`${baseURL}${path}`, {
    headers: { Accept: "application/json" },
    next: { revalidate, tags: ["public-catalog"] },
    signal: AbortSignal.timeout(2500),
  });
}

export async function getCurrentUser(): Promise<UserSummary | null> {
  const session = (await cookies()).get("sd_session")?.value;
  if (!session) return null;
  try {
    const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
    const response = await fetch(`${baseURL}/api/v1/me`, {
      headers: { Accept: "application/json", Cookie: `sd_session=${session}` },
      cache: "no-store",
      signal: AbortSignal.timeout(2000),
    });
    if (!response.ok) return null;
    return ((await response.json()) as SessionResponse).user;
  } catch {
    return null;
  }
}

export function getFavorites(): Promise<ItemListResponse | null> {
  return privateGet<ItemListResponse>("/api/v1/me/favorites");
}

export function getFollows(): Promise<ItemListResponse | null> {
  return privateGet<ItemListResponse>("/api/v1/me/follows");
}

export function getHiddenWorks(): Promise<ItemListResponse | null> {
  return privateGet<ItemListResponse>("/api/v1/me/hidden-works");
}

export function getHistory(): Promise<HistoryResponse | null> {
  return privateGet<HistoryResponse>("/api/v1/me/history");
}

export function getFeedback(): Promise<FeedbackListResponse | null> {
	return privateGet<FeedbackListResponse>("/api/v1/me/feedback", false);
}

async function privateGet<T>(path: string, projectMedia = true): Promise<T | null> {
  const session = (await cookies()).get("sd_session")?.value;
  if (!session) return null;
  try {
    const baseURL = process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080";
    const response = await fetch(`${baseURL}${path}`, {
      headers: { Accept: "application/json", Cookie: `sd_session=${session}` },
      cache: "no-store", signal: AbortSignal.timeout(2500),
    });
    if (!response.ok) return null;
		const value = (await response.json()) as T;
		return projectMedia ? mediaProjection(value) : value;
	} catch {
		return null;
	}
}
