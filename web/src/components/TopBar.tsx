import { MicrophoneStage, Moon, Sun } from "./icons";
import type { Theme } from "../lib/theme";

/**
 * The page's one bar. It holds identity and the theme switch and nothing else — the town, the dates and the taste
 * are part of the board, not of the chrome, which is why they live in the palette and the URL instead.
 *
 * A plain anchor to `/` and not a router link: this is a single-document app with two routes, and a full load is a
 * cached document plus one shard.
 */
export default function TopBar({ theme, onToggle }: { theme: Theme; onToggle: () => void }) {
  return (
    <header className="bar">
      <div className="bar__row">
        <a className="brand" href="/" aria-label="Messer Lineup">
          <MicrophoneStage />
          <b>Messer Lineup</b>
        </a>
        <div className="bar__actions">
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
