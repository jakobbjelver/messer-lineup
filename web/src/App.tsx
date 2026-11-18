import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Board from "./components/Board";
import Coverage from "./components/Coverage";
import Palette, { type PaletteTab } from "./components/Palette";
import Ribbon from "./components/Ribbon";
import TopBar from "./components/TopBar";
import { CaretDown } from "./components/icons";
import { createLamps, type Lamps } from "./lib/rays";
import { Key } from "./lib/key";
import { useTheme } from "./lib/theme";
import {
  readNames,
  readNamesParam,
  readTasteParam,
  toEntries,
  writeNames,
  writeTasteParam,
  type TasteEntry,
} from "./lib/taste";
import {
  boardDay,
  previouslySeen,
  publishedWindow,
  rememberVisit,
  windowFromURL,
  windowToURL,
  type Window as DateWindow,
} from "./lib/state";
import {
  dayOf,
  loadIndex,
  loadShard,
  stampLabel,
  townLabel,
  type Manifest,
  type Row,
  type Town,
  type TownEntry,
} from "./lib/indexData";

type Loaded = { manifest: Manifest; towns: Town[] };
type Phase =
  | { kind: "loading" }
  | { kind: "error"; code: string; message: string }
  | { kind: "ready"; loaded: Loaded; rows: Row[]; town: TownEntry };

