import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CaretDown } from "@phosphor-icons/react";
import Board from "./components/Board";
import Calendar, { DaySheet } from "./components/Calendar";
import Chrome from "./components/Chrome";
import Preferences, { type Panel } from "./components/Preferences";
import Wizard from "./components/Wizard";
import { Empty, Failed, Skeleton } from "./components/states";
import { filterMine, groupByDay, localDay, mineKeys as computeMine, monthOf } from "./lib/board";
import { distanceKm, pointOf, type Point } from "./lib/geo";
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
import { locationSupported, useDevicePosition } from "./lib/location";
import { loadPortraits, type PortraitMap } from "./lib/portraits";
import { readOnboarded, readTaste, readTown, readView, writeOnboarded, writeTaste, writeTown, writeView, type View } from "./lib/storage";
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
  /** Which town's shard is on screen or in flight — the effect's own guard, so it never depends on its output. */
  const loadedTown = useRef<string | null>(null);
  /** Whether a setting changed while a panel was open: closing must then keep the URL, not pop back to a stale one. */
  const panelDirty = useRef(false);

  const { theme, toggle: toggleTheme } = useTheme();
  const { position, request: locate } = useDevicePosition();
  const wide = useWide();
  const canLocate = locationSupported();
  const today = localDay();

  const townName = town && loaded ? townLabel(town, loaded.towns) : "";

  /* ── data ────────────────────────────────────────────────────────────────────────────────────── */

  useEffect(() => {
    let live = true;
    loadIndex()
      .then((next) => {
        if (live) setLoaded(next);
      })
      .catch((err: unknown) => {
        if (live) setError(err instanceof Error ? err.message : String(err));
      });
    loadPortraits().then((map) => {
      if (live) setPortraits(map);
    });
    return () => {
      live = false;
    };
  }, [nonce]);

  useEffect(() => {
    if (!loaded) return;
    const id = wantedTown ?? loaded.manifest.towns[0]!.town;
    const entry = loaded.manifest.towns.find((candidate) => candidate.town === id) ?? loaded.manifest.towns[0]!;
    if (loadedTown.current === entry.town) return;
    loadedTown.current = entry.town;
    let live = true;
    setSwitching(true);
    setTown(entry);
    loadShard(entry.shard)
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
    return filter === "mine" && mine.size > 0 ? filterMine(rows, mine) : rows;
  }, [rows, filter, mine]);
  const days = useMemo(() => groupByDay(visible), [visible]);
  const sheetRows = useMemo(
    () => (daySheet ? (days.find((group) => group.day === daySheet)?.rows ?? []) : []),
    [daySheet, days],
  );

  const firstMonth = rows && rows.length > 0 ? monthOf(dayOf(rows[0]!.d)) : monthOf(today);
  const shownMonth = month ?? firstMonth;

  const hasTaste = taste.length > 0;
  const mineMode = filter === "mine" && mine.size > 0;

  const finishSetup = useCallback(
    (entries: TasteEntry[]) => {
      writeOnboarded();
      writeTaste(entries);
      setTaste(entries);
      if (!entries.length) setFilter("all");
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
          <span className="sign" data-t={townName}>
            {townName}
          </span>
          <CaretDown size={16} />
        </button>
      </h1>
      {rows ? (
        <p className="count">
          <span>
            <span className="num">{visible.length}</span> {visible.length === 1 ? "show" : "shows"}
          </span>
          {hasTaste ? (
            <>
              <span className="dot" aria-hidden="true">
                ·
              </span>
              <button className="count__swap" onClick={() => setFilter(mineMode ? "all" : "mine")}>
                {mineMode ? (
                  <>
                    all <span className="num">{rows.length}</span>
                  </>
                ) : (
                  <>
                    yours <span className="num">{mine.size}</span>
                  </>
                )}
              </button>
            </>
          ) : null}
        </p>
      ) : (
        <p className="count" aria-hidden="true">
          <span className="sk__count" style={{ display: "inline-block", margin: 0 }} />
        </p>
      )}
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
    if (!loaded || !town || !rows) return <Skeleton />;
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
      return (
        <Empty
          title="None of your artists play here."
          detail={`${rows.length} shows are on the board.`}
          action={{ label: `Show all ${rows.length}`, onClick: () => setFilter("all") }}
          secondary={{ label: "Add artists", onClick: () => openPanel("taste") }}
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
      />
    );
  })();

  const foot = (
    <div className="foot">
      <span>
        Lineup{" "}
        <span className="num">
          {town ? `${town.sha256.slice(0, 7)} · ${loaded ? stampLabel(loaded.manifest.built_at) : "—"}` : "—"}
        </span>
      </span>
      {portraits && portraits.size > 0 ? (
        <a href="https://commons.wikimedia.org" target="_blank" rel="noopener noreferrer">
          Portraits · Wikimedia Commons
        </a>
      ) : null}
    </div>
  );

  return (
    <>
      <div className="stage" aria-hidden="true" />
      <Chrome
        view={view}
        onView={applyView}
        onPanel={() => openPanel("taste")}
        location={
          canLocate
            ? { state: position.state, canLocate, onLocate: locate }
            : undefined
        }
      />
      <main className="page" data-view={view} data-switching={switching || undefined}>
        {head}
        <div id="board">{body}</div>
        {foot}
      </main>

      {loaded && town && (setup || panel !== null) ? (
        <>
          {setup ? (
            <Wizard
              posts={loaded.manifest.towns}
              names={loaded.towns}
              taste={taste}
              onTaste={setTaste}
              town={town.town}
              onTown={applyTown}
              position={position}
              canLocate={canLocate}
              onLocate={locate}
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
            onTown={applyTown}
            posts={loaded.manifest.towns}
            names={loaded.towns}
            position={position}
            canLocate={canLocate}
            onLocate={locate}
            theme={theme}
            onTheme={toggleTheme}
          />
        </>
      ) : null}
    </>
  );
}
