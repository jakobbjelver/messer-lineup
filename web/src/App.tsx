import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Board from "./components/Board";
import Ribbon from "./components/Ribbon";
import { CaretDown, Info, MicrophoneStage, Moon, Sun } from "./components/icons";
import { createLamps, type Lamps } from "./lib/rays";
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
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    document.documentElement.classList.contains("dark") ? "dark" : "light",
  );
  const [pickerOpen, setPickerOpen] = useState(false);
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
        if (wantedTown !== town.town) {
          // The URL always names what is on screen, so a copied link opens the same board.
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
    document.documentElement.classList.toggle("dark", theme === "dark");
    try {
      localStorage.setItem("lineup.theme", theme);
    } catch {
      /* a browser that refuses storage still renders */
    }
    lamps.current?.retint();
  }, [theme]);

  const chooseTown = useCallback((id: string) => {
    setPickerOpen(false);
    setPhase({ kind: "loading" });
    setWantedTown(id);
    const url = new URL(location.href);
    url.searchParams.set("town", id);
    history.replaceState(null, "", url);
  }, []);

  const retry = useCallback(() => {
    setPhase({ kind: "loading" });
    setWantedTown((current) => current ?? "");
  }, []);

  const body = useMemo(() => {
    if (phase.kind === "loading") {
      return (
        <div className="state">
          <h2>Reading the index…</h2>
          <p>The manifest first, then one shard for this town. A page view costs nothing to serve.</p>
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
            The town is published, but no show in it lands inside the <span className="num">{phase.loaded.manifest.window.days}</span>-day
            window. Nothing is broken — the next sweep replaces the shard.
          </p>
        </div>
      );
    }
    return <Board rows={phase.rows} />;
  }, [phase, retry]);

  const ready = phase.kind === "ready" ? phase : null;
  const manifest = ready?.loaded.manifest;
  const townName = ready ? townLabel(ready.town, ready.loaded.towns) : "…";
  const sourceCount = ready ? new Set(ready.rows.flatMap((row) => row.src)).size : 0;

  return (
    <>
      <div className="stage" aria-hidden="true">
        <div className="stage__blobs" />
        <div className="stage__lift" />
        <div className="stage__grain" />
      </div>
      <canvas id="rays" ref={canvas} aria-hidden="true" />

      <header className="bar">
        <div className="bar__row">
          <a className="brand" href="/" aria-label="Messer Lineup">
            <MicrophoneStage />
            <b>Messer Lineup</b>
          </a>
          <div className="bar__actions">
            <button
              className="iconbtn"
              onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
              aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
            >
              {theme === "dark" ? <Sun /> : <Moon />}
            </button>
          </div>
        </div>
      </header>

      <main className="page">
        <section className="sheet">
          <div className="head">
            <div className="eyebrow">
              <span className="lab">
                Season board · <span className="num">{manifest?.window.days ?? 0}</span> days
              </span>
              <span className="r">
                <span className="lab">As of</span>{" "}
                <span className="num" style={{ fontSize: 11, color: "var(--text-2)" }}>
                  {manifest ? stampLabel(manifest.built_at) : "—"}
                </span>
              </span>
            </div>
            <div className="h1row">
              <h1>
                <button
                  className="townbtn"
                  onClick={() => setPickerOpen((open) => !open)}
                  aria-expanded={pickerOpen}
                  aria-label="Change town"
                >
                  <span
                    className="sign"
                    data-t={townName}
                    style={{
                      // The sign's two gradient stops are per-theme by design; the sheen itself is CSS.
                      ["--sign-a" as string]: theme === "dark" ? "oklch(.88 .20 132)" : "oklch(.44 .17 128)",
                      ["--sign-b" as string]: theme === "dark" ? "oklch(.74 .18 170)" : "oklch(.52 .15 150)",
                    }}
                  >
                    {townName}
                  </span>
                  <span className="approx" hidden={phase.kind !== "ready"}>
                    {ready?.rows.some((r) => r.g) ? "exact" : "approx."}
                  </span>
                  <CaretDown />
                </button>
              </h1>
              <div className="spacer" />
              <div className="actions">
                {pickerOpen && manifest ? (
                  <div className="seg" role="group" aria-label="Towns">
                    {manifest.towns.map((entry) => (
                      <button
                        key={entry.town}
                        aria-pressed={entry.town === ready?.town.town}
                        onClick={() => chooseTown(entry.town)}
                      >
                        {townLabel(entry, ready?.loaded.towns ?? [])} <span className="num">{entry.count}</span>
                      </button>
                    ))}
                  </div>
                ) : null}
              </div>
            </div>
          </div>

          {manifest && ready ? (
            <>
              <Ribbon
                from={manifest.window.from}
                days={manifest.window.days}
                rows={ready.rows}
                builtOn={dayOf(manifest.built_at)}
              />
              <button
                className="coverage"
                title={
                  manifest.sources
                    .map((s) => `${s.id}: ${s.ok ? `${s.rows ?? 0} rows` : "failed"}${s.note ? ` — ${s.note}` : ""}`)
                    .join("\n") || "no sources reported"
                }
              >
                <span>
                  <span className="num">{ready.rows.length}</span> shows
                </span>
                <span className="g">·</span>
                <span className="g">
                  <span className="num">{sourceCount}</span> {sourceCount === 1 ? "source" : "sources"}
                </span>
                <span className="i">
                  <Info />
                </span>
              </button>
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
    </>
  );
}
