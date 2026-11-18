/**
 * The town a visitor chose, remembered.
 *
 * A town is a preference, not an inference. The edge's country hint is a guess that is wrong often enough to matter —
 * a Swedish address a few kilometres from Copenhagen is served a Danish town's board only by accident of a border —
 * so an explicit pick outranks every guess, and survives the visit. Only a pick is stored: the country guess must be
 * re-asked, because a wrong guess made sticky is worse than no memory at all.
 */
const TOWN_KEY = "lineup.town.chosen";

export function rememberedTown(): string | null {
  try {
    const value = localStorage.getItem(TOWN_KEY);
    return value !== null && value !== "" ? value : null;
  } catch {
    return null;
  }
}

export function rememberTown(id: string): void {
  try {
    localStorage.setItem(TOWN_KEY, id);
  } catch {
    /* a browser that refuses storage simply asks again next time */
  }
}

export function forgetTown(): void {
  try {
    localStorage.removeItem(TOWN_KEY);
  } catch {
    /* nothing to forget */
  }
}
