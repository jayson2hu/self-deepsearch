import { forwardPlatformRequest } from "@self-deepsearch/web-runtime/platform-proxy";

export function proxyPlatform(request: Request, prefix: string, segments: string[], maximumBytes?: number): Promise<Response> {
  return forwardPlatformRequest(request, {
    baseURL: process.env.PLATFORM_API_URL ?? "http://127.0.0.1:8080",
    prefix,
    segments,
    maximumBytes,
  });
}
