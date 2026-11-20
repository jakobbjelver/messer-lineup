import { Crosshair, GearSix, ListBullets, SquaresFour } from "@phosphor-icons/react";
import Mark from "./Mark";
import type { Position } from "../lib/location";
import type { View } from "../lib/storage";

type Props = {
  /** Omitted on a page with no view to switch (the artist page). */
  view?: View;
  onView?: (view: View) => void;
  /** Omitted where settings cannot be changed in place. */
  onPanel?: () => void;
  /** Absent where a distance means nothing, or where GPS cannot work. */
  location?: { state: Position["state"]; canLocate: boolean; onLocate: () => void };
};

/**
 * The whole chrome: a mark, at most three controls.
 *
 * The location button is the one that may glow, and only while it is the action worth taking — a
 * granted fix stops the glow, because a light that outlives its action lies about what the next tap
 * does. The view control shows the view you would get, not the one you are in, so its label is always
 * the truth about the tap.
 */
export default function Chrome({ view, onView, onPanel, location }: Props) {
  const canLocate = location?.canLocate ?? false;
  const granted = location?.state === "granted";
  const lit = canLocate && location?.state === "idle";
  const locateLabel =
    location?.state === "asking"
      ? "Asking your browser for your location"
      : granted
        ? "Location is on — distances are from you"
        : "Use my location for distances";

  return (
    <header className="bar">
      <div className="bar__row">
        <a className="bar__mark" href="/" aria-label="Messer Lineup">
          <Mark />
        </a>
        <span className="bar__name">Lineup</span>
        <div className="bar__actions">
          {canLocate && location ? (
            <button
              className={lit ? "iconbtn iconbtn--lit" : "iconbtn"}
              aria-label={locateLabel}
              aria-busy={location.state === "asking" ? true : undefined}
              onClick={() => location.onLocate()}
            >
              <Crosshair size={16} />
            </button>
          ) : null}
          {view && onView ? (
            <button
              className="iconbtn"
              aria-label={view === "list" ? "Show the calendar" : "Show the list"}
              onClick={() => onView(view === "list" ? "calendar" : "list")}
            >
              {view === "list" ? <SquaresFour size={16} /> : <ListBullets size={16} />}
            </button>
          ) : null}
          {onPanel ? (
            <button className="iconbtn" aria-label="Artists and town" onClick={onPanel}>
              <GearSix size={16} />
            </button>
          ) : null}
        </div>
      </div>
    </header>
  );
}
