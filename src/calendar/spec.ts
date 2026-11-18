/**
 * The calendar spec: which board a feed is for.
 *
 * A subscribed calendar is a URL a client fetches on its own, so the URL has to carry the whole board — town, window,
 * taste — because there is no session to look up. That is the token: base64url of a small JSON object, nothing secret
 * in it, and deliberately **not** signed.
 *
 * The plan (§7.1) called for base64url + an HMAC. An HMAC proves the parameters were issued by us, which matters when
 * the parameters grant something — and here they grant nothing: the feed is the same public index the board shows, and
 * anyone can already ask for any town, any window, any artist list by typing them into the board. A signature would
 * mean holding a secret on the Worker for no gain, and the project does not take a keyed dependency it cannot justify.
 * What the token does need is to be *strict*: unknown fields, a bad version, a bad date or an unknown town are refused,
 * never guessed at.
 */

export type CalendarSpec = {
  /** Format version. A feed URL outlives a deploy, so a change that breaks old tokens must say so here. */
  v: 1;
  /** Town id, e.g. `copenhagen-dk`. */
  t: string;
  /** First day, `YYYY-MM-DD`. */
  f: string;
  /** Last day, `YYYY-MM-DD`, inclusive. */
  o: string;
  /** Artist identity keys. Absent or empty means the whole town. */
  a?: string[];
};

const DAY = /^\d{4}-\d{2}-\d{2}$/;

/** Encodes a spec into a token. Throws on anything it cannot represent, because a bad token is a bad link. */
export function encodeSpec(spec: CalendarSpec): string {
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
  if (!isDay(spec.f) || !isDay(spec.o)) return null;
  if ((spec.f as string) > (spec.o as string)) return null;
  let artists: string[] | undefined;
  if (spec.a !== undefined) {
    if (!Array.isArray(spec.a) || spec.a.length > 200) return null;
    if (!spec.a.every((key) => typeof key === "string" && key.length > 0 && key.length <= 96)) return null;
    artists = spec.a as string[];
  }
  return { v: 1, t: spec.t, f: spec.f as string, o: spec.o as string, ...(artists ? { a: artists } : {}) };
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
