import { describe, expect, it } from "vitest";
import { statusLabel } from "./indexData";

/**
 * The four status letters, pinned to the model's own table: `internal/model` declares
 * `o` on sale, `c` cancelled, `x` postponed, `s` sold out. The board had `x` and `s` swapped for as long as
 * no source published one of them — which is exactly why this test covers all four, not the one a fixture
 * happens to carry.
 */
describe("statusLabel", () => {
  it("names each status the model defines", () => {
    expect(statusLabel("o")).toBeNull();
    expect(statusLabel("c")).toBe("cancelled");
    expect(statusLabel("x")).toBe("postponed");
    expect(statusLabel("s")).toBe("sold out");
  });

  it("says nothing for a status this build does not know", () => {
    expect(statusLabel("")).toBeNull();
    expect(statusLabel("?")).toBeNull();
  });
});
