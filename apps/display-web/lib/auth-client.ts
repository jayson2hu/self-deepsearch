import type { ApiError } from "@self-deepsearch/api-contracts";

export class AuthRequestError extends Error {
  constructor(public readonly code: string, message: string) {
    super(message);
  }
}

export async function authRequest<T>(path: string, body?: unknown): Promise<T> {
  return apiMutation<T>(path, "POST", body);
}

export async function apiMutation<T>(path: string, method: "POST" | "DELETE", body?: unknown): Promise<T> {
  const baseURL = "";
  const response = await fetch(`${baseURL}${path}`, {
    method,
    credentials: "include",
    headers: body === undefined ? { Accept: "application/json" } : { Accept: "application/json", "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const fallback = { error: { code: "REQUEST_FAILED", message: "请求失败，请稍后重试", request_id: "" } };
    const payload = (await response.json().catch(() => fallback)) as ApiError;
    throw new AuthRequestError(payload.error.code, payload.error.message);
  }
  if (response.status === 204) return undefined as T;
  return (await response.json()) as T;
}

export function reloadAfterAuthChange(path = "/"): void {
  window.location.assign(path);
}
