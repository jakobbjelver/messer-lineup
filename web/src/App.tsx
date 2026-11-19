import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Board from "./components/Board";
import Coverage from "./components/Coverage";
import Palette, { type PaletteTab } from "./components/Palette";
import Ribbon from "./components/Ribbon";
import TopBar from "./components/TopBar";
import Fab from "./components/ui/Fab";
import { CaretDown } from "./components/icons";
import { createLamps, type Lamps } from "./lib/rays";
import { Key } from "./lib/key";
import { distanceKm, nearestTown, pointOf, type Origin } from "./lib/geo";
import { locationSupported, useDevicePosition } from "./lib/location";
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
  reachFromURL,
  reachToURL,
  rememberVisit,
  windowFromURL,
  windowToURL,
  REACH_STEPS,
  type Window as DateWindow,
} from "./lib/state";
import {
  addDays,
  dayNumber,
  defaultTown,
  dayOf,
  monthShort,
  loadIndex,
  loadShard,
  stampLabel,
  townLabel,
  type Manifest,
  type Row,
  type Town,
  type TownEntry,
} from "./lib/indexData";
import { forgetTown, rememberedTown, rememberTown } from "./lib/town";

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
  // A reach is a distance from the reader, so it means nothing until the reader has said where they are: a
  // `?reach=` that arrives on a device without a fix opens the full board rather than a false empty one.
  const [reach, setReach] = useState<number | null>(() => reachFromURL(location.search));
  // Whether the town on screen is the edge's guess, so its name can wear `approx.` — a fix removes it.
  const [approx, setApprox] = useState(false);
  const { position, request: locate } = useDevicePosition();
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
        // A town in the URL wins, then the one this visitor picked before, then the edge's country hint, then the
        // first published town. The memory outranks the hint on purpose: a border is not a preference.
        const picked = wantedTown ?? rememberedTown();
        let chosen = picked;
        let guessed = false;
        if (!chosen) {
          const hint = await defaultTown(manifest, towns);
          chosen = hint.town;
          guessed = hint.basis !== "fallback";
        }
        const town = manifest.towns.find((t) => t.town === chosen) ?? manifest.towns[0];
        const rows = await loadShard(town.shard);
        if (!live) return;
        setPhase({ kind: "ready", loaded: { manifest, towns }, rows, town });
        setApprox(guessed);

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
        // A link that names a town the index does not publish is rewritten to the one on screen — the URL must not
        // claim a town that is not there. A board that simply *chose* a town (from the edge's country, or the first
        // published) leaves the URL clean: that choice belongs to the visitor's location, not to the link.
        if (wantedTown && town.town !== wantedTown) {
          const url = new URL(location.href);
          url.searchParams.set("town", town.town);
          history.replaceState(null, "", url);
        }
        // The artist page's way back returns to this town, not to the first published one.
        try {
          sessionStorage.setItem("lineup.town", town.town);
        } catch {
          /* storage refused: back falls back to the root */
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
    rememberTown(id);
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
      forgetTown();
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
  // Where distances are measured from: only a device fix, because it is the only end that is a person. The town's
  // own point is used to *choose* a town, never to print kilometres.
  const origin = useMemo<Origin | null>(
    () => (position.state === "granted" ? { basis: "device", point: position.point } : null),
    [position],
  );
  const distanceFor = useMemo(
    () =>
      (row: Row): number | null => {
        if (!origin) return null;
        const point = pointOf(row);
        return point ? distanceKm(origin.point, point) : null;
      },
    [origin],
  );
  // A reach narrows by distance, and a row without a position cannot be judged by it: those rows leave the view and
  // are counted instead, never kept as though they had passed the test.
  const scoped = useMemo(() => {
    if (!origin || reach === null) return { rows: inWindow, unplaced: 0, beyond: 0 };
    const rows: Row[] = [];
    let unplaced = 0;
    let beyond = 0;
    for (const row of inWindow) {
      const point = pointOf(row);
      if (!point) {
        unplaced += 1;
        continue;
      }
      if (distanceKm(origin.point, point) <= reach) rows.push(row);
      else beyond += 1;
    }
    return { rows, unplaced, beyond };
  }, [inWindow, origin, reach]);
  const shown = useMemo(() => {
    return filter === "yours" && mineKeys.size > 0 ? scoped.rows.filter((row) => mineKeys.has(row.k)) : scoped.rows;
  }, [filter, mineKeys, scoped]);

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

  // The reach lives in the URL like every other choice, and is dropped when it is "anywhere".
  const applyReach = useCallback((km: number | null) => {
    setReach(km);
    history.replaceState(null, "", reachToURL(km, location.href));
  }, []);

  /* A granted fix answers what the edge could only guess: the nearest published town, and a distance for every row.
     A link that named a town keeps it — the visitor asked for that town, and that outranks where they are standing. */
  useEffect(() => {
    if (position.state !== "granted") return;
    setApprox(false);
    if (wantedTown || !ready) return;
    const near = nearestTown(ready.loaded.towns, position.point);
    if (near && near.town.id !== ready.town.town) chooseTown(near.town.id);
  }, [position, wantedTown, ready, chooseTown]);

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
    // An empty travel window is not an empty board: the town has shows, just not in these dates.
    if (inWindow.length === 0 && phase.rows.length > 0) {
      const from = dateWindow?.from ?? "";
      const to = dateWindow?.to ?? "";
      const nextDay = phase.rows.map((row) => dayOf(row.d)).find((day) => day > to);
      const prevDay = [...phase.rows].reverse().map((row) => dayOf(row.d)).find((day) => day < from);
      const jump = (day: string) => {
        const end = addDays(day, 6);
        applyWindow({ from: day, to: end > published!.to ? published!.to : end });
      };
      return (
        <div className="state">
          <h2>
            Nothing between {friendly(from)} and {friendly(to)}.
          </h2>
          <p>
            <span className="num">{phase.rows.length}</span> shows are in this town, just not in those dates.
            {nextDay ? ` The next one is ${friendly(nextDay)}.` : prevDay ? ` The last one was ${friendly(prevDay)}.` : ""}
          </p>
          <div className="cta">
            {nextDay ? (
              <button className="btn primary" onClick={() => jump(nextDay)}>
                Jump to {friendly(nextDay)}
              </button>
            ) : null}
            {published ? (
              <button className="btn" onClick={() => applyWindow(published)}>
                Show the whole season
              </button>
            ) : null}
            <button className="btn" onClick={() => setPalette({ open: true, tab: "town" })}>
              Another town
            </button>
          </div>
        </div>
      );
    }
    // A reach that leaves nothing is a distance question, not a taste one: offer the next stop up, then everywhere.
    if (shown.length === 0 && origin && reach !== null && scoped.rows.length === 0) {
      const wider = REACH_STEPS.find((km) => km > reach) ?? null;
      return (
        <div className="state">
          <h2>Nothing within {reach} km of you.</h2>
          <p>
            <span className="num">{scoped.beyond}</span> {scoped.beyond === 1 ? "show is" : "shows are"} further out
            {scoped.unplaced > 0 ? (
              <>
                , and <span className="num">{scoped.unplaced}</span> have no venue position to measure
              </>
            ) : null}
            . Nothing is broken — the town has them either way.
          </p>
          <div className="cta">
            {wider ? (
              <button className="btn primary" onClick={() => applyReach(wider)}>
                Within {wider} km
              </button>
            ) : null}
            <button className={wider ? "btn" : "btn primary"} onClick={() => applyReach(null)}>
              Anywhere in town
            </button>
          </div>
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
    const boardTown = ready ? townLabel(ready.town, ready.loaded.towns) : "";
    return (
      <Board
        rows={shown}
        tasteKeys={tasteKeys}
        newKeys={newKeys}
        townForRow={() => boardTown}
        distanceFor={distanceFor}
      />
    );
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
              {/* The count and the stamp are the two things that arrive late, so they are the two that get a placeholder
                  of the same width: the label reads "Season board · ▁ days" and never reflows when the number lands. */}
              <span className="lab">
                {dateWindow && published && dateWindow.from !== published.from ? "Travel" : "Season"} board
                {" · "}
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
                ) : (
                  <span className="sk-num" aria-hidden="true" />
                )}{" "}
                days
              </span>
              <span className="r">
                <span className="lab">As of</span>{" "}
                {manifest ? (
                  <span className="num" style={{ fontSize: 11, color: "var(--text-2)" }}>
                    {stampLabel(manifest.built_at)}
                  </span>
                ) : (
                  <span className="sk-stamp" aria-hidden="true" />
                )}
              </span>
            </div>
            <div className="h1row">
              <h1>
                {phase.kind !== "ready" ? (
                  <span className="sk-h1" aria-hidden="true" />
                ) : null}
                <button
                  className="townbtn"
                  hidden={phase.kind !== "ready"}
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
              {/* A guessed town says so, beside its name: an unlabelled guess is a lie. The chip is the sheet's own. */}
              {approx ? <span className="approx">approx.</span> : null}
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
                town={ready?.town.town ?? ""}
                dateWindow={dateWindow ?? published ?? manifest.window}
                artistNames={taste.map((entry) => entry.name)}
                reach={reach}
                unplaced={scoped.unplaced}
                located={origin !== null}
                shown={scoped.rows.length}
              />
              </>
            ) : (
              // The ribbon and the coverage line are ~90px of head; reserving them is the difference between a page
              // that settles once and a page that jumps when the index arrives.
              <div className="headsk" aria-hidden="true">
                <div className="sk-ribbon" />
                <div className="sk-coverage" />
              </div>
            )}

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

      {/* The board's own action, in reach of a thumb: taste is something you do to the whole board, not to the town. */}
      {/* The control that opens the sheet is not offered while the sheet is open: behind a scrim it is a dead button. */}
      {palette.open ? null : (
        <Fab
          onClick={() => setPalette({ open: true, tab: "taste" })}
          badge={tasteKeys.size > 0 ? String(tasteKeys.size) : undefined}
          label={tasteKeys.size > 0 ? `Set taste, ${tasteKeys.size} artists` : "Set taste"}
        >
          Set taste
        </Fab>
      )}

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
        position={position}
        canLocate={locationSupported()}
        onLocate={locate}
        reach={reach}
        onReach={applyReach}
      />
    </>
  );
}

/** `2027-01-15` → `15 jan`, the board's own voice for a day. */
function friendly(day: string): string {
  if (!day) return "";
  return `${Number(dayNumber(day))} ${monthShort(day)}`;
}
