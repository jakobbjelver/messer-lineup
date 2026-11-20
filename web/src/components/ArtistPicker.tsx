import { useEffect, useId, useMemo, useRef, useState } from "react";
import { MagnifyingGlass, Plus, X } from "@phosphor-icons/react";
import Loader from "./Loader";
import Avatar from "./Avatar";
import { importFrom, searchArtists, type ArtistHit } from "../lib/artistSearch";
import { Key } from "../lib/key";
import { labelFor, type TasteEntry } from "../lib/taste";
import { faceSettled, resolvePortraits, type PortraitMap } from "../lib/portraits";

type Props = {
  value: TasteEntry[];
  onChange: (entries: TasteEntry[]) => void;
  /** The sheet's compact spacing; the wizard gets the roomier one. */
  compact?: boolean;
  /** Faces where the published file has one; initials everywhere else. */
  portraits?: PortraitMap;
};

const DEBOUNCE_MS = 320;
const CAP = 100;

function merge(current: TasteEntry[], names: string[]): TasteEntry[] {
  const seen = new Set(current.map((entry) => entry.key));
  const next = [...current];
  for (const raw of names) {
    const name = raw.trim();
    if (!name) continue;
    const key = Key(name);
    if (seen.has(key)) continue;
    seen.add(key);
    next.push({ key, name });
    if (next.length >= CAP) break;
  }
  return next;
}

/** The query inside a name, so a result shows *why* it matched. Case-insensitive, first hit only. */
function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim().toLowerCase();
  if (!q) return <>{text}</>;
  const at = text.toLowerCase().indexOf(q);
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <b className="result__hit">{text.slice(at, at + q.length)}</b>
      {text.slice(at + q.length)}
    </>
  );
}

/**
 * The artist picker — the one control the product is built on.
 *
 * A combobox over MusicBrainz, plus an `Add "…"` row that is always there: the index's spellings and
 * MusicBrainz's will never fully agree, and a picker that cannot add what you typed is a wall. The
 * list is the feedback — every choice is a badge you can see and remove.
 *
 * The rules a search box owes a reader, in the order they matter:
 *
 *   - **Never a blank answer.** Zero hits says "no match" in words, a failed search says *that* it failed,
 *     and the `Add` row sits under both — the reader always has one thing to press.
 *   - **Always a way out.** A clear button in the field, Escape that closes the list before it closes the
 *     sheet, and a list that closes when you press anywhere else.
 *   - **Keyboard and pointer are one path.** Arrows move the highlight and it scrolls into view; hover and
 *     focus draw the same rule; every row is a pointer target.
 */
