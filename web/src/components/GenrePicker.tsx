import { useEffect, useId, useMemo, useRef, useState } from "react";
import { Tag, X } from "@phosphor-icons/react";
import { GENRES, GENRE_CAP, genreLabel, type GenreId } from "../lib/genres";

type Props = {
  value: GenreId[];
  onChange: (next: GenreId[]) => void;
  /** The settings sheet passes this; the wizard does not. */
  compact?: boolean;
};

/**
 * Genres beside artists.
 *
 * A closed vocabulary, so this is a filter box rather than a search: sixteen labels, the ones already chosen removed
 * from the list, and a chip for each choice. It sits under the artist picker and looks like it, because the two are the
 * same question — what should the board show — asked once about names and once about sound.
 */
export default function GenrePicker({ value, onChange, compact }: Props) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  const baseId = useId();

  const typed = query.trim().toLowerCase();
  const options = useMemo(
    () =>
      GENRES.filter(
        (genre) => !value.includes(genre.id) && (typed === "" || genre.label.toLowerCase().includes(typed)),
      ).slice(0, 8),
    [value, typed],
  );
  const full = value.length >= GENRE_CAP;
  const showList = open && !full && options.length > 0;

  // Escape belongs to the list before it belongs to the sheet: a capture listener on the window runs before Radix's
  // document handler, which is the only place that can win (the same rule as the artist search).
  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpen(false);
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open]);

  const add = (id: GenreId) => {
    onChange(value.includes(id) || value.length >= GENRE_CAP ? value : [...value, id]);
    setQuery("");
    setActive(0);
    input.current?.focus();
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setOpen(true);
      setActive((index) => Math.min(index + 1, Math.max(options.length - 1, 0)));
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      setActive((index) => Math.max(index - 1, 0));
    } else if (event.key === "Enter") {
      const option = options[active];
      if (option) {
        event.preventDefault();
        add(option.id);
      }
    }
  };

  return (
    <div className={compact ? "picker picker--compact" : "picker"}>
      <div className="field">
        <Tag size={16} className="field__icon" aria-hidden="true" />
        <input
          ref={input}
          type="text"
          value={query}
          placeholder={value.length ? "Add a genre…" : "Search genres…"}
          aria-label="Search genres"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={showList}
          aria-controls={`${baseId}-list`}
          aria-activedescendant={showList ? `${baseId}-opt-${active}` : undefined}
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="none"
          spellCheck={false}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          onFocus={() => setOpen(true)}
          onBlur={() => setOpen(false)}
        />
      </div>

      {showList ? (
        <ul className="results" id={`${baseId}-list`} role="listbox" aria-label="Genres">
          {options.map((genre, index) => (
            <li
              key={genre.id}
              id={`${baseId}-opt-${index}`}
              role="option"
              aria-selected={index === active}
              className="result result--genre"
              onPointerDown={(event) => {
                event.preventDefault();
                add(genre.id);
              }}
              onMouseEnter={() => setActive(index)}
            >
              <span className="result__name">{genre.label}</span>
            </li>
          ))}
        </ul>
      ) : null}

      {value.length ? (
        <div className="badges">
          {value.map((id) => (
            <span className="chip" key={id}>
              {genreLabel(id)}
              <button
                type="button"
                aria-label={`Remove ${genreLabel(id)}`}
                onClick={() => onChange(value.filter((entry) => entry !== id))}
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      {full ? <p className="hint hint--cap">{GENRE_CAP} is the limit — remove one to add another.</p> : null}
    </div>
  );
}
