/**
 * One theme, two screens.
 *
 * The board and the artist page are separate React trees (a route decision happens before either mounts), and both
 * need the same switch. The pre-paint script in `index.html` has already put the right class on the element; this
 * hook adopts what it found, writes the reader's choice, and keeps `<meta name="theme-color">` in step so the phone's
 * browser chrome does not stay the other colour.
 */
import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark";

const KEY = "lineup.theme";
const CHROME: Record<Theme, string> = { dark: "#08080a", light: "#f7f6f4" };

export function useTheme(): { theme: Theme; toggle: () => void } {
  const [theme, setTheme] = useState<Theme>(() =>
    document.documentElement.classList.contains("dark") ? "dark" : "light",
  );

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", CHROME[theme]);
    try {
      localStorage.setItem(KEY, theme);
    } catch {
      /* a browser that refuses storage still renders */
    }
  }, [theme]);

  const toggle = useCallback(() => setTheme((current) => (current === "dark" ? "light" : "dark")), []);
  return { theme, toggle };
}
