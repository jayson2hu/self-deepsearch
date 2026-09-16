import { sanitizeReturnTo } from "./return-target";

export type AuthIntentKind = "favorite" | "follow";

export type AuthIntent = {
  version: 1;
  kind: AuthIntentKind;
  entityId: string;
  targetPath: string;
  createdAt: number;
};

export const AUTH_INTENT_STORAGE_KEY = "mujian.auth-intent.v1";
export const AUTH_INTENT_TTL_MS = 15 * 60 * 1000;
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function availableSessionStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.sessionStorage;
  } catch {
    return null;
  }
}

function validIntent(value: unknown): value is AuthIntent {
  if (!value || typeof value !== "object") return false;
  const intent = value as Partial<AuthIntent>;
  const target = sanitizeReturnTo(intent.targetPath);
  const expectedPrefix = intent.kind === "favorite" ? "/works/" : intent.kind === "follow" ? "/performers/" : null;
  return intent.version === 1
    && expectedPrefix !== null
    && typeof intent.entityId === "string"
    && uuidPattern.test(intent.entityId)
    && typeof intent.createdAt === "number"
    && Number.isFinite(intent.createdAt)
    && target === intent.targetPath
    && target.startsWith(expectedPrefix);
}

export function createAuthIntent(kind: AuthIntentKind, entityId: string, targetPath: string, createdAt = Date.now()): AuthIntent | null {
  const candidate: AuthIntent = { version: 1, kind, entityId, targetPath, createdAt };
  return validIntent(candidate) ? candidate : null;
}

export function saveAuthIntent(storage: Pick<Storage, "setItem">, intent: AuthIntent): boolean {
  if (!validIntent(intent)) return false;
  try {
    storage.setItem(AUTH_INTENT_STORAGE_KEY, JSON.stringify(intent));
    return true;
  } catch {
    return false;
  }
}

export function clearAuthIntent(storage: Pick<Storage, "removeItem">): void {
  try {
    storage.removeItem(AUTH_INTENT_STORAGE_KEY);
  } catch {
    // Storage can be disabled; returning to the page must still work.
  }
}

export function loadAuthIntent(storage: Pick<Storage, "getItem" | "removeItem">, now = Date.now()): AuthIntent | null {
  let raw: string | null;
  try {
    raw = storage.getItem(AUTH_INTENT_STORAGE_KEY);
  } catch {
    return null;
  }
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (!validIntent(value) || value.createdAt > now || now - value.createdAt > AUTH_INTENT_TTL_MS) {
      clearAuthIntent(storage);
      return null;
    }
    return value;
  } catch {
    clearAuthIntent(storage);
    return null;
  }
}

export function matchingAuthIntent(
  storage: Pick<Storage, "getItem" | "removeItem">,
  expected: Pick<AuthIntent, "kind" | "entityId" | "targetPath">,
  now = Date.now(),
): AuthIntent | null {
  const intent = loadAuthIntent(storage, now);
  return intent
    && intent.kind === expected.kind
    && intent.entityId === expected.entityId
    && intent.targetPath === expected.targetPath
    ? intent
    : null;
}
