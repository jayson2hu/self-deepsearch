import { requestPassword } from "./password-confirmation";

type ConfirmationRound = {
  controller: AbortController;
  promise: Promise<Response | null>;
  waiters: number;
};

let pendingConfirmation: ConfirmationRound | null = null;
let confirmationVersion = 0;
let completedConfirmation: { version: number; result: Response | null; error?: unknown } | null = null;

/**
 * Sends an operator mutation and, when the API asks for recent password
 * confirmation, performs one confirmation round-trip before retrying it.
 * High-risk requests in Release A all use replayable JSON bodies.
 */
export async function adminFetch(input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> {
  const observedConfirmation = confirmationVersion;
  // Capture before the caller's busy state disables the initiating control.
  const focused = typeof document === "undefined" ? null : document.activeElement;
  const returnFocus = typeof HTMLElement !== "undefined" && focused instanceof HTMLElement ? focused : null;
  const requestInit: RequestInit = { ...init, credentials: init.credentials ?? "include" };
  const response = await fetch(input, requestInit);
  if (response.status !== 401) return response;

  const body = await response.clone().json().catch(() => null) as { error?: { code?: string } } | null;
  if (body?.error?.code !== "REAUTH_REQUIRED" || typeof window === "undefined") return response;

  // A concurrent request can receive its initial 401 after the shared dialog
  // has already finished. Reuse that outcome instead of opening another one.
  requestInit.signal?.throwIfAborted();
  const completed = completedConfirmation;
  if (completed && completed.version > observedConfirmation && completed.error !== undefined) throw completed.error;
  const confirmation = completed && completed.version > observedConfirmation
    ? completed.result
    : await recentPasswordConfirmation(requestInit.signal, returnFocus);
  if (!confirmation) return response;
  // Each concurrent caller receives its own readable error body.
  if (!confirmation.ok) return confirmation.clone();
  requestInit.signal?.throwIfAborted();
  return fetch(input, requestInit);
}

async function confirmPassword(signal: AbortSignal, returnFocus: HTMLElement | null): Promise<Response | null> {
  const password = await requestPassword(signal, returnFocus);
  if (password === null) return null;
  signal.throwIfAborted();
  return fetch("/api/v1/auth/reauth", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ password }),
    signal: AbortSignal.any([signal, AbortSignal.timeout(15000)]),
  });
}

function recentPasswordConfirmation(signal: AbortSignal | null | undefined, returnFocus: HTMLElement | null): Promise<Response | null> {
  signal?.throwIfAborted();
  if (!pendingConfirmation) {
    const round: ConfirmationRound = { controller: new AbortController(), promise: Promise.resolve(null), waiters: 0 };
    pendingConfirmation = round;
    round.promise = confirmPassword(round.controller.signal, returnFocus).then((result) => {
      if (!round.controller.signal.aborted) completedConfirmation = { version: ++confirmationVersion, result };
      return result;
    }, (error: unknown) => {
      if (!round.controller.signal.aborted) completedConfirmation = { version: ++confirmationVersion, result: null, error };
      throw error;
    }).finally(() => {
      if (pendingConfirmation === round) pendingConfirmation = null;
    });
  }
  const round = pendingConfirmation;
  round.waiters++;
  return new Promise((resolve, reject) => {
    let settled = false;
    const finish = (result: Response | null, error?: unknown) => {
      if (settled) return;
      settled = true;
      signal?.removeEventListener("abort", abort);
      round.waiters--;
      if (round.waiters === 0 && pendingConfirmation === round) {
        pendingConfirmation = null;
        round.controller.abort();
      }
      if (error !== undefined) reject(error);
      else resolve(result);
    };
    const abort = () => finish(null, signal?.reason);
    signal?.addEventListener("abort", abort, { once: true });
    round.promise.then((result) => finish(result), (error: unknown) => finish(null, error));
  });
}
