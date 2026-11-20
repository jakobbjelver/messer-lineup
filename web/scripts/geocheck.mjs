#!/usr/bin/env node
/**
 * The distance contract, checked without a browser.
 *
 * Three things can go wrong in a distance and none of them throws: a wrong radius, a wrong sign, a pair that is not a
 * place. So this asserts the numbers against published great-circle distances, the formatter against the widths the
 * row can afford, and `pointOf` against the pairs a source actually emits (`[0, 0]`, a short array, a missing field).
 *
 * The radius is the edge's own (`internal/server/where.go`, 6 371 km), so a town chosen by the board and a town
 * chosen by `/where` agree — two radii in one product is a drift nobody would ever notice.
 *
 * Run with `make geocheck`.
 */

import { distanceKm, formatKm, nearestTown, pointOf } from "../src/lib/geo.ts";

const CASES = [
  // name, from, to, expectation in km, tolerance
  ["copenhagen → aarhus", { lat: 55.6761, lon: 12.5683 }, { lat: 56.1629, lon: 10.2039 }, 157.1, 1.5],
  ["copenhagen → stockholm", { lat: 55.6761, lon: 12.5683 }, { lat: 59.3293, lon: 18.0686 }, 522.2, 3],
  ["copenhagen → berlin", { lat: 55.6761, lon: 12.5683 }, { lat: 52.52, lon: 13.405 }, 354.7, 3],
  ["a point to itself", { lat: 55.6761, lon: 12.5683 }, { lat: 55.6761, lon: 12.5683 }, 0, 0.001],
  ["across the street", { lat: 55.6761, lon: 12.5683 }, { lat: 55.6736, lon: 12.5989 }, 1.95, 0.15],
];

let failures = 0;

for (const [name, from, to, want, tolerance] of CASES) {
  const got = distanceKm(from, to);
  if (Math.abs(got - want) > tolerance) {
    failures += 1;
    console.error(`DISTANCE ${name}: want ${want} ±${tolerance} km, got ${got.toFixed(2)}`);
  }
}

const FORMATS = [
  [0.04, "< 0.1"],
  [0.4, "0.4"],
  [1.98, "2.0"],
  [9.94, "9.9"],
  [10.4, "10"],
  [37.5, "38"],
  [Number.NaN, "—"],
  [-1, "—"],
];
for (const [km, want] of FORMATS) {
  const got = formatKm(km);
  if (got !== want) {
    failures += 1;
    console.error(`FORMAT ${km}: want ${JSON.stringify(want)}, got ${JSON.stringify(got)}`);
  }
}

// A pair that is not a place must not become a distance. `[0, 0]` is the Gulf of Guinea, and a source that did not
// answer is not a venue there.
const PAIRS = [
  [{ g: [55.67, 12.55] }, true],
  [{ g: [0, 0] }, false],
  [{ g: [55.67] }, false],
  [{ g: [95, 12.55] }, false],
  [{ g: [-91, 12.55] }, false],
  [{}, false],
  [{ g: [55.67, Number.NaN] }, false],
];
for (const [row, want] of PAIRS) {
  const got = pointOf(row) !== null;
  if (got !== want) {
    failures += 1;
    console.error(`POINT ${JSON.stringify(row)}: want ${want ? "a place" : "no place"}, got ${got ? "a place" : "no place"}`);
  }
}

const towns = [
  { id: "copenhagen-dk", lat: 55.6814, lon: 12.5827 },
  { id: "stockholm-se", lat: 59.323, lon: 18.0677 },
  { id: "unplaced-xx", lat: 0, lon: 0 },
];
const near = nearestTown(towns, { lat: 55.68, lon: 12.57 });
if (near?.town.id !== "copenhagen-dk") {
  failures += 1;
  console.error(`NEAREST: want copenhagen-dk, got ${near?.town.id ?? "none"}`);
}

const total = CASES.length + FORMATS.length + PAIRS.length + 2;
console.log(`geocheck: ${total - failures}/${total} cases agree with the row contract`);
process.exit(failures ? 1 : 0);
