import { useEffect, useMemo, useRef, useState } from "react";

/**
 * A date range, on the canon's terms.
 *
 * There is no date block in the messer-ui registry, so this is the first copy and the prototype for one: square cells
 * on the 28px control height, hairlines instead of shadows, hover as one surface step, the selected range as an accent
 * tint with a 2px accent rule at each end, and arrow keys that move by day, week and month. Two months are shown
 * because a trip is usually planned across a month boundary.
 *
 * The browser's own `<input type="date">` is not an option: it opens the OS's picker, which ignores every token in the
 * design and cannot express a range at all.
 */

const WEEKDAYS = ["mo", "tu", "we", "th", "fr", "sa", "su"];
const MONTHS = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

type Props = {
  from: string;
  to: string;
  /** The window the index publishes: a day outside it has no shows, so it cannot be picked. */
  bounds: { from: string; to: string };
  onChange: (from: string, to: string) => void;
};

export default function DateRange({ from, to, bounds, onChange }: Props) {
  const wholeSeason = from === bounds.from && to === bounds.to;
  const [start, setStart] = useState(wholeSeason ? "" : from);
  const [end, setEnd] = useState(wholeSeason ? "" : to);
  const [pending, setPending] = useState<string | null>(null);
  const [cursor, setCursor] = useState(() => startOfMonth(from));
  const grid = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const whole = from === bounds.from && to === bounds.to;
    setStart(whole ? "" : from);
    setEnd(whole ? "" : to);
    setCursor(startOfMonth(from));
  }, [from, to, bounds.from, bounds.to]);

  // Focus moves after the render that moved the cursor, never inside the keydown: the grid is remounted when the
  // month changes, and a frame-old node loses focus the moment React replaces it.
  useEffect(() => {
    if (!pending) return;
    grid.current?.querySelector<HTMLElement>(`[data-day="${pending}"]`)?.focus();
    setPending(null);
  }, [pending]);

  const months = useMemo(() => {
    const first = startOfMonth(cursor);
    return [first, addMonths(first, 1)];
  }, [cursor]);

  const pick = (day: string) => {
    if (!start || (start && end) || day < start) {
      setStart(day);
      setEnd("");
      return;
    }
    setEnd(day);
    onChange(start, day);
  };

  // Arrow keys move the focused day; PageUp/PageDown move by month. Focus follows the cursor day.
  const onKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const moves: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
    if (event.key in moves) {
      event.preventDefault();
      const focused = document.activeElement as HTMLElement | null;
      const current = focused?.dataset?.day ?? start;
      const next = shiftDay(current, moves[event.key]);
      if (next < bounds.from) return;
      if (next > bounds.to) return;
      setCursor(startOfMonth(next));
      setPending(next);
      return;
    }
    if (event.key === "PageUp" || event.key === "PageDown") {
      event.preventDefault();
      setCursor((value) => addMonths(value, event.key === "PageUp" ? -1 : 1));
    }
  };

  return (
    <div className="daterange">
      <div className="daterange__head">
        <button
          className="daterange__nav"
          onClick={() => setCursor((value) => addMonths(value, -1))}
          disabled={months[0] <= startOfMonth(bounds.from)}
          aria-label="Previous month"
        >
          <Chevron direction="left" />
        </button>
        <span className="lab">
          {start && end ? (
            <>
              <span className="num">{daysBetween(start, end) + 1}</span> days chosen
            </>
          ) : (
            "Pick the first day"
          )}
        </span>
        <button
          className="daterange__nav"
          onClick={() => setCursor((value) => addMonths(value, 1))}
          disabled={months[1] >= startOfMonth(bounds.to)}
          aria-label="Next month"
        >
          <Chevron direction="right" />
        </button>
      </div>

      <div className="daterange__months" ref={grid} onKeyDown={onKeyDown}>
        {months.map((month) => (
          <div className="daterange__month" key={month}>
            <div className="daterange__title">
              {MONTHS[Number(month.slice(5, 7)) - 1]} <span className="num">{month.slice(0, 4)}</span>
            </div>
            <div className="daterange__grid" role="grid">
              {WEEKDAYS.map((weekday) => (
                <span className="daterange__wd" key={weekday}>
                  {weekday}
                </span>
              ))}
              {cellsOf(month).map((day, index) => {
                if (!day) return <span className="daterange__pad" key={`pad-${index}`} />;
                const outside = day < bounds.from || day > bounds.to;
                const selected = day === start || day === end;
                const within = !!start && !!end && day > start && day < end;
                return (
                  <button
                    key={day}
                    type="button"
                    data-day={day}
                    className="daterange__day"
                    data-selected={selected ? "" : undefined}
                    data-within={within ? "" : undefined}
                    disabled={outside}
                    aria-pressed={selected}
                    aria-label={day}
                    tabIndex={day === (start || bounds.from) ? 0 : -1}
                    onClick={() => pick(day)}
                  >
                    <span className="num">{Number(day.slice(8, 10))}</span>
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      <div className="daterange__foot">
        <button
          className="btn"
          onClick={() => {
            setStart(bounds.from);
            setEnd(bounds.to);
            onChange(bounds.from, bounds.to);
          }}
        >
          Whole season
        </button>
        <span className="lab" style={{ marginLeft: "auto" }}>
          {end ? `${start} → ${end}` : "pick a last day"}
        </span>
      </div>
    </div>
  );
}

/* ── days: plain strings, no Date arithmetic surprises ─────────────────────────────────────────────────── */

function startOfMonth(day: string): string {
  return `${day.slice(0, 7)}-01`;
}

function addMonths(month: string, count: number): string {
  const year = Number(month.slice(0, 4));
  const index = Number(month.slice(5, 7)) - 1 + count;
  const y = year + Math.floor(index / 12);
  const m = ((index % 12) + 12) % 12;
  return `${String(y).padStart(4, "0")}-${String(m + 1).padStart(2, "0")}-01`;
}

function shiftDay(day: string, days: number): string {
  const date = new Date(`${day}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() + days);
  return date.toISOString().slice(0, 10);
}

function daysBetween(from: string, to: string): number {
  const a = new Date(`${from}T12:00:00Z`).getTime();
  const b = new Date(`${to}T12:00:00Z`).getTime();
  return Math.max(0, Math.round((b - a) / 86_400_000));
}

/** Six weeks of cells, Monday first, with `null` where the month has not started or has ended. */
function cellsOf(month: string): (string | null)[] {
  const first = new Date(`${month}T12:00:00Z`);
  const offset = (first.getUTCDay() + 6) % 7; // Monday = 0
  const length = new Date(Date.UTC(first.getUTCFullYear(), first.getUTCMonth() + 1, 0)).getUTCDate();
  const cells: (string | null)[] = Array.from({ length: offset }, () => null);
  for (let day = 1; day <= length; day++) {
    cells.push(`${month.slice(0, 7)}-${String(day).padStart(2, "0")}`);
  }
  while (cells.length % 7 !== 0 || cells.length < 42) cells.push(null);
  return cells.slice(0, 42);
}

function Chevron({ direction }: { direction: "left" | "right" }) {
  return (
    <svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
      <path
        d={
          direction === "left"
            ? "M165.66,202.34a8,8,0,0,1-11.32,11.32l-80-80a8,8,0,0,1,0-11.32l80-80a8,8,0,0,1,11.32,11.32L91.31,128Z"
            : "M181.66,133.66l-80,80a8,8,0,0,1-11.32-11.32L164.69,128,90.34,53.66a8,8,0,0,1,11.32-11.32l80,80A8,8,0,0,1,181.66,133.66Z"
        }
      />
    </svg>
  );
}
