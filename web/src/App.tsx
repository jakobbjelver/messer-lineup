import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CalendarBlank, CaretDown, GlobeHemisphereWest, ListBullets } from "@phosphor-icons/react";
import Board from "./components/Board";
import Calendar, { DaySheet } from "./components/Calendar";
import Chrome from "./components/Chrome";
import Preferences, { type Panel } from "./components/Preferences";
import Subscribe from "./components/Subscribe";
import Wizard from "./components/Wizard";
import Loader from "./components/Loader";
import { Empty, Failed } from "./components/states";
import { filterMine, groupByDay, localDay, mineKeys as computeMine, monthOf } from "./lib/board";
import { distanceKm, nearestTown, pointOf, type Point } from "./lib/geo";
import {
  ANYWHERE,
  dayOf,
  loadBoard,
  loadIndex,
  stampLabel,
  townLabel,
  type Manifest,
  type Row,
  type Town,
  type TownEntry,
} from "./lib/indexData";
import { locationSupported, useDevicePosition } from "./lib/location";
import { loadPortraits, sourceNames, type PortraitMap } from "./lib/portraits";
import {
  followingSource,
  readOnboarded,
  readTaste,
  readTown,
  readView,
  writeImport,
  writeOnboarded,
  writeTaste,
  writeTown,
  writeView,
  type View,
} from "./lib/storage";
import { readNamesParam, readTasteParam, toEntries, writeTasteParam, type TasteEntry } from "./lib/taste";
import { useTheme } from "./lib/theme";
import { useWide } from "./lib/useMediaQuery";

const params = new URLSearchParams(location.search);
const urlTown = params.get("town");
const urlTaste = readTasteParam(location.search);
const startTaste = urlTaste.length
  ? toEntries(urlTaste, readNamesParam(location.search))
  : readTaste();

/**
 * The manifest entry for a scope. `anywhere` has no shard of its own — it is every shard — so the entry is the sum of
 * what is published, and the board loads it by union. Keeping the shape means every consumer of a town (the head, the
 * foot, the picker's count) needs no second code path.
 */
function scopeEntry(manifest: Manifest, scope: string): TownEntry {
  const first = manifest.towns[0]!;
  if (scope !== ANYWHERE) return manifest.towns.find((entry) => entry.town === scope) ?? first;
  const published = manifest.towns.filter((entry) => entry.count > 0);
  return {
    town: ANYWHERE,
    shard: "",
    sha256: "",
    count: published.reduce((sum, entry) => sum + entry.count, 0),
    sources: [...new Set(published.flatMap((entry) => entry.sources))],
    built_at: manifest.built_at,
  };
}

/**
 * The board.
 *
 * One surface, two views, one gate. The index is fetched once and each shard once per session; a
 * town switch is a crossfade over rows that are already in memory or in the HTTP cache, and a view
 * switch is not a request at all. Everything the reader chose lives in four storage keys and the URL.
 */