export default function App() {
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [wantedTown, setWantedTown] = useState<string | null>(() => new URLSearchParams(location.search).get("town"));
  // The URL carries keys; the browser remembers how the reader spells them, so "Björk" survives a reload.
  const [taste, setTaste] = useState<TasteEntry[]>(() => toEntries(readTasteParam(location.search), [...readNames(), ...readNamesParam(location.search)]));
  const [filter, setFilter] = useState<"all" | "yours">("all");
  // The window is the URL's, clamped to whatever the manifest publishes. Until the manifest arrives it is the
  // published window itself, so nothing renders against a range the index does not cover.
  const [dateWindow, setWindow] = useState<DateWindow | null>(null);
  const [newKeys, setNewKeys] = useState<Set<string>>(new Set());
  const [palette, setPalette] = useState<{ open: boolean; tab: PaletteTab }>({ open: false, tab: "town" });
  const { theme, toggle: toggleTheme } = useTheme();
  const lamps = useRef<Lamps | null>(null);
  const canvas = useRef<HTMLCanvasElement | null>(null);

  /* The index: the manifest, then one shard. The window and "as of" come from the miner, not the browser. */
  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const { manifest, towns } = await loadIndex();
        const town = manifest.towns.find((t) => t.town === wantedTown) ?? manifest.towns[0];
        const rows = await loadShard(town.shard);
        if (!live) return;
        setPhase({ kind: "ready", loaded: { manifest, towns }, rows, town });

        // Two things happen once, when the board first has rows: the travel window is resolved against what is
        // published, and the visitor's previous visit is compared with what is here now.
        const published = publishedWindow(manifest);
        const resolved = windowFromURL(location.search, published);
        setWindow(resolved);
        const seen = previouslySeen(boardDay(manifest));
        setNewKeys(seen.size ? new Set(rows.filter((row) => !seen.has(row.k)).map((row) => row.k)) : new Set());
        rememberVisit(rows.map((row) => row.k), boardDay(manifest));
        // When the index has a row by an artist in the list, its spelling wins: a pasted "bjork" becomes "Björk".
        setTaste((current) => {
          if (current.length === 0) return current;
          const spelled = new Map(rows.map((row) => [Key(row.a), row.a]));
          let changed = false;
          const next = current.map((entry) => {
            const name = spelled.get(entry.key);
            if (name && name !== entry.name) {
              changed = true;
              return { ...entry, name };
            }
            return entry;
          });
          if (changed) writeNames(next);
          return changed ? next : current;
        });
        // The artist page's way back returns to this town, not to the first published one.
        try {
          sessionStorage.setItem("lineup.town", town.town);
        } catch {
          /* storage refused: back falls back to the root */
        }
        if (wantedTown !== town.town) {
          const url = new URL(location.href);
          url.searchParams.set("town", town.town);
          history.replaceState(null, "", url);
          setWantedTown(town.town);
        }
      } catch (err) {
        if (!live) return;
        const code = err instanceof Error && "code" in err ? String((err as { code: unknown }).code) : "unknown";
        setPhase({ kind: "error", code, message: err instanceof Error ? err.message : String(err) });
      }
    })();
    return () => {
      live = false;
    };
  }, [wantedTown]);

  /* The stage: four drifting gradients, grain, a lift that follows the pointer, and the WebGL lamps. */
  useEffect(() => {
    const calm = matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (canvas.current) {
      lamps.current = createLamps(canvas.current);
      lamps.current.boot();
    }
    if (calm) return () => lamps.current?.stop();

    const fine = matchMedia("(pointer: fine)").matches;
    const root = document.documentElement;
    let frame = 0;
    const onMove = (event: PointerEvent) => {
      lamps.current?.pointer(event.clientX, event.clientY);
      if (frame) return;
      frame = requestAnimationFrame(() => {
        frame = 0;
        root.style.setProperty("--mx", `${event.clientX}px`);
        root.style.setProperty("--my", `${event.clientY}px`);
        if (fine) document.body.setAttribute("data-lit", "");
      });
    };
    window.addEventListener("pointermove", onMove, { passive: true });
    return () => {
      window.removeEventListener("pointermove", onMove);
      if (frame) cancelAnimationFrame(frame);
      lamps.current?.stop();
    };
  }, []);

  /* A theme change retints the lamps: they are lit by the tokens, not by hard-coded colour. */
  useEffect(() => {
    lamps.current?.retint();
  }, [theme]);

  /* ⌘K / Ctrl-K: the palette, from anywhere. */
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setPalette({ open: true, tab: "town" });
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const chooseTown = useCallback((id: string) => {
    setPhase((current) => (current.kind === "ready" && current.town.town === id ? current : { kind: "loading" }));
    setWantedTown(id);
    const url = new URL(location.href);
    url.searchParams.set("town", id);
    history.replaceState(null, "", url);
  }, []);

  const applyTaste = useCallback((entries: TasteEntry[]) => {
    setTaste(entries);
    writeNames(entries);
    const next = writeTasteParam(entries, location.href);
    history.replaceState(null, "", next);
    if (entries.length === 0) setFilter("all");
  }, []);
  const forgetEverything = useCallback(() => {
    applyTaste([]);
    try {
      localStorage.removeItem("lineup.seen");
      localStorage.removeItem("lineup.visited");
      localStorage.removeItem("lineup.names");
      sessionStorage.removeItem("lineup.town");
    } catch {
      /* a browser that refuses storage has nothing to forget */
    }
    setNewKeys(new Set());
    history.replaceState(null, "", location.pathname);
  }, [applyTaste]);


  const retry = useCallback(() => {
    setPhase({ kind: "loading" });
    setWantedTown((current) => current ?? "");
  }, []);

  const ready = phase.kind === "ready" ? phase : null;
  const manifest = ready?.loaded.manifest;

  const tasteKeys = useMemo(() => new Set(taste.map((entry) => entry.key)), [taste]);
  // One pass over the shard: a row is "yours" when its artist key is in the taste set. The count and the filter
  // both read this set, so they cannot disagree with the rows that are actually marked.
  const mineKeys = useMemo(() => {
    const rows = ready?.rows ?? [];
    if (tasteKeys.size === 0) return new Set<string>();
    return new Set(rows.filter((row) => tasteKeys.has(Key(row.a))).map((row) => row.k));
  }, [ready, tasteKeys]);
  const mineCount = mineKeys.size;
  // A travel window narrows the board; the taste filter narrows it again. Both are views over the same shard, so
  // the counts under the ribbon always describe what is actually on screen.
  const inWindow = useMemo(() => {
    const rows = ready?.rows ?? [];
    if (!dateWindow) return rows;
    return rows.filter((row) => {
      const day = dayOf(row.d);
      return day >= dateWindow.from && day <= dateWindow.to;
    });
  }, [ready, dateWindow]);
  const shown = useMemo(() => {
    return filter === "yours" && mineKeys.size > 0 ? inWindow.filter((row) => mineKeys.has(row.k)) : inWindow;
  }, [filter, mineKeys, inWindow]);

  // A window is only worth writing down once it differs from what the manifest publishes.
  const published = ready ? publishedWindow(ready.loaded.manifest) : null;
  const applyWindow = useCallback(
    (next: DateWindow) => {
      setWindow(next);
      if (published) {
        history.replaceState(null, "", windowToURL(next, published, location.href));
      }
    },
    [published],
  );

  const body = (() => {
    if (phase.kind === "loading") {
      // A skeleton of the board itself, in the board's own grid: the page does not jump when the rows arrive,
      // and a first visit reads as "loading", not as "empty". No spinner, no sentence.
      return (
        <div className="skeleton" aria-busy="true" aria-live="polite">
          <span className="sr-only">Reading the index…</span>
          <div className="sk-line sk-line--sign" />
          <div className="sk-line sk-line--ribbon" />
          {Array.from({ length: 8 }, (_, index) => (
            <div className="sk-row" key={index}>
              <div className="sk-line sk-line--day" />
              <div className="sk-stack">
                <div className="sk-line sk-line--artist" />
                <div className="sk-line sk-line--venue" />
              </div>
            </div>
          ))}
        </div>
      );
    }
    if (phase.kind === "error") {
      return (
        <div className="state">
          <h2>The index could not be read.</h2>
          <p>
            <span className="num">{phase.code}</span> — {phase.message}
          </p>
          <div className="cta">
            <button className="btn primary" onClick={retry}>
              Try again
            </button>
          </div>
        </div>
      );
    }
    if (phase.rows.length === 0) {
      return (
        <div className="state">
          <h2>Nothing in this window.</h2>
          <p>
            The town is published, but no show in it lands inside the{" "}
            <span className="num">{phase.loaded.manifest.window.days}</span>-day window. Nothing is broken — the next
            sweep replaces the shard.
          </p>
        </div>
      );
    }
    if (shown.length === 0) {
      return (
        <div className="state">
          <h2>None of your artists play here in this window.</h2>
          <p>
            <span className="num">{phase.rows.length}</span> shows are on the board. Your taste sharpens it — it never
            gates it — so the full board is one tap away.
          </p>
          <div className="cta">
            <button className="btn primary" onClick={() => setFilter("all")}>
              Show the whole board
            </button>
            <button className="btn" onClick={() => setPalette({ open: true, tab: "taste" })}>
              Add more artists
            </button>
          </div>
        </div>
      );
    }
    return <Board rows={shown} tasteKeys={tasteKeys} newKeys={newKeys} />;
  })();

  const townName = ready ? townLabel(ready.town, ready.loaded.towns) : "";
  const sourceCount = ready ? new Set(ready.rows.flatMap((row) => row.src)).size : 0;

  return (
    <>
      <div className="stage" aria-hidden="true">
        <div className="stage__blobs" />
        <div className="stage__lift" />
        <div className="stage__grain" />
      </div>
      <canvas id="rays" ref={canvas} aria-hidden="true" />

      <TopBar theme={theme} onToggle={toggleTheme} />

      <main className="page">
        <section className="sheet">
          <div className="head">
            <div className="eyebrow">
              <span className="lab">
                {dateWindow && published && dateWindow.from !== published.from ? "Travel" : "Season"} board
                {phase.kind === "ready" ? " · " : ""}
                {phase.kind === "ready" ? (
                  <span className="num">
                    {dateWindow && published
                    ? Math.round(
                        (new Date(`${dateWindow.to}T12:00:00Z`).getTime() -
                          new Date(`${dateWindow.from}T12:00:00Z`).getTime()) /
                          86_400_000,
                      ) + 1
                      : (manifest?.window.days ?? 0)}
                  </span>
                ) : null}
                {phase.kind === "ready" ? " days" : ""}
              </span>
              {manifest ? (
                <span className="r">
                  <span className="lab">As of</span>{" "}
                  <span className="num" style={{ fontSize: 11, color: "var(--text-2)" }}>
                    {stampLabel(manifest.built_at)}
                  </span>
                </span>
              ) : null}
            </div>
            <div className="h1row" hidden={phase.kind !== "ready"}>
              <h1>
                <button
                  className="townbtn"
                  onClick={() => setPalette({ open: true, tab: "town" })}
                  aria-label="Change town"
                >
                  <span
                    className="sign"
                    data-t={townName}
                  >
                    {townName}
                  </span>
                  <CaretDown />
                </button>
              </h1>
              <div className="spacer" />
              <div className="actions">
                {tasteKeys.size > 0 ? (
                  <>
                    <div className="seg" role="group" aria-label="Filter">
                      <button aria-pressed={filter === "all"} onClick={() => setFilter("all")}>
                        all
                      </button>
                      <button aria-pressed={filter === "yours"} onClick={() => setFilter("yours")}>
                        yours <span className="num">{mineCount}</span>
                      </button>
                    </div>
                    <span className="div" aria-hidden="true" />
                  </>
                ) : null}
                <button
                  className={tasteKeys.size > 0 ? "taste-cta linked" : "taste-cta"}
                  onClick={() => setPalette({ open: true, tab: "taste" })}
                >
                  <svg viewBox="0 0 256 256" fill="currentColor" aria-hidden="true">
                    <path d="M208,32H48A16,16,0,0,0,32,48V208a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V48A16,16,0,0,0,208,32ZM86,176a8,8,0,0,1-16,0V128a8,8,0,0,1,16,0Zm40,0a8,8,0,0,1-16,0V96a8,8,0,0,1,16,0Zm40,0a8,8,0,0,1-16,0V112a8,8,0,0,1,16,0Zm40,0a8,8,0,0,1-16,0V80a8,8,0,0,1,16,0Z" />
                  </svg>
                  <span>
                    {tasteKeys.size > 0 ? `${tasteKeys.size} artists` : "Add taste"}
                  </span>
                </button>
              </div>
            </div>
          </div>

          {manifest && ready ? (
            <>
              <Ribbon
                from={dateWindow?.from ?? manifest.window.from}
                days={
                  dateWindow
                    ? Math.round(
                        (new Date(`${dateWindow.to}T12:00:00Z`).getTime() -
                          new Date(`${dateWindow.from}T12:00:00Z`).getTime()) /
                          86_400_000,
                      ) + 1
                    : manifest.window.days
                }
                rows={inWindow}
                builtOn={dayOf(manifest.built_at)}
              />
              <Coverage
                manifest={manifest}
                shows={inWindow.length}
                sourceCount={sourceCount}
                onForget={forgetEverything}
                canForget={taste.length > 0 || newKeys.size > 0}
              />
            </>
          ) : null}

          <div id="board">{body}</div>

          <div className="foot">
            <span className="l">
              <span className="lab">Lineup</span>{" "}
              <span className="num">
                0.1 · {ready?.town.sha256.slice(0, 7) ?? "—"} · {manifest ? stampLabel(manifest.built_at) : "—"}
              </span>
            </span>
            <span className="num">{ready?.town.town ?? "—"}</span>
          </div>
        </section>
      </main>

      <Palette
        open={palette.open}
        tab={palette.tab}
        onTab={(tab) => setPalette((current) => ({ ...current, tab }))}
        onClose={() => setPalette((current) => ({ ...current, open: false }))}
        towns={manifest?.towns ?? []}
        townNames={ready?.loaded.towns ?? []}
        currentTown={ready?.town.town ?? ""}
        onTown={chooseTown}
        dateWindow={dateWindow}
        published={published}
        onWindow={applyWindow}
        taste={taste}
        onTaste={applyTaste}
        onBoard={(key) => (ready ? ready.rows.some((row) => Key(row.a) === key) : false)}
      />
    </>
  );
}
