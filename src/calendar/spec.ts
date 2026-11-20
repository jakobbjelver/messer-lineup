/**
 * The calendar spec: which board a feed is for.
 *
 * A subscribed calendar is a URL a client fetches on its own, so the URL has to carry the whole board — town, taste —
 * because there is no session to look up. That is the token: base64url of a small JSON object, nothing secret in it,
 * and deliberately **not** signed.
 *
 * The plan (§7.1) called for base64url + an HMAC. An HMAC proves the parameters were issued by us, which matters when
 * the parameters grant something — and here they grant nothing: the feed is the same public index the board shows, and
 * anyone can already ask for any town, any window, any artist list by typing them into the board. A signature would
 * mean holding a secret on the Worker for no gain, and the project does not take a keyed dependency it cannot justify.
 * What the token does need is to be *strict*: unknown fields, a bad version, a bad date or an unknown town are refused,
 * never guessed at.
 *
 * **The window is optional, and its absence is the point.** A subscription is a URL a client polls for as long as it
 * lives, so a window baked in at subscribe time is a feed that dies: measured 2026-11-20, a valid token whose window
 * had passed answered 0 events while the index held 718 — and the client kept polling it. Absent `f`/`o` the feed
 * follows the published window, which the nightly publish moves. Present, an older token keeps its frozen window, so
 * nothing that was issued stops working.
 */

export type CalendarSpec = {
  /** Format version. A feed URL outlives a deploy, so a change that breaks old tokens must say so here. */
  v: 1;
  /** Town id (`copenhagen-dk`), or `anywhere` — the reserved scope that renders every published town. */
  t: string;
  /** First day, `YYYY-MM-DD`. Absent with `o`: follow the published window. */
  f?: string;
  /** Last day, `YYYY-MM-DD`, inclusive. */
  o?: string;
  /** Artist identity keys. Absent or empty means the whole town. */
  a?: string[];
  /** A live artist list. `a` stays the fallback, so a provider that is down degrades to what the subscriber saw. */
  s?: CalendarSource;
};

export type CalendarSource = {
  /** The only source today. */
  k: "listenbrainz";
  /** The handle. */
  u: string;
};

/** The reserved scope: every published town at once. Never a town id (`<slug>-<cc>`), never a default. */
export const ANYWHERE = "anywhere";

/** The most artist keys a token may carry — one number, shared with the picker's cap. */
export const TASTE_CAP = 200;

const DAY = /^\d{4}-\d{2}-\d{2}$/;

/** Encodes a spec into a token. Throws on anything it cannot represent, because a bad token is a bad link. */
export function encodeSpec(spec: CalendarSpec): string {
  // The cap is enforced here as well as on the way in: a button that writes a token the feed will refuse with a 400 is
  // a broken control, and it would only ever show up on the subscriber's device.
  if (spec.a && spec.a.length > TASTE_CAP) {
    throw new Error(`a feed carries at most ${TASTE_CAP} artists`);
  }
  const json = JSON.stringify(spec);
  return base64urlEncode(new TextEncoder().encode(json));
}

/** Decodes a token, or returns `null` for anything that is not a well-formed spec. */
export function decodeSpec(token: string): CalendarSpec | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(base64urlDecode(token)));
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const spec = parsed as Record<string, unknown>;
  if (spec.v !== 1) return null;
  if (typeof spec.t !== "string" || spec.t.length === 0 || spec.t.length > 64) return null;

  // A window is both ends or neither: half a window is a link that lies about what it shows.
  const hasFrom = spec.f !== undefined;
  const hasTo = spec.o !== undefined;
  if (hasFrom !== hasTo) return null;
  let from: string | undefined;
  let to: string | undefined;
  if (hasFrom) {
    if (!isDay(spec.f) || !isDay(spec.o)) return null;
    if ((spec.f as string) > (spec.o as string)) return null;
    from = spec.f as string;
    to = spec.o as string;
  }

  let artists: string[] | undefined;
  if (spec.a !== undefined) {
    if (!Array.isArray(spec.a) || spec.a.length > TASTE_CAP) return null;
    if (!spec.a.every((key) => typeof key === "string" && key.length > 0 && key.length <= 96)) return null;
    artists = spec.a as string[];
  }

  let source: CalendarSource | undefined;
  if (spec.s !== undefined) {
    if (!spec.s || typeof spec.s !== "object") return null;
    const s = spec.s as Record<string, unknown>;
    if (s.k !== "listenbrainz") return null;
    if (typeof s.u !== "string" || s.u.length === 0 || s.u.length > 64) return null;
    source = { k: "listenbrainz", u: s.u };
  }

  return {
    v: 1,
    t: spec.t,
    ...(from && to ? { f: from, o: to } : {}),
    ...(artists ? { a: artists } : {}),
    ...(source ? { s: source } : {}),
  };
}

/**
 * One show, as a link.
 *
 * A subscribed feed has to fetch the index, because a week of a town is too much to put in a URL. A single event is not:
 * the row is nine short strings, so the whole event travels in the token and the route answers without reading anything.
 * That is why the town's *label* is in here rather than its id — `LOCATION` needs the name a person reads, and looking
 * one up would turn a free route into an index read.
 */
export type EventSpec = {
  v: 1;
  /** The row key: `artist|venue|date`, and the `UID` — the same event in a feed and in a single invite. */
  k: string;
  a: string;
  v2: string;
  d: string;
  u: string;
  n: string;
};

export function encodeEvent(event: EventSpec): string {
  return base64urlEncode(new TextEncoder().encode(JSON.stringify(event)));
}

export function decodeEvent(token: string): EventSpec | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder().decode(base64urlDecode(token)));
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const spec = parsed as Record<string, unknown>;
  if (spec.v !== 1) return null;
  const textField = (value: unknown, max: number): string | null =>
    typeof value === "string" && value.length > 0 && value.length <= max ? value : null;
  const k = textField(spec.k, 128);
  const a = textField(spec.a, 160);
  const v2 = textField(spec.v2, 160);
  const n = textField(spec.n, 80);
  const url = textField(spec.u, 512);
  if (!k || !a || !v2 || !n || !url) return null;
  if (!/^https?:\/\//.test(url)) return null;
  if (typeof spec.d !== "string" || Number.isNaN(new Date(spec.d).getTime())) return null;
  return { v: 1, k, a, v2, d: spec.d, u: url, n };
}

function isDay(value: unknown): boolean {
  if (typeof value !== "string" || !DAY.test(value)) return false;
  const date = new Date(`${value}T12:00:00Z`);
  return !Number.isNaN(date.getTime()) && date.toISOString().slice(0, 10) === value;
}

function base64urlEncode(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function base64urlDecode(token: string): Uint8Array {
  const padded = token.replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(padded + "=".repeat((4 - (padded.length % 4)) % 4));
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
  return bytes;
}
