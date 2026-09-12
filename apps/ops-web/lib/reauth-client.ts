/**
 * Sends an operator mutation and, when the API asks for recent password
 * confirmation, performs one confirmation round-trip before retrying it.
 * High-risk requests in Release A all use replayable JSON bodies.
 */
export async function adminFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const requestInit: RequestInit = { ...init, credentials: init.credentials ?? "include" };
  const response = await fetch(input, requestInit);
  if (response.status !== 401) return response;

  const body = await response.clone().json().catch(() => null) as { error?: { code?: string } } | null;
  if (body?.error?.code !== "REAUTH_REQUIRED" || typeof window === "undefined") return response;

  const password = window.prompt("该操作需要近期密码确认，请输入当前账号密码");
  if (!password) return response;
  const confirmation = await fetch("/api/v1/auth/reauth", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
    signal: requestInit.signal ?? AbortSignal.timeout(15000),
  });
  if (!confirmation.ok) return confirmation;
  return fetch(input, requestInit);
}
