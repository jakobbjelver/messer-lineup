import { useEffect, useMemo, useState } from "react";
import { ArrowLeft } from "@phosphor-icons/react";
import Avatar from "./Avatar";
import Board from "./Board";
import Chrome from "./Chrome";
import { Empty, Failed, Skeleton } from "./states";
import { groupByDay, localDay } from "../lib/board";
import { loadArtistRows, loadIndex, stampLabel, townLabel, type ArtistShow, type Manifest, type Town } from "../lib/indexData";
import { loadPortraits, portraitFor, type PortraitMap } from "../lib/portraits";
import { labelFor } from "../lib/taste";

type Phase =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "ready"; shows: ArtistShow[]; manifest: Manifest; towns: Town[] };

/**
 * `/a/<key>` — one artist, every date in the reach.
 *
 * The board answers "what is on near me"; this page answers "when can I see this act", which is the
 * other question the index can answer with no new data. It reads every town's shard because an artist
 * playing two towns is exactly the case a single-town board hides, and each row says which town it is
 * in instead of printing a distance that would be measured from a town's centre.
 */
export default function ArtistRoute({ artistKey }: { artistKey: string }) {
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });
  const [portraits, setPortraits] = useState<PortraitMap | undefined>(undefined);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let live = true;
    loadIndex()
      .then(async ({ manifest, towns }) => {
        const shows = await loadArtistRows(manifest, artistKey);
        if (live) setPhase({ kind: "ready", shows, manifest, towns });
      })
      .catch((err: unknown) => {
        if (live) setPhase({ kind: "error", message: err instanceof Error ? err.message : String(err) });
      });
    loadPortraits().then((map) => {
      if (live) setPortraits(map);
    });
    return () => {
      live = false;
    };
  }, [artistKey, nonce]);

  const ready = phase.kind === "ready" ? phase : null;
  const name = ready?.shows[0]?.a ?? labelFor(artistKey);
  const days = useMemo(() => groupByDay(ready?.shows ?? []), [ready]);
  const townCount = useMemo(() => new Set((ready?.shows ?? []).map((show) => show.town)).size, [ready]);
  const portrait = portraits ? portraitFor(portraits, name) : undefined;
  const today = localDay();

  return (
    <>
      <div className="stage" aria-hidden="true" />
      <Chrome />
      <main className="page" data-view="list">
        <a className="backlink" href="/">
          <ArrowLeft size={14} />
          All shows
        </a>

        {phase.kind === "ready" ? (
          <div className="artist">
            <Avatar name={name} portraits={portraits} size={96} />
            <div>
              <h1 className="artist__h1">{name}</h1>
              <p className="count">
                <span>
                  <span className="num">{phase.shows.length}</span> {phase.shows.length === 1 ? "date" : "dates"}
                </span>
                {townCount > 1 ? (
                  <>
                    <span className="dot" aria-hidden="true">
                      ·
                    </span>
                    <span>
                      <span className="num">{townCount}</span> towns
                    </span>
                  </>
                ) : null}
              </p>
              {portrait?.c ? (
                <p className="artist__credit">
                  Portrait from{" "}
                  <a href={portrait.c} target="_blank" rel="noopener noreferrer">
                    Wikimedia Commons
                  </a>
                </p>
              ) : null}
            </div>
          </div>
        ) : null}

        {phase.kind === "loading" ? <Skeleton /> : null}
        {phase.kind === "error" ? <Failed message={phase.message} onRetry={() => setNonce((n) => n + 1)} /> : null}
        {phase.kind === "ready" && phase.shows.length === 0 ? (
          <Empty
            title="No dates for this artist."
            detail="Nothing in the published window, in any town."
            action={{ label: "Back to the board", onClick: () => (location.href = "/") }}
          />
        ) : null}
        {phase.kind === "ready" && phase.shows.length > 0 ? (
          <Board
            days={days}
            portraits={portraits}
            linkArtist={false}
            today={today}
            townOf={(row) => townLabel({ town: (row as ArtistShow).town }, phase.towns)}
          />
        ) : null}

        <div className="foot">
          <span>
            Lineup{" "}
            <span className="num">
              {ready ? stampLabel(ready.manifest.built_at) : "—"}
            </span>
          </span>
          {portraits && portraits.size > 0 ? (
            <a href="https://commons.wikimedia.org" target="_blank" rel="noopener noreferrer">
              Portraits · Wikimedia Commons
            </a>
          ) : null}
        </div>
      </main>
    </>
  );
}
