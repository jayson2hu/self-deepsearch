"use client";

import { CheckCircle2, RefreshCw } from "lucide-react";
import Script from "next/script";
import { useEffect, useRef, useState } from "react";

type TurnstileOptions = {
  sitekey: string;
  action: TurnstileAction;
  callback: (token: string) => void;
  "error-callback": () => void;
  "expired-callback": () => void;
  "timeout-callback": () => void;
  size: "flexible";
};

type TurnstileAPI = {
  render: (container: HTMLElement, options: TurnstileOptions) => string;
  remove: (widgetID: string) => void;
  reset: (widgetID: string) => void;
};

declare global {
  interface Window {
    turnstile?: TurnstileAPI;
  }
}

type ChallengeState = "loading" | "waiting" | "verified" | "expired" | "error";
type TurnstileAction = "signup_code" | "password_code";

export function TurnstileField({
  action,
  allowDevelopmentBypass,
  onToken,
  resetKey = 0,
  siteKey,
}: {
  action: TurnstileAction;
  allowDevelopmentBypass: boolean;
  onToken: (token: string) => void;
  resetKey?: number;
  siteKey: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const widgetIDRef = useRef<string | null>(null);
  const [scriptReady, setScriptReady] = useState(false);
  const [scriptAttempt, setScriptAttempt] = useState(0);
  const [challengeState, setChallengeState] = useState<ChallengeState>("loading");

  useEffect(() => {
    if (!siteKey && allowDevelopmentBypass) {
      onToken("test-pass");
      return;
    }
    if (!siteKey) {
      onToken("");
      return;
    }
    if (!scriptReady || !containerRef.current) return;
    let active = true;
    if (!window.turnstile) {
      queueMicrotask(() => { if (active) setChallengeState("error"); });
      return () => { active = false; };
    }

    const turnstile = window.turnstile;
    const container = containerRef.current;
    onToken("");
    queueMicrotask(() => { if (active) setChallengeState("waiting"); });
    container.replaceChildren();
    try {
      widgetIDRef.current = turnstile.render(container, {
        sitekey: siteKey,
        action,
        size: "flexible",
        callback: (token) => {
          if (!active) return;
          onToken(token);
          setChallengeState("verified");
        },
        "error-callback": () => {
          if (!active) return;
          onToken("");
          setChallengeState("error");
        },
        "expired-callback": () => {
          if (!active) return;
          onToken("");
          setChallengeState("expired");
        },
        "timeout-callback": () => {
          if (!active) return;
          onToken("");
          setChallengeState("expired");
        },
      });
    } catch {
      queueMicrotask(() => {
        if (!active) return;
        onToken("");
        setChallengeState("error");
      });
    }

    return () => {
      active = false;
      if (widgetIDRef.current) {
        try { turnstile.remove(widgetIDRef.current); } catch { /* The widget may already have removed itself. */ }
        widgetIDRef.current = null;
      }
    };
  }, [action, allowDevelopmentBypass, onToken, resetKey, scriptReady, siteKey]);

  if (!siteKey) {
    if (allowDevelopmentBypass) return null;
    return <div className="turnstile-field turnstile-field--unavailable" role="alert">人机验证配置不可用，暂时无法发送验证码。</div>;
  }

  function retry() {
    onToken("");
    const widgetID = widgetIDRef.current;
    if (widgetID && window.turnstile) {
      try {
        window.turnstile.reset(widgetID);
        setChallengeState("waiting");
        return;
      } catch {
        widgetIDRef.current = null;
      }
    }
    setChallengeState("loading");
    setScriptReady(false);
    setScriptAttempt((current) => current + 1);
  }

  const failed = challengeState === "error" || challengeState === "expired";
  return <div aria-busy={challengeState === "loading"} className="turnstile-field">
    <Script
      id={`self-deepsearch-turnstile-${scriptAttempt}`}
      onError={() => {
        onToken("");
        setScriptReady(false);
        setChallengeState("error");
      }}
      onReady={() => setScriptReady(true)}
      src={`https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit&attempt=${scriptAttempt}`}
      strategy="afterInteractive"
    />
    <div aria-label="人机验证" className="turnstile-widget" ref={containerRef} role="group" />
    <div aria-live="polite" className={`turnstile-status${failed ? " turnstile-status--error" : ""}`} role={challengeState === "error" ? "alert" : "status"}>
      {challengeState === "loading" ? <span>正在加载人机验证…</span> : null}
      {challengeState === "waiting" ? <span>请完成人机验证</span> : null}
      {challengeState === "verified" ? <span><CheckCircle2 aria-hidden="true" size={15} />验证完成</span> : null}
      {challengeState === "expired" ? <span>验证已过期，请重新验证</span> : null}
      {challengeState === "error" ? <span>人机验证暂时无法加载</span> : null}
      {failed ? <button className="turnstile-retry" onClick={retry} type="button"><RefreshCw aria-hidden="true" size={14} />重试验证</button> : null}
    </div>
  </div>;
}
