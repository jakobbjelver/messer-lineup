import type { CSSProperties } from "react";

/**
 * The one loader.
 *
 * A 3×3 grid of dots with a diagonal wave — Aurral's dot helix, made to read at a glance: the peak dot grows and
 * lights instead of only fading, so the pulse travels across the grid rather than blinking in place. It replaces
 * every skeleton on the surface: a page that is loading says so once, in one place, instead of drawing the page it
 * does not have yet.
 */
export default function Loader({ size = 28, label = "Loading" }: { size?: number; label?: string }) {
  const tile = Math.max(4, Math.round(size * 0.19));
  const gap = Math.max(3, Math.round(size * 0.1));
  const style = { "--t": `${tile}px`, "--g": `${gap}px` } as CSSProperties;
  return (
    <span className="loader" style={style} role="status" aria-label={label}>
      {Array.from({ length: 9 }, (_, index) => {
        // A diagonal wave: the delay is the dot's distance from the top-left corner.
        const row = Math.floor(index / 3);
        const column = index % 3;
        return <i key={index} aria-hidden="true" style={{ animationDelay: `${(row + column) * 95}ms` }} />;
      })}
    </span>
  );
}
