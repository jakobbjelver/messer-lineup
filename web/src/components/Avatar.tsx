import { useState } from "react";
import { initials, portraitFor, type PortraitMap } from "../lib/portraits";

type Props = {
  name: string;
  portraits?: PortraitMap;
  /** `row` is 40 px; the artist page uses 96, the calendar 22, a search result 28, a badge 18. */
  size?: "row" | 96 | 28 | 22 | 18 | 16;
  /** A row by an artist in the reader's list, while the board is showing everyone. */
  mine?: boolean;
};

/**
 * The face, or the two letters that stand in for it.
 *
 * A portrait is an enhancement (ADR-002): the initials are the default path, they are deterministic
 * from the name, and an image that fails to load leaves them in place rather than a broken icon. The
 * photo is grayscale on purpose — the board has one chroma, and a face is not the place to spend it.
 */
export default function Avatar({ name, portraits, size = "row", mine }: Props) {
  const [broken, setBroken] = useState(false);
  const portrait = portraits && !broken ? portraitFor(portraits, name) : undefined;
  const cls = size === "row" ? "avatar" : `avatar avatar--${size}`;
  return (
    <span className={cls} data-mine={mine ? "" : undefined} aria-hidden="true">
      {portrait ? (
        <img src={portrait.u} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />
      ) : (
        // One letter below 24 px: two letters at a legible size do not fit, and a single glyph reads as an avatar
        // rather than as a truncated monogram.
        (size === 22 || size === 16 || size === 18 ? initials(name).slice(0, 1) : initials(name))
      )}
    </span>
  );
}
