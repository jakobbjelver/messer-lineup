import { describe, expect, it } from "vitest";
import { groupByDay, nearestElsewhere } from "./board";
import type { Row } from "./indexData";

/** The smallest row the board will render: identity, artist, venue, town, start, status, link. */
function row(k: string, day: string, artist = "Opeth"): Row {
  return {
    k,
    a: artist,
    v: "Debaser",
    c: "stockholm-se",
    d: `${day}T20:00:00+01:00`,
    st: "o",
    u: `https://example.test/${k}`,
    src: ["tickster"],
  };
}

describe("groupByDay", () => {
  it("groups rows by the day their own timestamp names, in the order they arrive", () => {
    const rows = [
      row("a|debaser|2026-11-21", "2026-11-21"),
      row("b|debaser|2026-11-21", "2026-11-21", "Katatonia"),
      row("c|debaser|2026-11-22", "2026-11-22"),
    ];

    const days = groupByDay(rows);

    expect(days.map((group) => group.day)).toEqual(["2026-11-21", "2026-11-22"]);
    expect(days[0]!.rows.map((entry) => entry.k)).toEqual(["a|debaser|2026-11-21", "b|debaser|2026-11-21"]);
    expect(days[1]!.rows).toHaveLength(1);
  });

  it("never re-sorts a group: the loader's order is the board's order", () => {
    const rows = [
      row("late|debaser|2026-11-21", "2026-11-21"),
      row("early|debaser|2026-11-21", "2026-11-21"),
    ];

    expect(groupByDay(rows)[0]!.rows.map((entry) => entry.k)).toEqual([
      "late|debaser|2026-11-21",
      "early|debaser|2026-11-21",
    ]);
  });

  it("an empty board is no days, not one empty day", () => {
    expect(groupByDay([])).toEqual([]);
  });
});

describe("nearestElsewhere", () => {
  const stockholm = row("opeth|debaser|2026-12-01", "2026-12-01");
  const gothenburg = { ...row("opeth|pustervik|2026-12-04", "2026-12-04"), c: "gothenburg-se", v: "Pustervik" };
  const malmo = { ...row("opeth|planb|2026-12-02", "2026-12-02"), c: "malmo-se", v: "Plan B" };
  const mine = new Set(["opeth"]);

  it("returns the soonest matching show in another town", () => {
    const found = nearestElsewhere([stockholm, gothenburg, malmo], mine, "stockholm-se", null);
    expect(found?.row.k).toBe(malmo.k);
    expect(found?.km).toBeNull();
  });

  it("ignores rows that are not yours, and rows in the scope", () => {
    const other = { ...row("someone|debaser|2026-11-30", "2026-11-30", "Someone"), c: "malmo-se" };
    const found = nearestElsewhere([stockholm, other, malmo], mine, "stockholm-se", null);
    expect(found?.row.k).toBe(malmo.k);
    expect(nearestElsewhere([stockholm], mine, "stockholm-se", null)).toBeNull();
  });

  it("with a position, only a venue within 150 km counts", () => {
    // Stockholm (59.33, 18.07) → Malmö is ~510 km, Gothenburg ~400: nothing is nearby.
    const far = nearestElsewhere([gothenburg, malmo], mine, "stockholm-se", { lat: 59.33, lon: 18.07 });
    expect(far).toBeNull();
    // Gothenburg to itself: Put a Malmö venue 20 km from a point outside Gothenburg.
    const close = { ...row("opeth|close|2026-12-03", "2026-12-03"), c: "gothenburg-se", g: [57.72, 11.98] as [number, number] };
    const near = nearestElsewhere([close], mine, "stockholm-se", { lat: 57.7, lon: 11.97 });
    expect(near?.row.k).toBe(close.k);
    expect(near?.km).toBeGreaterThan(0);
    expect(near!.km!).toBeLessThan(5);
  });

  it("a fix cannot place a row with no venue position, so it skips it", () => {
    const found = nearestElsewhere([gothenburg, malmo], mine, "stockholm-se", { lat: 55.6, lon: 13.0 });
    expect(found).toBeNull();
  });

  it("is null for an empty taste list", () => {
    expect(nearestElsewhere([malmo], new Set(), "stockholm-se", null)).toBeNull();
  });
});
