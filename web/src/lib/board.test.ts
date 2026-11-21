import { describe, expect, it } from "vitest";
import { groupByDay } from "./board";
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
