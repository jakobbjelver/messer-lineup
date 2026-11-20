import { useEffect, useId, useMemo, useRef, useState } from "react";
import { MagnifyingGlass, X } from "@phosphor-icons/react";
import Loader from "./Loader";
import { importFrom, searchArtists, type ArtistHit } from "../lib/artistSearch";
import { Key } from "../lib/key";
import { labelFor, type TasteEntry } from "../lib/taste";

type Props = {
  value: TasteEntry[];
  onChange: (entries: TasteEntry[]) => void;
  /** The sheet's compact spacing; the wizard gets the roomier one. */
  compact?: boolean;
};

const DEBOUNCE_MS = 380;
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

/**
 * The artist picker — the one control the product is built on.
 *
 * A combobox over MusicBrainz, plus an `Add "…"` row that is always there: the index's spellings and
 * MusicBrainz's will never fully agree, and a picker that cannot add what you typed is a wall. The
 * list is the feedback — every choice is a badge you can see and remove.
 */
export default function ArtistPicker({ value, onChange, compact }: Props) {
  const baseId = useId();
  const input = useRef<HTMLInputElement | null>(null);
  const listId = `${baseId}-list`;

  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<ArtistHit[]>([]);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [busy, setBusy] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [announce, setAnnounce] = useState("");

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

  const options = useMemo(() => {
    const typed = query.trim();
    const list: { name: string; kind: "hit" | "add"; meta?: string }[] = hits.map((hit) => ({
      name: hit.name,
      kind: "hit",
      meta: [hit.type, hit.country].filter(Boolean).join(" · "),
    }));
    if (typed.length >= 2 && !hits.some((hit) => Key(hit.name) === Key(typed))) {
      list.push({ name: typed, kind: "add" });
    }
    return list;
  }, [hits, query]);

  const add = (name: string) => {
    const next = merge(value, [name]);
    if (next.length !== value.length) onChange(next);
    setQuery("");
    setHits([]);
    setOpen(false);
    setImported(null);
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
    if (event.key === "Escape" && open) {
      event.preventDefault();
      setOpen(false);
    }
  };

  const showList = open && options.length > 0;

  return (
    <div className={compact ? "picker picker--compact" : "picker"}>
      <div className="field">
        <MagnifyingGlass size={16} style={{ color: "var(--text-3)", flex: "none" }} />
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
          spellCheck={false}
          onChange={(event) => {
            setQuery(event.target.value);
            setOpen(true);
            setActive(0);
          }}
          onKeyDown={onKeyDown}
          onBlur={() => window.setTimeout(() => setOpen(false), 120)}
        />
        {busy ? <Loader size={16} label="Searching" /> : null}
      </div>

      <p className="sr-only" aria-live="polite">
        {announce}
      </p>

      {showList ? (
        <ul className="results" id={listId} role="listbox" aria-label="Artists">
          {options.map((option, index) => (
            <li
              key={`${option.kind}-${option.name}`}
              id={`${baseId}-opt-${index}`}
              role="option"
              aria-selected={index === active}
              className={option.kind === "add" ? "result result--add" : "result"}
              onMouseDown={(event) => {
                event.preventDefault();
                add(option.name);
              }}
              onMouseEnter={() => setActive(index)}
            >
              <span className="result__name">{option.kind === "add" ? `Add “${option.name}”` : option.name}</span>
              {option.meta ? <span className="result__meta">{option.meta}</span> : null}
            </li>
          ))}
        </ul>
      ) : unavailable ? (
        <p className="hint">Search is busy — keep typing, or add the name as it is.</p>
      ) : null}

      <div className="badges">
        {value.length === 0 ? (
          <span className="badges__empty">No artists yet — the board will show everything.</span>
        ) : (
          value.map((entry) => (
            <span className="chip" key={entry.key}>
              {entry.name || labelFor(entry.key)}
              <button
                type="button"
                aria-label={`Remove ${entry.name}`}
                onClick={() => onChange(value.filter((item) => item.key !== entry.key))}
              >
                <X size={12} />
              </button>
            </span>
          ))
        )}
      </div>

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
