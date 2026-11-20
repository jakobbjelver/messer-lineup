import { SlidersHorizontal } from "@phosphor-icons/react";
import Mark from "./Mark";

type Props = {
  /** Omitted where settings cannot be changed in place. */
  onPanel?: () => void;
};

/**
 * The chrome: a mark, and settings.
 *
 * The view control moved down into the board's own head, beside `yours · all` — both switch what the list is
 * showing, so both belong to the list, and the bar keeps the one thing that is not about the list's contents.
 *
 * The location control lives in the town panel, where the question it answers is asked. A bar that carried it
 * had three icons for one of them to be a maybe.
 */
export default function Chrome({ onPanel }: Props) {
  return (
    <header className="bar">
      <div className="bar__row">
        <a className="bar__brand" href="/" aria-label="Messer Lineup">
          <span className="bar__mark">
            <Mark size={30} />
          </span>
          <span className="bar__name">Lineup</span>
        </a>
        <div className="bar__actions">
          {onPanel ? (
            <button className="iconbtn" aria-label="Artists and town" onClick={onPanel}>
              <SlidersHorizontal size={20} />
            </button>
          ) : null}
        </div>
      </div>
    </header>
  );
}
