/**
 * The index, as the browser sees it.
 *
 * Two requests, no Worker in the path: `manifest.json` is the map and the only file that changes per build, and
 * a shard's name is the SHA-256 of its own bytes, so it is fetched once and cached forever. Nothing here reads
 * the clock — the window comes from the manifest, because a board whose "today" is the visitor's clock and whose
 * rows are the miner's window disagrees with itself across time zones.
 */

export type Row = {
  /** `artistKey|venueKey|YYYY-MM-DD` — the identity of a show. */
  k: string;
  a: string;
  v: string;
  /** `<slug>-<cc>`: the town this row is published under. */
  c: string;
  /** RFC 3339 with the venue's offset. */
  d: string;
  /** o on sale · c cancelled · x sold out · s postponed */
  st: string;
  u: string;
  src: string[];
  /** [lat, lon] when the source publishes a venue position. */
  g?: [number, number];
};

export type SourceReport = {
  id: string;
  index: string;
  ok: boolean;
  parse_yield?: number;
  last_ok?: string;
  rows?: number;
  note?: string;
};

export type TownEntry = {
  town: string;
  shard: string;
  sha256: string;
  count: number;
  sources: string[];
  built_at: string;
};

export type Manifest = {
  format: string;
  built_at: string;
  window: { from: string; to: string; days: number };
  sources: SourceReport[];
  towns: TownEntry[];
};

export type Town = { id: string; name: string; c: string; lat: number; lon: number; tz: string };

const BASE = (import.meta.env.VITE_DATA_BASE ?? "").replace(/\/$/, "");

export class IndexError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

async function readError(res: Response): Promise<IndexError> {
  // The publisher and `lineup serve` both answer `{"error":{"code","message"}}`; anything else is a proxy.
  try {
    const body = await res.json();
    const err = body?.error;
    if (err?.code) return new IndexError(String(err.code), String(err.message ?? ""));
  } catch {
    /* not JSON */
  }
  return new IndexError(`http_${res.status}`, `${res.status} ${res.statusText}`);
}

async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { cache: "no-cache" });
  if (!res.ok) throw await readError(res);
  return (await res.json()) as T;
}

async function gunzip(buffer: ArrayBuffer): Promise<string> {
  // The shard is `immutable`, so the browser cache holds it and this cost is paid once per build.
  const stream = new Blob([buffer]).stream().pipeThrough(new DecompressionStream("gzip"));
  return await new Response(stream).text();
}

/** One gzipped JSONL shard → rows. A line that does not parse is skipped, never guessed at. */
export async function loadShard(shard: string): Promise<Row[]> {
  const res = await fetch(`${BASE}/${shard.replace(/^\//, "")}`);
  if (!res.ok) throw await readError(res);
  const text = await gunzip(await res.arrayBuffer());
  const rows: Row[] = [];
  for (const line of text.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    try {
      const row = JSON.parse(trimmed) as Row;
      // The nine-key contract: a row without an artist, a venue, a date and a link is not a board row.
      if (row.a && row.v && row.d && row.u) rows.push(row);
    } catch {
      /* skip a malformed line rather than failing the town */
    }
  }
  // The shard is written in merge order, not date order. A board is a calendar, so the sort is the loader's job
  // — and the tie-break is the artist, so two shows on one night still read alphabetically.
  rows.sort((a, b) => (a.d === b.d ? a.a.localeCompare(b.a, "da") : a.d < b.d ? -1 : 1));
  return rows;
}

export async function loadIndex(): Promise<{ manifest: Manifest; towns: Town[] }> {
  const [manifest, townsFile] = await Promise.all([
    fetchJSON<Manifest>("/manifest.json"),
    fetchJSON<{ towns: Town[] }>("/towns.json").catch(() => ({ towns: [] as Town[] })),
  ]);
  if (!Array.isArray(manifest.towns) || manifest.towns.length === 0) {
    throw new IndexError("no_towns", "the manifest lists no towns");
  }
  return { manifest, towns: townsFile.towns ?? [] };
}

/* ── dates: the window is the manifest's, the day is the row's own ─────────────────────────────────── */

const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"];
const MONTHS_LONG = [
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
// Three letters: the date rail is 44–58 px wide, and "wednesday" there collides with the artist column.
const WEEKDAYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

/** `YYYY-MM-DD` from an RFC 3339 stamp, without re-interpreting the venue's offset. */
export function dayOf(stamp: string): string {
  return stamp.slice(0, 10);
}

/** A calendar day as a UTC-noon Date, so weekday arithmetic cannot slip across a time zone. */
export function dateOf(day: string): Date {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d, 12));
}

export function isoDay(date: Date): string {
  return date.toISOString().slice(0, 10);
}

export function addDays(day: string, n: number): string {
  const d = dateOf(day);
  d.setUTCDate(d.getUTCDate() + n);
  return isoDay(d);
}

export function weekdayOf(day: string): string {
  return WEEKDAYS[dateOf(day).getUTCDay()];
}

export function dayNumber(day: string): string {
  return String(dateOf(day).getUTCDate()).padStart(2, "0");
}

export function monthShort(day: string): string {
  return MONTHS[dateOf(day).getUTCMonth()];
}

export function monthLong(day: string): string {
  return MONTHS_LONG[dateOf(day).getUTCMonth()];
}

/** `17 nov 06:12` — the label form: a mono day inside a caps label. */
export function stampLabel(rfc3339: string): string {
  const date = new Date(rfc3339);
  if (Number.isNaN(date.getTime())) return "—";
  const day = `${String(date.getUTCDate()).padStart(2, "0")} ${MONTHS[date.getUTCMonth()]}`;
  const time = `${String(date.getUTCHours()).padStart(2, "0")}:${String(date.getUTCMinutes()).padStart(2, "0")}`;
  return `${day} ${time}`;
}

/** The town's display name: the index's own label when it has one, else a title-cased id. */
export function townLabel(entry: TownEntry, towns: Town[]): string {
  const known = towns.find((t) => t.id === entry.town);
  if (known?.name) return known.name;
  const slug = entry.town.split("-")[0].replace(/_/g, " ");
  return slug.charAt(0).toUpperCase() + slug.slice(1);
}

export function statusLabel(status: string): string | null {
  switch (status) {
    case "c":
      return "cancelled";
    case "x":
      return "sold out";
    case "s":
      return "postponed";
    default:
      return null;
  }
}
