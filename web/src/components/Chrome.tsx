import { CalendarBlank, ListBullets, SlidersHorizontal } from "@phosphor-icons/react";
import Mark from "./Mark";
import type { View } from "../lib/storage";

type Props = {
  /** Omitted on a page with no view to switch (the artist page). */
  view?: View;
  onView?: (view: View) => void;
  /** Omitted where settings cannot be changed in place. */
  onPanel?: () => void;
};

/**
 * The whole chrome: a mark and two controls.
 *
 * The location control moved into the town panel, where the question it answers is asked. A bar that carried
 * it had three icons for one of them to be a maybe; the settings sheet is exactly where "where am I" belongs.
 * The view control shows the view you would get, not the one you are in, so its label is always the truth
 * about the tap.
 */
export default function Chrome({ view, onView, onPanel }: Props) {
  return (
    <header className="bar">
      <div className="bar__row">
        <a className="bar__brand" href="/" aria-label="Messer Lineup">
          <span className="bar__mark">
            <Mark />
          </span>
          <span className="bar__name">Lineup</span>
        </a>
        <div className="bar__actions">
          {view && onView ? (
            <button
              className="iconbtn"
              aria-label={view === "list" ? "Show the calendar" : "Show the list"}
              onClick={() => onView(view === "list" ? "calendar" : "list")}
            >
              {view === "list" ? <CalendarBlank size={16} /> : <ListBullets size={16} />}
            </button>
          ) : null}
          {onPanel ? (
            <button className="iconbtn" aria-label="Artists and town" onClick={onPanel}>
              <SlidersHorizontal size={16} />
            </button>
          ) : null}
        </div>
      </div>
    </header>
  );
}
