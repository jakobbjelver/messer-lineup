/**
 * RFC 5545: the board as a calendar a client can subscribe to.
 *
 * Deliberate choices, because a feed is read by software rather than by a person:
 *
 *   - **Times are UTC.** The index carries the venue's offset (a Copenhagen show is `+01:00`), which is what makes the
 *     local time right; converting once here means every calendar client renders it in the reader's own zone without
 *     a `VTIMEZONE` block to keep in sync.
 *   - **`UID` is the row key.** Subscribed calendars re-fetch; an event whose UID changes is a duplicate, and the row
 *     key is stable across sweeps by construction (`artist|venue|date`).
 *   - **A three-hour default duration.** The index knows when a show starts and nothing about when it ends — the plan's
 *     row contract has no end field and inventing one would be a lie in the data. Three hours is a calendar block, and
 *     `DTEND` is what makes a client draw one at all.
 *   - **Line folding and escaping are not optional.** A comma in a venue name that is not escaped truncates the
 *     `LOCATION`; a line over 75 octets without folding is rejected by strict parsers.
 */

export type CalendarRow = {
  k: string;
  a: string;
  v: string;
  d: string;
  u: string;
  st: string;
};

type Meta = {
  townId: string;
  townLabel: string;
  builtAt: string;
  /** Host the feed is served from, for `URL:` lines that point back at the board. */
  origin: string;
};

/** Three hours: a block a client draws, not a claim about the running time. */
const DEFAULT_DURATION_MS = 3 * 60 * 60 * 1000;

export function buildIcs(rows: CalendarRow[], meta: Meta): string {
  const stamp = toStamp(new Date(meta.builtAt)) ?? toStamp(new Date())!;
  const lines: string[] = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Messer//Lineup//EN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    `X-WR-CALNAME:${escapeText(`Messer Lineup — ${meta.townLabel}`)}`,
    `X-WR-CALDESC:${escapeText(`Shows in ${meta.townLabel}, from the Messer Lineup index.`)}`,
    "REFRESH-INTERVAL;VALUE=DURATION:PT6H",
    "X-PUBLISHED-TTL:PT6H",
  ];

  for (const row of rows) {
    const event = vevent(row, meta, stamp);
    if (event) lines.push(...event);
  }

  lines.push("END:VCALENDAR");
  return finish(lines);
}

/**
 * One show as a calendar file: the same `VEVENT` the feed would write for it, and nothing else. Same `UID`, so a show
 * added from a row and the same show arriving through a subscription are one event to the client, not two.
 */
export function buildEvent(row: CalendarRow, meta: Meta): string {
  const stamp = toStamp(new Date(meta.builtAt)) ?? toStamp(new Date())!;
  const event = vevent(row, meta, stamp) ?? [];
  return finish([
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "PRODID:-//Messer//Lineup//EN",
    "CALSCALE:GREGORIAN",
    "METHOD:PUBLISH",
    ...event,
    "END:VCALENDAR",
  ]);
}

/** The lines of one event, or `null` for a row whose date cannot be read — which is not an event. */
function vevent(row: CalendarRow, meta: Meta, stamp: string): string[] | null {
  const start = new Date(row.d);
  if (Number.isNaN(start.getTime())) return null;
  const end = new Date(start.getTime() + DEFAULT_DURATION_MS);
  const status = row.st === "c" ? "CANCELLED" : "CONFIRMED";
  return [
    "BEGIN:VEVENT",
    `UID:${escapeText(`${row.k}@lineup.messer.wtf`)}`,
    `DTSTAMP:${stamp}`,
    `DTSTART:${toStamp(start)!}`,
    `DTEND:${toStamp(end)!}`,
    `SUMMARY:${escapeText(`${row.a} @ ${row.v}`)}`,
    `LOCATION:${escapeText(`${row.v}, ${meta.townLabel}`)}`,
    `URL:${row.u}`,
    `STATUS:${status}`,
    `CATEGORIES:${escapeText("Messer Lineup")}`,
    "TRANSP:OPAQUE",
    "END:VEVENT",
  ];
}

// CRLF, folded at 75 octets, and no trailing newline beyond the final CRLF the spec requires.
function finish(lines: string[]): string {
  return lines.map(fold).join("\r\n") + "\r\n";
}

/** `YYYYMMDDTHHMMSSZ`, the UTC form every client understands without a VTIMEZONE. */
function toStamp(date: Date): string | null {
  if (Number.isNaN(date.getTime())) return null;
  return date.toISOString().replace(/[-:]/g, "").replace(/\.\d{3}/, "");
}

/** RFC 5545 §3.3.11: backslash, semicolon, comma and newline are escaped; the newline becomes a literal `\n`. */
function escapeText(value: string): string {
  return value
    .replace(/\\/g, "\\\\")
    .replace(/;/g, "\\;")
    .replace(/,/g, "\\,")
    .replace(/\r?\n/g, "\\n");
}

/** RFC 5545 §3.1: physical lines are at most 75 octets; continuations start with a single space. */
function fold(line: string): string {
  const bytes = new TextEncoder().encode(line);
  if (bytes.length <= 75) return line;
  const out: string[] = [];
  let current = "";
  let size = 0;
  for (const char of line) {
    const width = new TextEncoder().encode(char).length;
    // 74, not 75: the continuation space is part of the limit
    if (size + width > 74) {
      out.push(current);
      current = "";
      size = 0;
    }
    current += char;
    size += width;
  }
  if (current) out.push(current);
  return out.join("\r\n ");
}
