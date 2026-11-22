import { describe, expect, it } from "vitest";
import {
  EMPTY_GENRE_MAP,
  GENRES,
  GENRE_CAP,
  isGenre,
  keysForGenres,
  readGenreParam,
  toggleGenre,
  writeGenreParam,
  type GenreId,
} from "./genres";
import { tokenFor } from "./calendar";
import { decodeSpec } from "../../../src/calendar/spec";

describe("the vocabulary", () => {
  it("is sixteen unique ids with the token's grammar", () => {
    expect(GENRES.length).toBe(16);
    expect(new Set(GENRES.map((genre) => genre.id)).size).toBe(16);
    for (const genre of GENRES) {
      expect(genre.id).toMatch(/^[a-z0-9-]{1,24}$/);
      expect(isGenre(genre.id)).toBe(true);
    }
    expect(isGenre("polka")).toBe(false);
  });
});

describe("readGenreParam", () => {
  it("keeps order, drops unknown ids and duplicates", () => {
    expect(readGenreParam("?g=metal,polka,punk,metal")).toEqual(["metal", "punk"]);
  });

  it("applies the cap", () => {
    const many = "?g=rock,metal,punk,pop,folk,jazz,blues,world";
    expect(readGenreParam(many)).toHaveLength(GENRE_CAP);
  });

  it("is empty without the parameter", () => {
    expect(readGenreParam("?town=stockholm-se")).toEqual([]);
  });
});

describe("writeGenreParam", () => {
  it("writes and deletes the parameter", () => {
    expect(writeGenreParam(["metal", "punk"], "https://x.test/?town=stockholm-se")).toBe(
      "/?town=stockholm-se&g=metal%2Cpunk",
    );
    expect(writeGenreParam([], "https://x.test/?town=stockholm-se&g=metal")).toBe("/?town=stockholm-se");
  });
});

describe("toggleGenre", () => {
  it("adds, removes and refuses past the cap", () => {
    expect(toggleGenre([], "metal")).toEqual(["metal"]);
    expect(toggleGenre(["metal"], "metal")).toEqual([]);
    const full = ["rock", "metal", "punk", "pop", "folk", "jazz"] as GenreId[];
    expect(toggleGenre(full, "blues")).toEqual(full);
  });
});

describe("keysForGenres", () => {
  it("unions the chosen genres and ignores the unknown", () => {
    const map = {
      artist: new Map(),
      genres: new Map([
        ["metal", ["opeth", "katatonia"]],
        ["punk", ["refused"]],
      ]),
    };
    expect([...keysForGenres(map, ["metal", "punk", "polka"])].sort()).toEqual(["katatonia", "opeth", "refused"]);
    expect(keysForGenres(EMPTY_GENRE_MAP, ["metal"]).size).toBe(0);
  });
});

describe("the feed token carries genres", () => {
  it("moves to v2 and carries g", () => {
    const token = tokenFor({ scope: "stockholm-se", artists: [], genres: ["metal", "punk"] });
    expect(decodeSpec(token)).toEqual({ v: 2, t: "stockholm-se", g: ["metal", "punk"] });
  });

  it("keeps a genre-only board out of v1", () => {
    const token = tokenFor({ scope: "stockholm-se", artists: [], genres: [] });
    expect(decodeSpec(token)).toEqual({ v: 1, t: "stockholm-se" });
  });
});
