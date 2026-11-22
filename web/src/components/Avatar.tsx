import { useState } from "react";
import { initials, portraitFor, type PortraitMap } from "../lib/portraits";

type Props = {
  name: string;
  portraits?: PortraitMap;
  /** `row` is 40 px; the artist page uses 96, the calendar 22, a search result 28, a badge 18. */
  size?: "row" | 96 | 28 | 22 | 18 | 16;
  /** A row by an artist in the reader's list, while the board is showing everyone. */
  mine?: boolean;
  /** The face is being looked up: hold the box and shimmer, rather than print initials that are about to change. */
  pending?: boolean;
};

/**
 * The face, or the two letters that stand in for it.
 *
 * A portrait is an enhancement (ADR-002): the initials are the default path, they are deterministic
 * from the name, and an image that fails to load leaves them in place rather than a broken icon. The
 * photo is grayscale on purpose — the board has one chroma, and a face is not the place to spend it.
 *
 * `pending` is the third state, and it only exists where a face is fetched on demand (the artist picker's search
 * results). Printing initials and then swapping them for a photo makes the row look like it changed artist.
 *
 * **A fourth state: decoding.** A portrait that exists but has not finished loading holds the same shimmer until the
 * image's own `load` event, then fades in as a whole. Without it the picture paints line by line as the bytes arrive
 * (each row at its own pace, which reads as the list "rolling"), because a CSS fade begins when the element is
 * inserted, not when the bytes are there.
 */
export default function Avatar({ name, portraits, size = "row", mine, pending }: Props) {
  const [broken, setBroken] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const portrait = pending ? undefined : portraits && !broken ? portraitFor(portraits, name) : undefined;
  const decoding = Boolean(portrait) && !loaded;
  const cls = size === "row" ? "avatar" : `avatar avatar--${size}`;
  const classes = [cls, pending ? "avatar--pending" : "", decoding ? "avatar--loading" : ""].filter(Boolean).join(" ");
  return (
    <span className={classes} data-mine={mine ? "" : undefined} aria-hidden="true">
      {pending ? null : portrait ? (
        <img
          // A cached image can be complete before React sees it, and then `load` never fires: check once on mount.
          ref={(element) => {
            if (element?.complete && !loaded) setLoaded(true);
          }}
          src={portrait.u}
          alt=""
          loading="lazy"
          decoding="async"
          data-loaded={loaded ? "" : undefined}
          onLoad={() => setLoaded(true)}
          onError={() => setBroken(true)}
        />
      ) : (
        // One letter below 24 px: two letters at a legible size do not fit, and a single glyph reads as an avatar
        // rather than as a truncated monogram.
        (size === 22 || size === 16 || size === 18 ? initials(name).slice(0, 1) : initials(name))
      )}
    </span>
  );
}
