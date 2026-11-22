import { describe, expect, it } from "vitest";
import { decodeSpec, encodeSpec } from "../../../src/calendar/spec";
import { tokenFor } from "./calendar";
import { buildIcs } from "../../../src/calendar/ics";
import fixture from "../../../testdata/calendar/tokens.json";

type Vector = { name: string; token: string; ok: boolean; spec?: unknown };

/**
 * The token vectors are shared with the Go decoder (`internal/server/calendar_test.go` reads the same file). A token is
 * a URL that outlives a deploy, so both runtimes have to agree on what it means and, more importantly, on what they
 * refuse: a v2 token carrying genres must be a `400` here until this build serves genres, never a whole-town feed.
 */
describe("calendar token vectors", () => {
  for (const vector of (fixture as { vectors: Vector[] }).vectors) {
    it(vector.name, () => {
      const decoded = decodeSpec(vector.token);
      if (!vector.ok) {
        expect(decoded).toBeNull();
        return;
      }
      expect(decoded).toEqual(vector.spec);
    });
  }
});

describe("encodeSpec", () => {
  it("round-trips a spec through the decoder", () => {
    const spec = { v: 1 as const, t: "malmo-se", a: ["opeth", "bjork"] };
    expect(decodeSpec(encodeSpec(spec))).toEqual(spec);
  });

  it("refuses to write a token the feed would reject", () => {
    const tooMany = Array.from({ length: 201 }, (_, index) => `artist-${index}`);
    expect(() => encodeSpec({ v: 1, t: "malmo-se", a: tooMany })).toThrow(/at most 200/);
  });
});

describe("tokenFor", () => {
  const artists = [{ key: "opeth", name: "Opeth" }];

  it("stays v1 without a reminder", () => {
    expect(decodeSpec(tokenFor({ scope: "stockholm-se", artists }))).toEqual({ v: 1, t: "stockholm-se", a: ["opeth"] });
  });

  it("moves to v2 and carries r when asked", () => {
    expect(decodeSpec(tokenFor({ scope: "stockholm-se", artists, reminder: true }))).toEqual({
      v: 2,
      t: "stockholm-se",
      a: ["opeth"],
      r: 1,
    });
  });
});

describe("buildIcs", () => {
  const row = {
    k: "opeth|vega|2026-12-02",
    a: "Opeth",
    v: "Vega",
    d: "2026-12-02T20:00:00+01:00",
    u: "https://t.test/1",
    st: "o",
  };
  const meta = {
    townId: "copenhagen-dk",
    townLabel: "København",
    builtAt: "2026-11-22T10:00:00Z",
    origin: "https://lineup.messer.wtf",
  };

  it("has no alarm by default", () => {
    expect(buildIcs([row], meta)).not.toContain("VALARM");
  });

  it("wraps each event in a day-before alarm when asked", () => {
    const ics = buildIcs([row], { ...meta, reminder: true });
    expect((ics.match(/BEGIN:VALARM/g) ?? []).length).toBe(1);
    expect((ics.match(/END:VALARM/g) ?? []).length).toBe(1);
    expect(ics).toContain("ACTION:DISPLAY");
    expect(ics).toContain("TRIGGER:-P1D");
    expect(ics).toContain("END:VALARM\r\nEND:VEVENT");
    for (const line of ics.split("\r\n")) expect(new TextEncoder().encode(line).length).toBeLessThanOrEqual(75);
  });
});
