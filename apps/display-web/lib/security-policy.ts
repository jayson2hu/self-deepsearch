const sensitivePublicPath = /^(?:\/(?:login|register|forgot-password|invite|logout)\/?|\/account(?:\/.*)?)$/;

export function createRequestNonce(): string {
  const bytes = new Uint8Array(16);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

export function isSensitivePublicPage(pathname: string): boolean {
  return sensitivePublicPath.test(pathname);
}