export default function App() {
  const [loaded, setLoaded] = useState<{ manifest: Manifest; towns: Town[] } | null>(null);
  const [town, setTown] = useState<TownEntry | null>(null);
  const [rows, setRows] = useState<Row[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);
  const [switching, setSwitching] = useState(false);

  const [taste, setTaste] = useState<TasteEntry[]>(startTaste);
  const [wantedTown, setWantedTown] = useState<string | null>(() => urlTown ?? readTown());
  const [view, setView] = useState<View>(() => (params.get("view") === "calendar" ? "calendar" : readView()));
  const [panel, setPanel] = useState<Panel | null>(() => {
    const value = params.get("panel");
    return value === "taste" || value === "town" ? value : null;
  });
  const [filter, setFilter] = useState<"mine" | "all">(startTaste.length ? "mine" : "all");
  const [setup, setSetup] = useState(() => !readOnboarded() && !urlTown && startTaste.length === 0);
  const [daySheet, setDaySheet] = useState<string | null>(null);
  const [month, setMonth] = useState<string | null>(null);
  const [portraits, setPortraits] = useState<PortraitMap | undefined>(undefined);
  /** The town a device fix chose. Not the town: a pick releases it, and a released pin is not re-applied. */
  const [fixTown, setFixTown] = useState<string | null>(null);
  /** The fix already applied, so one position can only ever move the reader once. */
  const appliedFix = useRef<string | null>(null);
  /** Which town's shard is on screen or in flight — the effect's own guard, so it never depends on its output. */
  const loadedTown = useRef<string | null>(null);
  /** Whether a setting changed while a panel was open: closing must then keep the URL, not pop back to a stale one. */
  const panelDirty = useRef(false);

  const { theme, toggle: toggleTheme } = useTheme();
  const { position, request: locate } = useDevicePosition();
  const wide = useWide();
  const canLocate = locationSupported();
  const today = localDay();

  const townName = town && loaded ? (town.town === ANYWHERE ? "Anywhere" : townLabel(town, loaded.towns)) : "";

  /* ── data ────────────────────────────────────────────────────────────────────────────────────── */

  useEffect(() => {
    let live = true;
    loadIndex()
      .then((next) => {
        if (!live) return null;
        setLoaded(next);
        // The manifest names the portraits file; a build from before content addressing has no `files` map, and the
        // loader falls back to the legacy name.
        return loadPortraits(next.manifest.files?.portraits);
      })
      .then((map) => {
        if (live && map) setPortraits(map);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      live = false;
    };
  }, [nonce]);

  useEffect(() => {
    if (!loaded) return;
    const entry = scopeEntry(loaded.manifest, wantedTown ?? loaded.manifest.towns[0]!.town);
    if (loadedTown.current === entry.town) return;
    loadedTown.current = entry.town;
    let live = true;
    setSwitching(true);
    setTown(entry);
    loadBoard(loaded.manifest, entry.town)
      .then((next) => {
        if (!live) return;
        setRows(next);
        setError(null);
        window.scrollTo({ top: 0, behavior: "instant" as ScrollBehavior });
      })
      .catch((err: unknown) => {
        if (!live) return;
        loadedTown.current = null;
        setRows(null);
        setError(err instanceof Error ? err.message : String(err));
      })
      .finally(() => {
        if (live) setSwitching(false);
      });
    return () => {
      live = false;
    };
  }, [loaded, wantedTown, nonce]);

  /* ── the reader's choices, in the URL and in storage ──────────────────────────────────────────── */

  const setTasteAndStore = useCallback((entries: TasteEntry[]) => {
    setTaste(entries);
    writeTaste(entries);
    // One edit and the list is the reader's, not a provider's: the provenance goes with it, so a feed that was
    // following a handle stops following one the reader has overruled. A list that still equals the import keeps it.
    if (!followingSource(entries)) writeImport(null);
    const next = writeTasteParam(entries, location.href);
    if (history.state && typeof history.state === "object" && "panel" in history.state) panelDirty.current = true;
    history.replaceState(history.state, "", next);
    if (entries.length === 0) setFilter("all");
  }, []);

  const applyTown = useCallback((id: string) => {
    setWantedTown(id);
    writeTown(id);
    setMonth(null);
    const url = new URL(location.href);
    if (url.searchParams.get("town") === id) return;
    url.searchParams.set("town", id);
    // A change made inside the settings sheet must survive the sheet closing. `closePanel` pops the history entry
    // it pushed, which would restore the town that was on screen before the sheet opened — so the change is
    // recorded, and closing rewrites the current entry instead of popping.
    if (history.state && typeof history.state === "object" && "panel" in history.state) panelDirty.current = true;
    history.replaceState(history.state, "", url);
  }, []);

  /**
   * A fix chooses a town once, here, and nowhere else.
   *
   * This used to live in the panels (`useNearestTown`), which meant the effect ran again every time the settings
   * sheet mounted — so a reader who pinned their location, then picked another town, found the pin had silently
   * taken the town back the next time they opened the sheet. One fix, applied once: the point itself is the guard.
   */
  useEffect(() => {
    if (position.state !== "granted" || !loaded) return;
    // Anywhere is not a place a fix can promote you out of: the scope means everywhere, so a position only adds
    // distances to the rows. Promoting a town here would silently answer a question the reader did not ask.
    if ((wantedTown ?? "") === ANYWHERE) return;
    const point = `${position.point.lat},${position.point.lon}`;
    if (appliedFix.current === point) return;
    appliedFix.current = point;
    const published = loaded.towns.filter((entry) => loaded.manifest.towns.some((post) => post.town === entry.id));
    const near = nearestTown(published, position.point as Point);
    if (!near) return;
    setFixTown(near.town.id);
    applyTown(near.town.id);
  }, [position, loaded, applyTown, wantedTown]);

  /** The reader's own pick: it outranks the fix, so it releases the pin rather than fighting it on the next open. */
  const pickTown = useCallback(
    (id: string) => {
      setFixTown(null);
      applyTown(id);
    },
    [applyTown],
  );

  /** Put the fix back to work without asking the browser again, or give it up and keep the town as it is. */
  const useMyLocation = useCallback(() => {
    if (position.state !== "granted") {
      locate();
      return;
    }
    if (!loaded) return;
    // On anywhere the fix is for distances only — there is no town to move to, and moving would answer a different
    // question than the one "Anywhere" asked.
    if ((wantedTown ?? "") === ANYWHERE) return;
    const published = loaded.towns.filter((entry) => loaded.manifest.towns.some((post) => post.town === entry.id));
    const near = nearestTown(published, position.point as Point);
    if (!near) return;
    setFixTown(near.town.id);
    applyTown(near.town.id);
  }, [position, loaded, applyTown, locate, wantedTown]);

  const releaseFix = useCallback(() => setFixTown(null), []);

  const applyView = useCallback((next: View) => {
    setView(next);
    writeView(next);
    const url = new URL(location.href);
    if (next === "calendar") url.searchParams.set("view", "calendar");
    else url.searchParams.delete("view");
    history.replaceState(history.state, "", url);
  }, []);

  const openPanel = useCallback((next: Panel) => {
    setPanel(next);
    const url = new URL(location.href);
    url.searchParams.set("panel", next);
    history.pushState({ panel: next }, "", url);
  }, []);

  const closePanel = useCallback(() => {
    setPanel(null);
    const pushed = history.state !== null && typeof history.state === "object" && "panel" in history.state;
    const url = new URL(location.href);
    url.searchParams.delete("panel");
    if (pushed && !panelDirty.current) {
      history.back();
      return;
    }
    panelDirty.current = false;
    history.replaceState(null, "", url);
  }, []);

  useEffect(() => {
    const onPop = () => {
      const value = new URLSearchParams(location.search).get("panel");
      setPanel(value === "taste" || value === "town" ? value : null);
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  /* A fix adds a distance. It never moves the reader to another town — only the pickers do that. */
  const origin = useMemo<Point | null>(
    () => (position.state === "granted" ? position.point : null),
    [position],
  );
  const distanceFor = useMemo(
    () =>
      (row: Row): number | null => {
        if (!origin) return null;
        const point = pointOf(row);
        return point ? distanceKm(origin, point) : null;
      },
    [origin],
  );

  /* ── the board's data ────────────────────────────────────────────────────────────────────────── */

  const tasteKeys = useMemo(() => new Set(taste.map((entry) => entry.key)), [taste]);
  const mine = useMemo(() => computeMine(rows ?? [], tasteKeys), [rows, tasteKeys]);
  const visible = useMemo(() => {
    if (!rows) return [];
    return filter === "mine" ? filterMine(rows, mine) : rows;
  }, [rows, filter, mine]);
  const days = useMemo(() => groupByDay(visible), [visible]);
  const sheetRows = useMemo(
    () => (daySheet ? (days.find((group) => group.day === daySheet)?.rows ?? []) : []),
    [daySheet, days],
  );

  const firstMonth = rows && rows.length > 0 ? monthOf(dayOf(rows[0]!.d)) : monthOf(today);
  const shownMonth = month ?? firstMonth;

  const hasTaste = taste.length > 0;
  const mineMode = filter === "mine";
  /** The scope is `anywhere`: every published town at once. Never a default — only the URL or an explicit pick. */
  const anywhere = town?.town === ANYWHERE;
  /**
   * A live list to follow, only while the list is still exactly what the import produced. One edit and the reader has
   * said "this is my list", so the snapshot in the token is the truth again.
   */
  const source = useMemo(() => (taste.length > 0 ? followingSource(taste) : null), [taste]);

  const finishSetup = useCallback(
    (entries: TasteEntry[]) => {
      writeOnboarded();
      writeTaste(entries);
      setTaste(entries);
      // A gate that just collected a list lands on that list: `yours` is the board the visitor built, and making them
      // press a toggle to see it reads as though the choice did not take.
      setFilter(entries.length ? "mine" : "all");
      if (wantedTown) writeTown(wantedTown);
      setSetup(false);
    },
    [wantedTown],
  );

  /* ── render ──────────────────────────────────────────────────────────────────────────────────── */

  const head = (
    <section className="head">
      <h1 className="head__h1">
        <button
          className="townbtn"
          onClick={() => openPanel("town")}
          aria-expanded={panel === "town"}
          aria-label={townName ? `Change town, now ${townName}` : "Choose a town"}
          disabled={!town}
        >
          {/* The scope is not a town, so it wears the glyph that says so — ink, not accent: the accent is for state,
              and "anywhere" is an identity. Sized in `em` so it scales with the sign it sits beside. */}
          {anywhere ? <GlobeHemisphereWest size={24} className="globe" aria-hidden="true" /> : null}
          <span className="sign" data-t={townName}>
            {townName}
          </span>
          <CaretDown size={16} className="caret" />
        </button>
      </h1>
      {rows && town ? (
        <div className="head__row">
          <div className="seg" role="group" aria-label="Whose shows">
            <button aria-pressed={filter === "mine"} onClick={() => setFilter("mine")}>
              yours <span className="num">{mine.size}</span>
            </button>
            <button aria-pressed={filter === "all"} onClick={() => setFilter("all")}>
              all <span className="num">{rows.length}</span>
            </button>
          </div>
          <div className="head__end">
            <div className="seg seg--icons" role="group" aria-label="View">
              <button aria-pressed={view === "list"} aria-label="List view" onClick={() => applyView("list")}>
                <ListBullets size={15} />
              </button>
              <button aria-pressed={view === "calendar"} aria-label="Calendar view" onClick={() => applyView("calendar")}>
                <CalendarBlank size={15} />
              </button>
            </div>
            {/* One control, one mount: the board's head row is the only place a subscription is offered. The label
                says Subscribe and nothing else; everything it means lives behind the mark beside it. */}
            <Subscribe
              scope={town.town}
              label={townName ?? ""}
              artists={taste}
              matched={mine.size}
              total={rows.length}
              source={source}
            />
          </div>
        </div>
      ) : null}
    </section>
  );

  const body = (() => {
    if (error) {
      return (
        <Failed
          message={error}
          onRetry={() => {
            loadedTown.current = null;
            setNonce((n) => n + 1);
          }}
        />
      );
    }
    if (!loaded || !town || !rows) {
      return (
        <div className="loading">
          <Loader size={46} label="Reading the board" />
        </div>
      );
    }
    if (rows.length === 0) {
      return (
        <Empty
          title="Nothing here yet."
          detail="This town has no shows in the published window."
          action={{ label: "Change town", onClick: () => openPanel("town") }}
        />
      );
    }
    if (visible.length === 0) {
      return hasTaste ? (
        <Empty
          title={anywhere ? "None of your artists play anywhere." : "None of your artists play here."}
          detail={
            anywhere
              ? `${rows.length} shows across ${loaded.manifest.towns.length} towns.`
              : `${rows.length} shows are on the board.`
          }
          action={{ label: `Show all ${rows.length}`, onClick: () => setFilter("all") }}
          secondary={{ label: "Add artists", onClick: () => openPanel("taste") }}
        />
      ) : (
        <Empty
          title="No artists yet."
          detail="Pick the ones you go see — this address is your board."
          action={{ label: "Add artists", onClick: () => openPanel("taste") }}
          secondary={{ label: `Show all ${rows.length}`, onClick: () => setFilter("all") }}
        />
      );
    }
    if (view === "calendar") {
      return (
        <>
          <Calendar
            days={days}
            month={shownMonth}
            onMonth={setMonth}
            portraits={portraits}
            today={today}
            from={loaded.manifest.window.from}
            to={loaded.manifest.window.to}
            maxFaces={wide ? 3 : 2}
            onDay={setDaySheet}
          />
          <DaySheet
            day={daySheet}
            rows={sheetRows}
            portraits={portraits}
            onClose={() => setDaySheet(null)}
          />
        </>
      );
    }
    return (
      <Board
        days={days}
        portraits={portraits}
        mine={mine}
        markMine={!mineMode && hasTaste}
        distanceFor={distanceFor}
        today={today}
        townOf={anywhere ? (row) => townLabel({ town: row.c }, loaded.towns) : undefined}
      />
    );
  })();

  const foot = (
    <div className="foot">
      <span>
        Lineup{" "}
        <span className="num">
          {town
            ? `${town.sha256 ? `${town.sha256.slice(0, 7)} · ` : ""}${loaded ? stampLabel(loaded.manifest.built_at) : "—"}`
            : "—"}
        </span>
      </span>
      {portraits && portraits.size > 0 ? (
        <span className="foot__credit">
          Portraits · {sourceNames(portraits).join(" · ")}
        </span>
      ) : null}
    </div>
  );

  return (
    <>
      <div className="stage" aria-hidden="true" />
      <Chrome onPanel={() => openPanel("taste")} />
      <main className="page" data-view={view} data-switching={switching || undefined}>
        {rows ? head : null}
        <div id="board">{body}</div>
        {rows ? foot : null}
      </main>

      {loaded && town && (setup || panel !== null) ? (
        <>
          {setup ? (
            <Wizard
              posts={loaded.manifest.towns}
              names={loaded.towns}
              taste={taste}
              onTaste={setTaste}
              portraits={portraits}
              town={town.town}
              onTown={pickTown}
              position={position}
              canLocate={canLocate}
              onLocate={useMyLocation}
              located={fixTown !== null && town.town === fixTown}
              onRelease={releaseFix}
              onDone={() => finishSetup(taste)}
              onSkip={() => finishSetup(taste)}
            />
          ) : null}
          <Preferences
            open={panel !== null}
            tab={panel ?? "taste"}
            onTab={(next) => {
              setPanel(next);
              const url = new URL(location.href);
              url.searchParams.set("panel", next);
              history.replaceState({ panel: next }, "", url);
            }}
            onClose={closePanel}
            taste={taste}
            onTaste={setTasteAndStore}
            town={town.town}
            onTown={pickTown}
            posts={loaded.manifest.towns}
            names={loaded.towns}
            portraits={portraits}
            position={position}
            canLocate={canLocate}
            onLocate={useMyLocation}
            located={fixTown !== null && town.town === fixTown}
            onRelease={releaseFix}
            theme={theme}
            onTheme={toggleTheme}
          />
        </>
      ) : null}
    </>
  );
}
