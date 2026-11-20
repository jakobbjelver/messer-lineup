import { Crosshair } from "@phosphor-icons/react";
import { townLabel, type Town, type TownEntry } from "../lib/indexData";
import type { Position } from "../lib/location";

type Props = {
  posts: TownEntry[];
  names: Town[];
  value: string;
  onChange: (id: string) => void;
  position: Position;
  canLocate: boolean;
  onLocate: () => void;
};

/**
 * Where. Just the towns the index publishes — no search field, no gazetteer promise.
 *
 * A fix picks the nearest published town and says so; it never prints kilometres, because a town's
 * point is the mean of its venues and that is a way to choose a place, not a distance from a person.
 */
export default function TownPicker({ posts, names, value, onChange, position, canLocate, onLocate }: Props) {
  const located = position.state === "granted";
  const asking = position.state === "asking";

  return (
    <div>
      {canLocate ? (
        <button
          type="button"
          className={located ? "locate locate--on" : "locate"}
          onClick={onLocate}
          aria-busy={asking ? true : undefined}
          aria-disabled={located ? true : undefined}
        >
          <Crosshair size={16} />
          <span>
            {asking ? "Asking your browser…" : located ? "Nearest town selected" : "Use my location"}
          </span>
          {located ? <span className="locate__tag">on</span> : null}
        </button>
      ) : null}

      <div className="picks" role="radiogroup" aria-label="Town">
        {posts.map((post) => {
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
    </div>
  );
}
