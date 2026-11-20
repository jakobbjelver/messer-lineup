import { useEffect, useRef, useState } from "react";
import ShowRow from "./ShowRow";
import { dayNumber, monthShort, weekdayOf, type Row } from "../lib/indexData";
import type { DayGroup } from "../lib/board";
import type { PortraitMap } from "../lib/portraits";

type Props = {
  days: DayGroup[];
  portraits?: PortraitMap;
  /** The reader's rows, for the mark that only appears while the board shows everyone. */
  mine?: Set<string>;
  markMine?: boolean;
  distanceFor?: (row: Row) => number | null;
  /** The visitor's today, for the one day that gets a lit rule. */
  today: string;
  /** A page that spans towns says which town each row is in. */
  townOf?: (row: Row) => string;
  linkArtist?: boolean;
  /** Rows per page. Groups are never split, so a busy day is one page on its own. */
  pageRows?: number;
};

/** How many whole day groups fit in `limit` rows. */
function groupsWithin(days: DayGroup[], limit: number): number {
  let count = 0;
  let groups = 0;
  for (const group of days) {
    if (count >= limit) break;
    count += group.rows.length;
    groups += 1;
  }
  return Math.max(groups, 1);
}

/**
 * The board: a sticky date, then that day's shows.
 *
 * Rendering is paged **by row count but cut on a day boundary**, because both units matter: 693 rows in
 * the DOM is a 54 000 px page, and a day split across two pages is a day that reads as two. Sixty rows is
 * four or five calm days, or one very busy one — and the sentinel appends before the reader reaches the end.
 */
export default function Board({
  days,
  portraits,
  mine,
  markMine = false,
  distanceFor,
  today,
  townOf,
  linkArtist = true,
  pageRows = 60,
}: Props) {
  const [limit, setLimit] = useState(pageRows);
  const sentinel = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    setLimit(pageRows);
  }, [days, pageRows]);

  const shown = days.slice(0, groupsWithin(days, limit));
  const complete = shown.length === days.length;

  useEffect(() => {
    const node = sentinel.current;
    if (!node || complete) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setLimit((current) => current + pageRows);
        }
      },
      { rootMargin: "900px 0px" },
    );
    io.observe(node);
    return () => io.disconnect();
  }, [complete, limit, pageRows]);

  return (
    <div className="board">
      {shown.map((group) => {
        const isToday = group.day === today;
        return (
          <section className={`day${isToday ? " day--today" : ""}`} key={group.day}>
            <h2 className="day__head">
              <span className="day__label">
                <span className="day__wd">{weekdayOf(group.day)}</span>
                <span className="num day__dd">{dayNumber(group.day)}</span>
                <span className="day__mo">{monthShort(group.day)}</span>
              </span>
              <span className="day__n">
                <span className="num">{group.rows.length}</span>
                <span className="day__nword"> {group.rows.length === 1 ? "show" : "shows"}</span>
              </span>
            </h2>
            {group.rows.map((row) => (
              <ShowRow
                key={row.k}
                row={row}
                portraits={portraits}
                mine={markMine && mine?.has(row.k)}
                linkArtist={linkArtist}
                distance={distanceFor?.(row) ?? null}
                town={townOf?.(row)}
              />
            ))}
          </section>
        );
      })}
      {complete ? null : <div ref={sentinel} className="board__sentinel" aria-hidden="true" />}
    </div>
  );
}
