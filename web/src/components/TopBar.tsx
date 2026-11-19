import { Crosshair, MicrophoneStage, Moon, Sun } from "./icons";
import type { Theme } from "../lib/theme";
import type { Position } from "../lib/location";

/**
 * The page's one bar. It holds identity, the location control and the theme switch — the town, the dates and the taste
 * are part of the board, not of the chrome, which is why they live in the palette and the URL instead.
 *
 * The location button is the design's second lit control, and the only one that may glow besides `Set taste`: while
 * the town is still the edge's guess it is the action worth taking, so it carries the neon treatment. Once a fix is in
 * hand it goes quiet (accent icon, no bloom) — **a glow that outlives the action is a lie about what tapping it will
 * do next.** It is absent entirely where GPS cannot work (no secure context), because a control that cannot do its job
 * is a lie of the same kind.
 *
 * A plain anchor to `/` and not a router link: this is a single-document app with two routes, and a full load is a
 * cached document plus one shard.
 */
export type BarLocation = {
  state: Position["state"];
  /** Whether this browser could grant a fix at all: GPS needs a secure context. */
  canLocate: boolean;
  onLocate: () => void;
  /** Opens the palette on the town tab — where a refusal is explained, and where the town list lives. */
  onExplain: () => void;
};

export default function TopBar({
  theme,
  onToggle,
  location,
}: {
  theme: Theme;
  onToggle: () => void;
  /** Omitted on a page where a distance means nothing (the artist route), which is why the button is not global. */
  location?: BarLocation;
}) {
  const granted = location?.state === "granted";
  // Only the un-asked state glows: after a refusal the action is not worth taking, and after a fix it is done.
  const lit = location !== undefined && location.canLocate && location.state === "idle";
  const label =
    location === undefined
      ? ""
      : granted
        ? "Using your location"
        : location.state === "asking"
          ? "Asking your browser for your location"
          : location.state === "denied" || location.state === "unavailable"
            ? "Location is not available — open the town list"
            : "Use my location";

  return (
    <header className="bar">
      <div className="bar__row">
        <a className="brand" href="/" aria-label="Messer Lineup">
          <MicrophoneStage />
          <b>Messer Lineup</b>
        </a>
        <div className="bar__actions">
          {location !== undefined && location.canLocate ? (
            <button
              className={lit ? "iconbtn neon" : "iconbtn"}
              data-located={granted ? "" : undefined}
              aria-label={label}
              aria-busy={location.state === "asking" ? true : undefined}
              onClick={() => {
                // A granted button explains (reach, and how the fix is held); a refused one sends you to the town
                // list with the sentence that says what happened; only the un-asked state actually asks.
                if (granted || location.state === "denied" || location.state === "unavailable") location.onExplain();
                else location.onLocate();
              }}
            >
              <Crosshair />
            </button>
          ) : null}
          <button
            className="iconbtn"
            onClick={onToggle}
            aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
          >
            {theme === "dark" ? <Sun /> : <Moon />}
          </button>
        </div>
      </div>
    </header>
  );
}