export default function ArtistPicker({ value, onChange, compact, portraits }: Props) {
  const baseId = useId();
  const input = useRef<HTMLInputElement | null>(null);
  const box = useRef<HTMLDivElement | null>(null);
  const list = useRef<HTMLUListElement | null>(null);
  const listId = `${baseId}-list`;

  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<ArtistHit[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [settled, setSettled] = useState("");
  const [announce, setAnnounce] = useState("");
  /** Faces fetched on demand for the hits on screen, and the names still in flight (their box shimmers). */
  const [faces, setFaces] = useState<PortraitMap>(new Map());
  const [waiting, setWaiting] = useState<string[]>([]);

  const [mode, setMode] = useState<null | "listenbrainz" | "lastfm">(null);
  const [handle, setHandle] = useState("");
  const [importing, setImporting] = useState(false);
  const [importError, setImportError] = useState<string | null>(null);
  const [imported, setImported] = useState<number | null>(null);

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setHits([]);
      setBusy(false);
      setUnavailable(false);
      setSettled("");
      return;
    }
    const controller = new AbortController();
    setBusy(true);
    const timer = window.setTimeout(() => {
      searchArtists(q, controller.signal)
        .then((result) => {
          setHits(result.hits);
          setUnavailable(result.unavailable);
          setActive(0);
          setOpen(true);
          setSettled(q);
          setAnnounce(result.hits.length === 1 ? "1 artist" : `${result.hits.length} artists`);
        })
        .catch(() => {
          /* aborted by the next keystroke */
        })
        .finally(() => setBusy(false));
    }, DEBOUNCE_MS);
    return () => {
      controller.abort();
      window.clearTimeout(timer);
    };
  }, [query]);

  /**
   * A search hit is usually an artist the published file has no portrait for — the picker is the one surface whose
   * rows are not the index's rows. So the names on screen are asked for once, as a batch, and the rows hold a
   * skeleton until each one is settled (a picture, or a confirmed none).
   */
  useEffect(() => {
    if (hits.length === 0) {
      setFaces(new Map());
      setWaiting([]);
      return;
    }
    const unknown = hits.map((hit) => hit.name).filter((name) => !portraits?.has(Key(name)));
    const unanswered = unknown.filter((name) => !faceSettled(name));
    setWaiting(unanswered.map((name) => Key(name)));
    if (unknown.length === 0) return;
    let live = true;
    resolvePortraits(unknown)
      .then((next) => {
        if (!live) return;
        setFaces(next);
        setWaiting([]);
      })
      .catch(() => {
        if (live) setWaiting([]);
      });
    return () => {
      live = false;
    };
  }, [hits, portraits]);

  /**
   * Escape closes the list before it closes the sheet.
   *
   * It has to be a **capture** listener on `window`. Radix's overlay listens for Escape with
   * `{capture: true}` on the document (measured in `@radix-ui/react-dismissable-layer`), so capture at the window
   * runs before it and a `stopPropagation` here is the only thing that can decide "the list, not the sheet".
   * A React `onKeyDown` bubble handler arrives last and cannot stop it — that is why the sheet used to close
   * with the first Escape and leave the reader with a query and no list.
   */
  useEffect(() => {
    if (!open) return;
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      event.preventDefault();
      setOpen(false);
    };
    window.addEventListener("keydown", onEscape, true);
    return () => window.removeEventListener("keydown", onEscape, true);
  }, [open]);

  /** A press anywhere outside the picker closes the list — the 120 ms blur timer this replaces lost clicks. */
  useEffect(() => {
    const onDown = (event: PointerEvent) => {
      if (!box.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onDown);
    return () => document.removeEventListener("pointerdown", onDown);
  }, []);

  /** Keep the highlighted row in view when the arrows walk past the list's own height. */
  useEffect(() => {
    if (!open) return;
    const node = list.current?.querySelector<HTMLElement>(`#${CSS.escape(`${baseId}-opt-${active}`)}`);
    node?.scrollIntoView({ block: "nearest" });
  }, [active, open, baseId]);

  const options = useMemo(() => {
    const typed = query.trim();
    const rows: { name: string; kind: "hit" | "add"; meta?: string }[] = hits.map((hit) => ({
      name: hit.name,
      kind: "hit",
      meta: [hit.type, hit.country].filter(Boolean).join(" · "),
    }));
    if (typed.length >= 2 && !hits.some((hit) => Key(hit.name) === Key(typed))) {
      rows.push({ name: typed, kind: "add" });
    }
    return rows;
  }, [hits, query]);

  const add = (name: string) => {
    const next = merge(value, [name]);
    if (next.length !== value.length) onChange(next);
    setQuery("");
    setHits([]);
    setOpen(false);
    setSettled("");
    setImported(null);
    input.current?.focus();
  };

  const clear = () => {
    setQuery("");
    setHits([]);
    setOpen(false);
    setSettled("");
    input.current?.focus();
  };

  const runImport = async () => {
    if (!mode || !handle.trim()) return;
    setImporting(true);
    setImportError(null);
    try {
      const names = await importFrom(mode, handle);
      if (names.length === 0) {
        setImportError("That account has no listening history yet.");
      } else {
        const before = value.length;
        const next = merge(value, names);
        onChange(next);
        setImported(next.length - before);
        setMode(null);
        setHandle("");
      }
    } catch (err) {
      setImportError(err instanceof Error ? err.message : "The import did not answer.");
    } finally {
      setImporting(false);
    }
  };

  const onKeyDown = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      if (options.length === 0) return;
      event.preventDefault();
      setOpen(true);
      setActive((current) =>
        event.key === "ArrowDown" ? (current + 1) % options.length : (current - 1 + options.length) % options.length,
      );
      return;
    }
    if (event.key === "Enter") {
      const pick = options[active];
      if (pick) {
        event.preventDefault();
        add(pick.name);
      }
      return;
    }

  };

  /** The published file wins; the on-demand faces fill the gaps it leaves. */
  const shownPortraits = useMemo<PortraitMap>(
    () => new Map([...(portraits ?? []), ...faces]),
    [portraits, faces],
  );

  const showList = open && options.length > 0;
  const typed = query.trim();
  const note =
    typed.length >= 2 && settled === typed && !busy
      ? unavailable
        ? "Search is unavailable."
        : hits.length === 0
          ? "No match."
          : ""
      : "";

  return (
    <div className={compact ? "picker picker--compact" : "picker"} ref={box}>
      <div className="field">
        <MagnifyingGlass size={16} className="field__icon" />
        <input
          ref={input}
          type="text"
          value={query}
          placeholder="Search artists…"
          aria-label="Search artists"
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={showList}
          aria-controls={listId}
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
          onFocus={() => {
            if (options.length > 0) setOpen(true);
          }}
          onBlur={(event) => {
            if (!box.current?.contains(event.relatedTarget as Node | null)) setOpen(false);
          }}
        />
        {query ? (
          <button type="button" className="field__clear" aria-label="Clear search" onClick={clear}>
            <X size={13} />
          </button>
        ) : null}
        {busy ? <Loader size={16} label="Searching" /> : null}
      </div>

      <p className="sr-only" aria-live="polite">
        {announce}
      </p>

      {note ? <p className="results__note">{note}</p> : null}

      {showList ? (
        <ul className="results" id={listId} role="listbox" aria-label="Artists" ref={list}>
          {options.map((option, index) => (
            <li
              key={`${option.kind}-${option.name}`}
              id={`${baseId}-opt-${index}`}
              role="option"
              aria-selected={index === active}
              className={option.kind === "add" ? "result result--add" : "result"}
              onPointerDown={(event) => {
                // Pressing the row must not blur the field before the click lands.
                event.preventDefault();
                add(option.name);
              }}
              onMouseEnter={() => setActive(index)}
            >
              {option.kind === "add" ? (
                <span className="result__add" aria-hidden="true">
                  <Plus size={14} />
                </span>
              ) : (
                <Avatar
                  name={option.name}
                  portraits={shownPortraits}
                  pending={option.kind === "hit" && waiting.includes(Key(option.name))}
                  size={28}
                />
              )}
              <span className="result__name">
                {option.kind === "add" ? `Add “${option.name}”` : <Highlight text={option.name} query={typed} />}
              </span>
              {option.meta ? <span className="result__meta">{option.meta}</span> : null}
            </li>
          ))}
        </ul>
      ) : null}

      {value.length ? (
        <div className="badges">
          {value.map((entry) => (
            <span className="chip" key={entry.key}>
              <Avatar name={entry.name || labelFor(entry.key)} portraits={shownPortraits} size={18} />
              {entry.name || labelFor(entry.key)}
              <button
                type="button"
                aria-label={`Remove ${entry.name}`}
                onClick={() => onChange(value.filter((item) => item.key !== entry.key))}
              >
                <X size={12} />
              </button>
            </span>
          ))}
        </div>
      ) : null}

      {mode === null ? (
        <div className="import">
          {imported !== null ? <span>Added {imported} artists · </span> : <span>Import from</span>}
          <button type="button" onClick={() => { setMode("listenbrainz"); setImportError(null); }}>
            ListenBrainz
          </button>
          <button type="button" onClick={() => { setMode("lastfm"); setImportError(null); }}>
            Last.fm
          </button>
        </div>
      ) : (
        <div className="import">
          <div className="import__field">
            <div className="field">
              <input
                type="text"
                value={handle}
                placeholder={mode === "listenbrainz" ? "ListenBrainz username" : "Last.fm username"}
                aria-label={mode === "listenbrainz" ? "ListenBrainz username" : "Last.fm username"}
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                onChange={(event) => setHandle(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") void runImport();
                }}
              />
            </div>
            <button className="btn" type="button" disabled={importing || !handle.trim()} onClick={() => void runImport()}>
              {importing ? "Importing…" : "Import"}
            </button>
            <button
              className="iconbtn"
              type="button"
              aria-label="Cancel import"
              onClick={() => {
                setMode(null);
                setHandle("");
                setImportError(null);
              }}
            >
              <X size={14} />
            </button>
          </div>
          {importError ? <p className="hint">{importError}</p> : null}
        </div>
      )}
    </div>
  );
}
