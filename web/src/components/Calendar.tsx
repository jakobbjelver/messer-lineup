import { useMemo } from "react";
import { CaretLeft, CaretRight } from "@phosphor-icons/react";
import ResponsiveDialog from "./ui/ResponsiveDialog";
import Avatar from "./Avatar";
import ShowRow from "./ShowRow";
import { monthLong, monthShort, weekdayOf, type Row } from "../lib/indexData";
import type { PortraitMap } from "../lib/portraits";
import { monthCells, shiftMonth } from "../lib/board";

type SheetProps = {
  day: string | null;
  rows: Row[];
  portraits?: PortraitMap;
  onClose: () => void;
};

/**
 * A day, in the board's own rows.
 *
 * The calendar is an index; this is the page. On a phone it is the answer to "text in a tiny cell" —
 * and because it reuses `ShowRow`, a day looks exactly like the list it came from.
 */
export function DaySheet({ day, rows, portraits, onClose }: SheetProps) {
  return (
    <ResponsiveDialog
      open={day !== null}
      onOpenChange={(next) => (next ? undefined : onClose())}
      label={day ? `Shows on ${day}` : "Shows"}
      className="ui-dialog--day"
    >
      <div className="daysheet">
        <h2 className="daysheet__title">
          {day ? weekdayOf(day) : ""}
          <span className="num">{day ? day.slice(8, 10) : ""}</span>
          {day ? monthShort(day) : ""}
        </h2>
        <div className="daysheet__body">
          {rows.map((row) => (
            <ShowRow key={row.k} row={row} portraits={portraits} />
          ))}
        </div>
      </div>
    </ResponsiveDialog>
  );
}

type Props = {
  /** The board's days, already narrowed to what the reader is looking at. */
  days: { day: string; rows: Row[] }[];
  month: string;
  onMonth: (month: string) => void;
  portraits?: PortraitMap;
  today: string;
  /** The published window: days outside it are not "empty", they are not covered. */
  from: string;
  to: string;
  /** How many faces a cell holds before it prints `+N`. */
  maxFaces?: number;
  onDay: (day: string) => void;
};

/**
 * The month grid: a calendar made of faces.
 *
 * Every cell is built from the rows already in memory, so a month change costs no request — that is
 * the point of the shard cache, and the reason the calendar can be a view rather than a second page.
 */
export default function Calendar({ days, month, onMonth, portraits, today, from, to, maxFaces = 3, onDay }: Props) {
  const byDay = useMemo(() => {
    const map = new Map<string, Row[]>();
    for (const group of days) map.set(group.day, group.rows);
    return map;
  }, [days]);

  const cells = useMemo(() => monthCells(month, today), [month, today]);
  const title = useMemo(() => monthLong(`${month}-01`), [month]);
  const year = month.slice(0, 4);

  return (
    <div className="cal">
      <div className="cal__head">
        <h2 className="cal__title">
          {title}
          <span className="num">{year}</span>
        </h2>
        <div className="cal__nav">
          <button className="iconbtn" aria-label="Previous month" onClick={() => onMonth(shiftMonth(month, -1))}>
            <CaretLeft size={16} />
          </button>
          <button className="iconbtn" aria-label="Next month" onClick={() => onMonth(shiftMonth(month, 1))}>
            <CaretRight size={16} />
          </button>
        </div>
      </div>

      <div className="cal__wd" aria-hidden="true">
        {["mo", "tu", "we", "th", "fr", "sa", "su"].map((wd) => (
          <span key={wd}>{wd}</span>
        ))}
      </div>

      <div className="cal__grid">
        {cells.map(({ day, inMonth, blank }) => {
          if (blank) return <div className="cal__cell cal__cell--blank" key={day} />;
          const rows = byDay.get(day) ?? [];
          const faces = rows.slice(0, maxFaces);
          const extra = rows.length - faces.length;
          const covered = day >= from && day <= to;
          const className = [
            "cal__cell",
            inMonth && covered ? "" : "cal__cell--out",
            day === today ? "cal__cell--today" : "",
          ]
            .filter(Boolean)
            .join(" ");

          const inner = (
            <>
              <span className="cal__n">{day.slice(8, 10)}</span>
              {faces.length ? (
                <span className="cal__faces">
                  {faces.map((row) => (
                    <span className="cal__face" key={row.k} data-name={row.a}>
                      <Avatar name={row.a} portraits={portraits} size={22} />
                    </span>
                  ))}
                </span>
              ) : null}
              {extra > 0 ? <span className="cal__more">+{extra}</span> : null}
            </>
          );

          if (rows.length === 0) {
            return (
              <div key={day} className={className} data-past={day < today ? "" : undefined}>
                {inner}
              </div>
            );
          }
          return (
            <button
              key={day}
              className={className}
              data-shows=""
              data-past={day < today ? "" : undefined}
              onClick={() => onDay(day)}
              aria-label={`${rows.length} ${rows.length === 1 ? "show" : "shows"} on ${weekdayOf(day)} ${day}`}
            >
              {inner}
            </button>
          );
        })}
      </div>
    </div>
  );
}
