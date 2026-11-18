import { ArrowUpRight } from "./icons";
import { Key } from "../lib/key";
import { dayNumber, dayOf, monthShort, statusLabel, weekdayOf } from "../lib/indexData";
import type { Row } from "../lib/indexData";

type Props = {
  rows: Row[];
  /** The visitor's taste, as identity keys. A row whose artist is in this set is "yours". */
  tasteKeys?: Set<string>;
  /** Shows that were not on the board at this browser's last visit. */
  newKeys?: Set<string>;
};

/**
 * The board: month headers, then one row per show, oldest first.
 *
 * A row is a link. It carries no portrait yet — ADR-002 decides where those come from, and until it does the
 * flat scrim the stylesheet draws *is* the row, not a placeholder for one.
 */
export default function Board({ rows, tasteKeys, newKeys }: Props) {
  let month = "";
  const hasTaste = (tasteKeys?.size ?? 0) > 0;

  return (
    <div>
      {rows.map((row) => {
        const day = dayOf(row.d);
        const monthName = monthLongUpper(day);
        const header = monthName !== month ? monthName : null;
        if (header) month = monthName;
        const status = statusLabel(row.st);
        const mine = hasTaste && tasteKeys?.has(Key(row.a));
        const isNew = newKeys?.has(row.k) ?? false;

        return (
          <div key={row.k}>
            {header ? <div className="month">{header}</div> : null}
            <a
              className="row"
              href={row.u}
              target="_blank"
              rel="noopener noreferrer"
              data-status={row.st}
              data-mine={mine ? "" : undefined}
              aria-label={`${row.a} at ${row.v}, ${weekdayOf(day)} ${dayNumber(day)} ${monthShort(day)}${status ? ` (${status})` : ""}`}
            >
              <span className="shot" />
              {isNew ? <span className="new" aria-label="new since your last visit" /> : null}
              <span className="when">
                <span className="wd">{weekdayOf(day)}</span>
                <span className="dd">
                  {dayNumber(day)} {monthShort(day)}
                </span>
              </span>
              <span className="who">
                <span className="artist">{row.a}</span>
                <span className="meta">
                  <span className="venue">{row.v}</span>
                  {status ? (
                    <>
                      <span className="dot">·</span>
                      <span className="km">{status}</span>
                    </>
                  ) : null}
                </span>
                {mine ? <span className="aff" style={{ width: "100%" }} /> : null}
              </span>
              <span className="go">
                <ArrowUpRight />
              </span>
            </a>
          </div>
        );
      })}
    </div>
  );
}

function monthLongUpper(day: string): string {
  const names = [
    "JANUARY",
    "FEBRUARY",
    "MARCH",
    "APRIL",
    "MAY",
    "JUNE",
    "JULY",
    "AUGUST",
    "SEPTEMBER",
    "OCTOBER",
    "NOVEMBER",
    "DECEMBER",
  ];
  return names[new Date(`${day}T12:00:00Z`).getUTCMonth()] ?? "";
}
