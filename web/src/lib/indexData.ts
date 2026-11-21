import { Key } from "./key";
import { ANYWHERE } from "./calendar";

export { ANYWHERE };

/**
 * The index, as the browser sees it.
 *
 * Two requests, no Worker in the path: `manifest.json` is the map and the only file that changes per
 * build, and a shard's name is the SHA-256 of its own bytes, so it is fetched once and cached forever.
 * Nothing here reads the clock — a board whose "today" is the visitor's clock and whose rows are the
 * miner's window disagrees with itself across time zones.
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

/** A row that knows which town published it, for the artist page. */
export type ArtistShow = Row & { town: string };

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
  sources: { id: string; index: string; ok: boolean; rows?: number }[];
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
  const stream = new Blob([buffer]).stream().pipeThrough(new DecompressionStream("gzip"));
  return await new Response(stream).text();
}

/**
 * One gzipped JSONL shard → rows, sorted by start.
 *
 * The shards are content-addressed and immutable, so the browser cache is the session cache: a second
 * town switch costs a conditional request at most, and this map makes the common case free.
 */
const shards = new Map<string, Promise<Row[]>>();

export function loadShard(shard: string): Promise<Row[]> {
  const key = shard.replace(/^\//, "");
  const cached = shards.get(key);
  if (cached) return cached;
  const pending = (async () => {
    const res = await fetch(`${BASE}/${key}`);
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
    rows.sort(byStart);
    return rows;
  })();
  shards.set(key, pending);
  pending.catch(() => shards.delete(key));
  return pending;
}

/** A start as an instant. The stamp carries the venue's offset, so comparing the strings is wrong across zones. */
function startOf(row: Row): number {
  const ms = Date.parse(row.d);
  return Number.isNaN(ms) ? Number.POSITIVE_INFINITY : ms;
}

/** Earliest first; then the artist, so a day reads consistently; then the key, so the order is total. */
function byStart(a: Row, b: Row): number {
  const left = startOf(a);
  const right = startOf(b);
  if (left !== right) return left < right ? -1 : 1;
  if (a.a !== b.a) return a.a.localeCompare(b.a, "da");
  return a.k < b.k ? -1 : a.k > b.k ? 1 : 0;
}

/**
 * The rows for a scope: one town's shard, or every shard at once for `anywhere`.
 *
 * `anywhere` is the artist route's own data path — read every shard, dedupe by row key, sort — which is why the scope
 * needs no published "world" shard. Two towns 40 km apart can publish the same show under the same key; the first
 * entry in the manifest keeps it, so the label on the row is a decision rather than a race.
 */
export function loadBoard(manifest: Manifest, scope: string): Promise<Row[]> {
  if (scope !== ANYWHERE) {
    const entry = manifest.towns.find((candidate) => candidate.town === scope) ?? manifest.towns[0];
    return entry ? loadShard(entry.shard) : Promise.resolve([]);
  }
  return Promise.all(manifest.towns.map((entry) => loadShard(entry.shard))).then((groups) => {
    const seen = new Set<string>();
    const rows: Row[] = [];
    for (const row of groups.flat()) {
      if (seen.has(row.k)) continue;
      seen.add(row.k);
      rows.push(row);
    }
    return rows.sort(byStart);
  });
}

const index = { pending: null as Promise<{ manifest: Manifest; towns: Town[] }> | null };

export function loadIndex(): Promise<{ manifest: Manifest; towns: Town[] }> {
  if (index.pending) return index.pending;
  const pending = (async () => {
    const [manifest, townsFile] = await Promise.all([
      fetchJSON<Manifest>("/manifest.json"),
      fetchJSON<{ towns: Town[] }>("/towns.json").catch(() => ({ towns: [] as Town[] })),
    ]);
    if (!Array.isArray(manifest.towns) || manifest.towns.length === 0) {
      throw new IndexError("no_towns", "the manifest lists no towns");
    }
    return { manifest, towns: townsFile.towns ?? [] };
  })();
  index.pending = pending;
  pending.catch(() => {
    index.pending = null;
  });
  return pending;
}

/**
 * Every dated show for one artist, across every published town.
 *
 * The index is sharded per town, so "across the reach" means reading every shard — two towns today, and
 * the page says which town each date is in rather than pretending the board's town is the only one. A
 * shard that fails to load is reported, not swallowed: an artist page missing one town is a wrong
 * answer, not a smaller one.
 */
export async function loadArtistRows(manifest: Manifest, key: string): Promise<ArtistShow[]> {
  const groups = await Promise.all(
    manifest.towns.map(async (entry) => {
      const rows = await loadShard(entry.shard);
      return rows.filter((row) => Key(row.a) === key).map((row) => ({ ...row, town: entry.town }));
    }),
  );
  const seen = new Set<string>();
  const shows: ArtistShow[] = [];
  for (const row of groups.flat()) {
    if (seen.has(row.k)) continue;
    seen.add(row.k);
    shows.push(row);
  }
  shows.sort((a, b) => (a.d === b.d ? a.town.localeCompare(b.town) : a.d < b.d ? -1 : 1));
  return shows;
}

/* ── dates: the day is the row's own ─────────────────────────────────────────────────────────────── */

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
const WEEKDAYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"];

/** `YYYY-MM-DD` from an RFC 3339 stamp, without re-interpreting the venue's offset. */
export function dayOf(stamp: string): string {
  return stamp.slice(0, 10);
}

/** A calendar day as a UTC-noon Date, so weekday arithmetic cannot slip across a time zone. */
export function dateOf(day: string): Date {
  const [y, m, d] = day.split("-").map(Number);
  return new Date(Date.UTC(y!, m! - 1, d!, 12));
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
  return WEEKDAYS[dateOf(day).getUTCDay()] ?? "";
}

export function dayNumber(day: string): string {
  return String(dateOf(day).getUTCDate()).padStart(2, "0");
}

export function monthShort(day: string): string {
  return MONTHS[dateOf(day).getUTCMonth()] ?? "";
}

export function monthLong(day: string): string {
  return MONTHS_LONG[dateOf(day).getUTCMonth()] ?? "";
}

/** `17 nov 06:12` — a mono day inside a small label. */
export function stampLabel(rfc3339: string): string {
  const date = new Date(rfc3339);
  if (Number.isNaN(date.getTime())) return "—";
  const day = `${String(date.getUTCDate()).padStart(2, "0")} ${MONTHS[date.getUTCMonth()]}`;
  const time = `${String(date.getUTCHours()).padStart(2, "0")}:${String(date.getUTCMinutes()).padStart(2, "0")}`;
  return `${day} ${time}`;
}

/** The town's display name: the index's own label when it has one, else a title-cased id. */
export function townLabel(entry: { town: string }, towns: Town[]): string {
  const known = towns.find((t) => t.id === entry.town);
  if (known?.name) return known.name;
  const slug = entry.town.split("-")[0]!.replace(/_/g, " ");
  return slug.charAt(0).toUpperCase() + slug.slice(1);
}

export function statusLabel(status: string): string | null {
  switch (status) {
    case "c":
      return "cancelled";
    case "x":
      return "postponed";
    case "s":
      return "sold out";
    default:
      return null;
  }
}
