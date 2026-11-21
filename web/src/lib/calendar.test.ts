import { describe, expect, it } from "vitest";
import { decodeSpec, encodeSpec } from "../../../src/calendar/spec";
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
