/**
 * next-themes 要等 React 首次 commit 之后才把主题类写到 <html> 上，冷加载时
 * 暗色用户会先看到一整帧亮色。故障处置常常是从告警链接冷启动的，每次刷新闪一次白
 * 既是眩光，也会让人怀疑是不是加载错了。这里在首帧之前同步落一次主题，
 * 与密度用同一套前置手法（见 densityMode.ts）。
 */

/** next-themes 的默认存储键。改 ThemeProvider 的 storageKey 时要同改这里。 */
const THEME_STORAGE_KEY = "theme";

const DARK_CLASS = "dark";
const LIGHT_CLASS = "light";

function prefersDark(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") {
    return false;
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

/** `light` / `dark` / `system`（含未设置）三档，与 ThemeProvider 的取值一致。 */
export function readThemeMode(): "light" | "dark" | "system" {
  if (typeof window === "undefined") return "system";
  try {
    const stored = window.localStorage?.getItem(THEME_STORAGE_KEY);
    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    return "system";
  }
}

export function applyThemeMode(mode: "light" | "dark" | "system"): void {
  if (typeof document === "undefined") return;
  const dark = mode === "dark" || (mode === "system" && prefersDark());
  const root = document.documentElement;
  root.classList.toggle(DARK_CLASS, dark);
  root.classList.toggle(LIGHT_CLASS, !dark);
  root.style.colorScheme = dark ? "dark" : "light";
}
