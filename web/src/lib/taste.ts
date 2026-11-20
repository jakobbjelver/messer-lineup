/**
 * Taste, without an account.
 *
 * The product's second-ranked taste input is "type or paste artists", and it is the only one that needs no
 * backend at all: the list becomes identity keys in the browser and the board matches them against the index it
 * already has. Nothing about it is stored server-side — it lives in the URL (`?a=`), which also means a
 * personalised board is a shareable link.
 *
 * A provider link (Spotify, Last.fm) resolves to the same shape — a set of keys — which is why this file does not
 * care where a list came from.
 */

import { Key } from "./key";

export type TasteEntry = { key: string; name: string };

const SPLIT = /[\n\r,;\t]+/;
/** Bullets, numbering and quote marks a pasted list arrives wrapped in. */
const LEADING = /^\s*(?:[-*•·]|\d+[.)])\s*/;

/**
 * A pasted list → entries. Order is preserved and duplicates collapse on the key, so "Björk" and "bjork" are
 * one artist, which is the same rule the merge uses on the server.
 */
export function parseTasteList(text: string): TasteEntry[] {
  const seen = new Set<string>();
  const out: TasteEntry[] = [];
  for (const raw of text.split(SPLIT)) {
    const name = raw
      .replace(LEADING, "")
      .replace(/^["'“”]+|["'“”]+$/g, "")
      .trim();
    if (!name) continue;
    // A header row from a CSV is not an artist; it is also not worth guessing about, so the rule is structural:
    // a line with nothing but letters that says "artist", "name" or "artists" is dropped.
    if (/^(artist|artists|name|title)$/i.test(name)) continue;
    const key = Key(name);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push({ key, name });
  }
  return out;
}

/**
 * `?n=key:Name,key:Name` → the spellings a key cannot carry.
 *
 * A key is a slug, so `a=bjork` reads back as "Bjork". Names that differ from their own key are written alongside as
 * `n=`, and only those: a list whose names are plain ("bad-bunny") costs nothing extra, and a shared link spells
 * "Björk" correctly on a device that has never seen it.
 *
 * The decode is guarded per pair, because this runs while the module is being evaluated: a link cut mid-escape — which
 * is exactly what a chat app's URL truncation produces — used to throw `URIError` out of the module and leave the page
 * blank. A pair that cannot be read is dropped; the key in `a` still identifies the artist.
 */
export function readNamesParam(search: string): TasteEntry[] {
  const raw = new URLSearchParams(search).get("n");
  if (!raw) return [];
  const out: TasteEntry[] = [];
  for (const pair of raw.split(",")) {
    const [key, name] = pair.split(":");
    if (!key || !name) continue;
    let spelled: string;
    try {
      spelled = decodeURIComponent(name).trim();
    } catch {
      continue;
    }
    if (!spelled) continue;
    out.push({ key: key.trim(), name: spelled });
  }
  return out;
}

/** `?a=key,key` → keys. A malformed parameter yields nothing rather than throwing. */
export function readTasteParam(search: string): string[] {
  const raw = new URLSearchParams(search).get("a");
  if (!raw) return [];
  return [...new Set(raw.split(",").map((k) => k.trim()).filter(Boolean))];
}

/**
 * Writes the taste set into the URL — keys in `a`, and in `n` only the names the keys cannot spell. The parameter
 * is dropped entirely when the list is empty, so clearing taste leaves no residue.
 */
export function writeTasteParam(entries: TasteEntry[], current: string): string {
  const url = new URL(current);
  if (entries.length === 0) {
    url.searchParams.delete("a");
    url.searchParams.delete("n");
    return `${url.pathname}${url.search}${url.hash}`;
  }
  url.searchParams.set("a", entries.map((entry) => entry.key).join(","));
  const named = entries.filter((entry) => entry.name && entry.name !== labelFor(entry.key));
  if (named.length) url.searchParams.set("n", named.map((e) => `${e.key}:${encodeURIComponent(e.name)}`).join(","));
  else url.searchParams.delete("n");
  return `${url.pathname}${url.search}${url.hash}`;
}

/** A display name for a key that arrived by URL and has no label of its own. */
export function labelFor(key: string): string {
  return key
    .split("-")
    .map((part) => (part.length <= 2 ? part : part.charAt(0).toUpperCase() + part.slice(1)))
    .join(" ");
}

/**
 * The names a list arrives with, keyed by identity key.
 *
 * A URL carries keys because keys are the identity, but a key is a slug: read back on its own it
 * turns "Björk" into "Bjork". The picker supplies the spelling, `storage.ts` remembers the list, and
 * a key with no label falls back to a readable form rather than to nothing.
 */
export function toEntries(keys: string[], known: TasteEntry[] = []): TasteEntry[] {
  const names = new Map(known.map((entry) => [entry.key, entry.name]));
  return keys.map((key) => ({ key, name: names.get(key) ?? labelFor(key) }));
}
