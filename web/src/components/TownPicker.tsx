import { useEffect, useMemo, useRef, useState } from "react";
import { Crosshair, GlobeHemisphereWest, MagnifyingGlass, X } from "@phosphor-icons/react";
import { ANYWHERE, townLabel, type Town, type TownEntry } from "../lib/indexData";
import type { Position } from "../lib/location";

type Props = {
  posts: TownEntry[];
  names: Town[];
  value: string;
  onChange: (id: string) => void;
  position: Position;
  canLocate: boolean;
  /** Use the fix, or use the fix already in hand without asking again. */
  onLocate: () => void;
  /** Whether the town on screen is the one this fix chose. */
  located: boolean;
  /** Give the fix up: the town stays, the pin does not. */
  onRelease: () => void;
};

/** Accents, case and the `ø` a Danish keyboard writes: a typed search must not lose a town to its spelling. */
function fold(value: string): string {
  return value
    .normalize("NFD")
    .replace(/\p{M}/gu, "")
    .replace(/ø/gi, "o")
    .replace(/æ/gi, "ae")
    .replace(/å/gi, "a")
    .toLowerCase();
}

/**
 * Where. The towns the index publishes, a search over them, and one way to ask the device.
 *
 * A fix picks the nearest published town and says so; it never prints kilometres, because a town's
 * point is the mean of its venues and that is a way to choose a place, not a distance from a person.
 *
 * Two rules learned the hard way. The pin is **not** the town: choosing another town releases it (the
 * button goes back to `Use my location`) instead of the sheet re-pinning on every open. And a pin you
 * cannot give back is a state with no exit, so the located button is pressable and pressing it releases.
 */
export default function TownPicker({
  posts,
  names,
  value,
  onChange,
  position,
  canLocate,
  onLocate,
  located,
  onRelease,
}: Props) {
  const [query, setQuery] = useState("");
  const search = useRef<HTMLInputElement | null>(null);
  const asking = position.state === "asking";
  const denied = position.state === "denied" || position.state === "unavailable";
  // Escape clears the town search before it closes the sheet — the same order the artist list uses, and again a
  // window-capture listener because Radix listens for Escape in the capture phase on the document.
  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || !query || document.activeElement !== search.current) return;
      event.stopPropagation();
      event.preventDefault();
      setQuery("");
    };
    window.addEventListener("keydown", onEscape, true);
    return () => window.removeEventListener("keydown", onEscape, true);
  }, [query]);

  const rows = useMemo(() => {
    const q = fold(query.trim());
    if (!q) return posts;
    return posts.filter((post) => fold(townLabel(post, names)).includes(q) || fold(post.town).includes(q));
  }, [posts, names, query]);

  /** The whole board, for the scope row's count: it is what every town adds up to. */
  const total = useMemo(() => posts.reduce((sum, post) => sum + post.count, 0), [posts]);
  const anywhere = value === ANYWHERE;

  return (
    <div className="towns">
      {canLocate ? (
        <button
          type="button"
          className={located ? "locate locate--on" : "locate"}
          onClick={located ? onRelease : onLocate}
          aria-busy={asking ? true : undefined}
        >
          <Crosshair size={16} />
          <span>
            {asking
              ? "Asking your browser…"
              : located
                ? "Nearest town selected"
                : anywhere && position.state === "granted"
                  ? "Location on"
                  : denied
                    ? "Location off"
                    : "Use my location"}
          </span>
          {/* Both states carry the tag: it is the one thing on the control that is a state, and a lamp with no
              counterpart reads as decoration rather than as "on". */}
          <span className={located ? "locate__tag locate__tag--on" : "locate__tag"}>
            {located ? "on" : "off"}
          </span>
        </button>
      ) : null}

      {posts.length > 1 ? (
        <div className="field field--search">
          <MagnifyingGlass size={16} className="field__icon" />
          <input
            ref={search}
            type="search"
            value={query}
            placeholder="Search towns…"
            aria-label="Search towns"
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            onChange={(event) => setQuery(event.target.value)}
          />
          {query ? (
            <button type="button" className="field__clear" aria-label="Clear town search" onClick={() => setQuery("")}>
              <X size={13} />
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="picks" role="radiogroup" aria-label="Town">
        {/* The scope, above the towns and always present: it is a choice, it has no shard of its own, and it can only
            ever be asked for explicitly — never the default the edge or the first town would pick. */}
        {posts.length > 1 ? (
          <button
            type="button"
            className="pick pick--scope"
            role="radio"
            aria-checked={anywhere}
            onClick={() => onChange(ANYWHERE)}
          >
            <span className="pick__dot" aria-hidden="true" />
            <GlobeHemisphereWest size={15} className="pick__globe" aria-hidden="true" />
            <span className="pick__name">Anywhere</span>
            <span className="pick__meta">
              {total} {total === 1 ? "show" : "shows"}
            </span>
          </button>
        ) : null}
        {rows.map((post) => {
          const selected = post.town === value;
          return (
            <button
              key={post.town}
              type="button"
              className="pick"
              role="radio"
              aria-checked={selected}
              onClick={() => onChange(post.town)}
            >
              <span className="pick__dot" aria-hidden="true" />
              <span className="pick__name">{townLabel(post, names)}</span>
              <span className="pick__meta">
                {post.count} {post.count === 1 ? "show" : "shows"}
              </span>
            </button>
          );
        })}
      </div>

      {rows.length === 0 ? <p className="hint">No town matches.</p> : null}
    </div>
  );
}
