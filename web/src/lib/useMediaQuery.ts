import { useEffect, useState } from "react";

/**
 * A media query as state.
 *
 * The one thing a React app cannot ask CSS is "which shape should this overlay be", because a dialog and a bottom sheet
 * are different *behaviours* (a dialog traps focus in the middle of the screen, a sheet holds the bottom edge) and not
 * two styles of one box. So the breakpoint is read once here and the overlay picks its shape from it.
 */
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(() =>
    typeof window === "undefined" ? false : window.matchMedia(query).matches,
  );

  useEffect(() => {
    const list = window.matchMedia(query);
    const update = () => setMatches(list.matches);
    update();
    list.addEventListener("change", update);
    return () => list.removeEventListener("change", update);
  }, [query]);

  return matches;
}

/** The family's one breakpoint: above it there is room for a centred dialog, below it there is not. */
export const useWide = (): boolean => useMediaQuery("(min-width: 721px)");
