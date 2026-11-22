/**
 * The sixteen words, in one place for both runtimes.
 *
 * The web picker and the Worker's `X-WR-CALDESC` both need the labels, and the feed's token carries the ids. Keeping the
 * list here rather than in `web/` is what stops the two from drifting: the Worker cannot import a module that reads
 * `import.meta.env`, and a second copy of sixteen strings is exactly the kind of list that gets one edit.
 */

export const GENRES = [
  { id: "rock", label: "Rock" },
  { id: "metal", label: "Metal" },
  { id: "punk", label: "Punk" },
  { id: "pop", label: "Pop" },
  { id: "electronic", label: "Electronic" },
  { id: "hip-hop", label: "Hip-hop" },
  { id: "folk", label: "Folk" },
  { id: "jazz", label: "Jazz" },
  { id: "soul", label: "Soul" },
  { id: "blues", label: "Blues" },
  { id: "country", label: "Country" },
  { id: "classical", label: "Classical" },
  { id: "reggae", label: "Reggae" },
  { id: "world", label: "World" },
  { id: "experimental", label: "Experimental" },
  { id: "soundtrack", label: "Soundtrack" },
] as const;

export type GenreId = (typeof GENRES)[number]["id"];

/** Six words is a taste; sixteen is a dump. The picker's cap and the token's cap are the same number. */
export const GENRE_CAP = 6;

const LABELS = new Map<string, string>(GENRES.map((genre) => [genre.id, genre.label]));

export function genreLabel(id: string): string {
  return LABELS.get(id) ?? id;
}
