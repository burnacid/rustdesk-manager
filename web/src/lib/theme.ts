import { useEffect, useState } from "react";

// Light / dark / follow the OS. The choice lives in this browser only (localStorage);
// index.html applies it before the first paint so the page doesn't flash.

export type Theme = "light" | "dark" | "system";
const KEY = "rdm-theme";

function stored(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    if (t === "light" || t === "dark") return t;
  } catch {
    // storage blocked: follow the OS
  }
  return "system";
}

const media = () => window.matchMedia("(prefers-color-scheme: dark)");

function apply(theme: Theme) {
  const dark = theme === "dark" || (theme === "system" && media().matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light"; // native inputs and scrollbars
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setTheme] = useState<Theme>(stored);

  useEffect(() => {
    apply(theme);
    if (theme !== "system") return;
    const m = media();
    const onChange = () => apply("system");
    m.addEventListener("change", onChange);
    return () => m.removeEventListener("change", onChange);
  }, [theme]);

  const choose = (t: Theme) => {
    try {
      if (t === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, t);
    } catch {
      // not saved; still applies for this visit
    }
    setTheme(t);
    window.dispatchEvent(new Event("rdm:theme")); // keep other switchers on the page in sync
  };

  useEffect(() => {
    const sync = () => setTheme(stored());
    window.addEventListener("rdm:theme", sync);
    return () => window.removeEventListener("rdm:theme", sync);
  }, []);

  return [theme, choose];
}
