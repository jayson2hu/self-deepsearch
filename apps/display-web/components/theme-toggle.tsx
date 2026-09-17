"use client";

import { Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

type Theme = "light" | "dark";

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>("light");

  useEffect(() => {
    const root = document.documentElement;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const sync = () => {
      const explicit = root.dataset.theme;
      setTheme(explicit === "dark" || !explicit && media.matches ? "dark" : "light");
    };
    sync();
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, []);

  function toggle() {
    const next: Theme = theme === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    document.cookie = `mujian_theme=${next}; Max-Age=31536000; Path=/; SameSite=Lax`;
    setTheme(next);
  }

  const nextLabel = theme === "dark" ? "切换到浅色模式" : "切换到深色模式";
  return <button className="theme-toggle" type="button" onClick={toggle} aria-label={nextLabel} title={nextLabel} aria-pressed={theme === "dark"}>
    {theme === "dark" ? <Sun aria-hidden="true" size={18} /> : <Moon aria-hidden="true" size={18} />}
  </button>;
}
