import type { MouseEvent } from "react";
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
  /** `k` → the town it is in, for a page that spans towns (the artist route). */
  townOf?: Map<string, string>;
  /** Whether the artist name is a link to that artist's page. Off on the page itself. */
  linkArtist?: boolean;
};

/**
 * The board: month headers, then one row per show, oldest first.
 *
 * A row is a link. It carries no portrait yet — ADR-002 decides where those come from, and until it does the
 * flat scrim the stylesheet draws *is* the row, not a placeholder for one.
 */
export default function Board({ rows, tasteKeys, newKeys, townOf, linkArtist = true }: Props) {
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
        // The whole row opens the show, except on a control — the artist link and the arrow own their own clicks.
        const openRow = (event: MouseEvent<HTMLDivElement>) => {
          if ((event.target as HTMLElement).closest("a,button")) return;
          window.open(row.u, "_blank", "noopener,noreferrer");
        };

        return (
          <div key={row.k}>
            {header ? <div className="month">{header}</div> : null}
            <div
              className="row"
              data-status={row.st}
              data-mine={mine ? "" : undefined}
              onClick={openRow}
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
                {linkArtist ? (
                  <a className="artist artist--link" href={`/a/${Key(row.a)}`}>
                    {row.a}
                  </a>
                ) : (
                  <span className="artist">{row.a}</span>
                )}
                <span className="meta">
                  <span className="venue">{row.v}</span>
                  {townOf?.get(row.k) ? (
                    <>
                      <span className="dot">·</span>
                      <span className="venue town">{townOf.get(row.k)}</span>
                    </>
                  ) : null}
                  {status ? (
                    <>
                      <span className="dot">·</span>
                      <span className="km">{status}</span>
                    </>
                  ) : null}
                </span>
                {mine ? <span className="aff" style={{ width: "100%" }} /> : null}
              </span>
              <a
                className="go"
                href={row.u}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`Open ${row.a} at ${row.v}, ${weekdayOf(day)} ${dayNumber(day)} ${monthShort(day)}${status ? ` (${status})` : ""}`}
              >
                <ArrowUpRight />
              </a>
            </div>
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
