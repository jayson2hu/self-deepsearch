import "server-only";

export type TurnstilePublicConfig = {
  siteKey: string;
  allowDevelopmentBypass: boolean;
};

export function turnstilePublicConfig(): TurnstilePublicConfig {
  const appEnv = process.env.APP_ENV?.trim().toLowerCase() || "development";
  return {
    siteKey: process.env.TURNSTILE_SITE_KEY?.trim() || "",
    allowDevelopmentBypass:
      process.env.NODE_ENV === "development" ||
      (appEnv !== "production" && process.env.TURNSTILE_BYPASS === "true"),
  };
}
