import { useEffect, useMemo, useState } from "react";
import Board from "./Board";
import TopBar from "./TopBar";
import { ArrowLeft } from "./icons";
import {
  loadArtistRows,
  loadIndex,
  stampLabel,
  townLabel,
  type ArtistShow,
  type Manifest,
  type Town,
  type TownEntry,
} from "../lib/indexData";
import { useTheme } from "../lib/theme";

/**
 * `/a/<key>` — one artist, every date in the reach.
 *
 * The board answers "what is on near me this season"; this page answers "when can I see *this* act", which is the
 * other question the index can answer without any new data. It reads every town's shard, because an artist playing
 * two towns is exactly the case a single-town board hides, and each row says which town it is in.
 *
 * No WebGL lamp here: this page is a list, and the atmospheric layer belongs to the board's stage.
 */
export default function ArtistRoute({ artistKey }: { artistKey: string }) {
  const { theme, toggle } = useTheme();
  const [phase, setPhase] = useState<
    { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; shows: ArtistShow[]; manifest: Manifest; towns: Town[]; entries: TownEntry[] }
  >({ kind: "loading" });

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const { manifest, towns } = await loadIndex();
        const shows = await loadArtistRows(manifest, artistKey);
        if (!live) return;
        setPhase({ kind: "ready", shows, manifest, towns, entries: manifest.towns });
      } catch (error) {
        if (!live) return;
        setPhase({ kind: "error", message: error instanceof Error ? error.message : "the index could not be read" });
      }
    })();
    return () => {
      live = false;
    };
  }, [artistKey]);

  const name = phase.kind === "ready" && phase.shows.length > 0 ? phase.shows[0].a : prettyKey(artistKey);
  useEffect(() => {
    document.title = `${name} — Messer Lineup`;
  }, [name]);

  const townOf = useMemo(() => {
    const map = new Map<string, string>();
    if (phase.kind === "ready") {
      for (const entry of phase.entries) {
        const label = townLabel(entry, phase.towns);
        for (const show of phase.shows) if (show.town === entry.town) map.set(show.k, label);
      }
    }
    return map;
  }, [phase]);

  // Back goes to the board in the town it was showing: a remembered town is better than the first published one.
  const backHref = useMemo(() => {
    try {
      const town = sessionStorage.getItem("lineup.town");
      return town ? `/?town=${encodeURIComponent(town)}` : "/";
    } catch {
      return "/";
    }
  }, []);

  const body = (() => {
    if (phase.kind === "loading") {
      return (
        <div className="skeleton" aria-busy="true" aria-live="polite">
          <span className="sr-only">Reading the index…</span>
          {Array.from({ length: 5 }, (_, index) => (
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
          <p>{phase.message}</p>
        </div>
      );
    }
    if (phase.shows.length === 0) {
      return (
        <div className="state">
          <h2>No dates in this window.</h2>
          <p>
            Nothing by <b>{prettyKey(artistKey)}</b> is published for the next{" "}
            <span className="num">{phase.manifest.window.days}</span> days — either they are not touring here, or the
            miner has not seen them yet.
          </p>
        </div>
      );
    }
    return <Board rows={phase.shows} townOf={townOf} linkArtist={false} />;
  })();

  const townCount = new Set(phase.kind === "ready" ? phase.shows.map((s) => s.town) : []).size;

  return (
    <>
      <div className="stage" aria-hidden="true">
        <div className="stage__blobs" />
        <div className="stage__lift" />
        <div className="stage__grain" />
      </div>
      <TopBar theme={theme} onToggle={toggle} />

      <main className="page">
        <section className="sheet">
          <div className="head">
            <a
              className="backlink"
              href={backHref}
              onClick={(event) => {
                // A same-origin referrer means the board is one back-step away; keep its state instead of reloading.
                if (document.referrer.startsWith(location.origin) && history.length > 1) {
                  event.preventDefault();
                  history.back();
                }
              }}
            >
              <ArrowLeft /> All shows
            </a>
            <div className="eyebrow">
              <span className="lab">
                Artist
                {phase.kind === "ready" && phase.shows.length > 0 ? (
                  <>
                    {" · "}
                    <span className="num">{phase.shows.length}</span> {phase.shows.length === 1 ? "date" : "dates"}
                  </>
                ) : null}
              </span>
              {phase.kind === "ready" && townCount > 1 ? (
                <span className="r">
                  <span className="lab">
                    <span className="num">{townCount}</span> towns
                  </span>
                </span>
              ) : null}
            </div>
            <div className="h1row">
              <h1>
                <span className="sign" data-t={name}>
                  {name}
                </span>
              </h1>
            </div>
          </div>

          <div id="board">{body}</div>

          {phase.kind === "ready" ? (
            <div className="foot">
              <span className="l">
                As of <span className="num">{stampLabel(phase.manifest.built_at)}</span>
              </span>
            </div>
          ) : null}
        </section>
      </main>
    </>
  );
}

/** A key that matched nothing still has to read as a name; a hashed key (`x-ebd798351b`) is left as the key is. */
function prettyKey(key: string): string {
  if (/^x-[0-9a-f]{10}$/.test(key)) return key;
  return key.replace(/-/g, " ").replace(/\b\p{Ll}/gu, (c) => c.toUpperCase());
}
