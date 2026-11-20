/**
 * Distance, and the one rule that keeps it honest: no position, no number.
 *
 * A row carries a venue position only when its source published one (two thirds of the Copenhagen shard, half of
 * Stockholm's, measured 2026-11-19). A distance is therefore always *optional*, and a row without a position prints
 * nothing rather than a guess — the same rule the Go side follows when it marshals `g`.
 *
 * The other end of the measurement matters as much. A distance from the middle of a town is not a distance from a
 * person, so the board only computes against a **device fix**: the visitor asked for it, it never leaves memory, and
 * the town's own point (the mean of its venues) is used for *choosing* a town, never for printing kilometres.
 */

export type Point = { lat: number; lon: number };

// The same radius the edge's `/where` uses (`internal/server/where.go`): one product, one earth.
const EARTH_KM = 6371;

/** Great-circle distance in kilometres. */
export function distanceKm(from: Point, to: Point): number {
  const rad = Math.PI / 180;
  const dLat = (to.lat - from.lat) * rad;
  const dLon = (to.lon - from.lon) * rad;
  const a =
    Math.sin(dLat / 2) ** 2 + Math.cos(from.lat * rad) * Math.cos(to.lat * rad) * Math.sin(dLon / 2) ** 2;
  return 2 * EARTH_KM * Math.asin(Math.min(1, Math.sqrt(a)));
}

/**
 * The number the row prints, without its unit: `0.4`, `2.4`, `37`.
 *
 * One decimal below 10 km because that is the range where a person reads the difference, whole kilometres above it.
 * A distance that would round to zero says `< 0.1` instead of `0.0`, which reads as "here" and is not a distance.
 */
export function formatKm(km: number): string {
  if (!Number.isFinite(km) || km < 0) return "—";
  if (km < 0.1) return "< 0.1";
  if (km < 10) return km.toFixed(1);
  return String(Math.round(km));
}

/** A row's position, or `null` when the pair is missing or cannot be a place. */
export function pointOf(row: { g?: [number, number] }): Point | null {
  const pair = row.g;
  if (!Array.isArray(pair) || pair.length !== 2) return null;
  const [lat, lon] = pair;
  if (!Number.isFinite(lat) || !Number.isFinite(lon)) return null;
  if (lat < -90 || lat > 90 || lon < -180 || lon > 180) return null;
  // (0, 0) is the Gulf of Guinea, not a venue: a zero pair is a source that did not answer, not a position.
  if (lat === 0 && lon === 0) return null;
  return { lat, lon };
}

/** A town's own point, which a town file publishes as the mean of its venues — used to choose, never to print. */
export function townPoint(town: { lat: number; lon: number }): Point | null {
  return pointOf({ g: [town.lat, town.lon] });
}

/** The published town nearest a point, for the moment a device fix arrives. */
export function nearestTown<T extends { lat: number; lon: number }>(towns: T[], from: Point): { town: T; km: number } | null {
  let best: { town: T; km: number } | null = null;
  for (const town of towns) {
    const point = townPoint(town);
    if (!point) continue;
    const km = distanceKm(from, point);
    if (!best || km < best.km) best = { town, km };
  }
  return best;
}
