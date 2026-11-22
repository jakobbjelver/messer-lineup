import { useEffect, useMemo, useState } from "react";
import { ArrowLeft } from "@phosphor-icons/react";
import Avatar from "./Avatar";
import Board from "./Board";
import Chrome from "./Chrome";
import Loader from "./Loader";
import Subscribe from "./Subscribe";
import { Empty, Failed } from "./states";
import { groupByDay, localDay } from "../lib/board";
import { EMPTY_GENRE_MAP, genreLabel, loadGenres, type GenreMap } from "../lib/genres";
import { loadArtistRows, loadIndex, stampLabel, townLabel, type ArtistShow, type Manifest, type Town } from "../lib/indexData";
import { loadPortraits, portraitFor, sourceName, sourceNames, type PortraitMap } from "../lib/portraits";
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
  const [genreMap, setGenreMap] = useState<GenreMap>(EMPTY_GENRE_MAP);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    let live = true;
    loadIndex()
      .then(async ({ manifest, towns }) => {
        // The genre artifact is asked for beside the shards rather than after them: it is the same size class as the
        // portraits file, and the page's genre line is not worth a second-round-trip skeleton.
        const genreTask = loadGenres(manifest.files?.genres);
        const shows = await loadArtistRows(manifest, artistKey);
        if (!live) return;
        setPhase({ kind: "ready", shows, manifest, towns });
        const [map, genre] = await Promise.all([loadPortraits(manifest.files?.portraits), genreTask]);
        if (!live) return;
        setPortraits(map);
        setGenreMap(genre);
      })
      .catch((err: unknown) => {
        if (live) setPhase({ kind: "error", message: err instanceof Error ? err.message : String(err) });
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
  /**
   * This artist's own genres. The route is keyed by the same identity the board links with (`Key(name)`), so the map
   * answers here without the row carrying anything: an artist with no dates still has a genre, and a page that says
   * "0 dates · metal" is the more useful page.
   */
  const genres = useMemo(
    () => (genreMap.artist.get(artistKey) ?? []).map(genreLabel),
    [genreMap, artistKey],
  );
  const today = localDay();

  return (
    <>
      <div className="stage" aria-hidden="true" />
      <Chrome />
      <main className="page" data-view="list">
        <a className="backlink" href="/">
          <ArrowLeft size={16} weight="bold" aria-hidden="true" />
          All shows
        </a>

        {phase.kind === "ready" ? (
          <div className="artist">
            <Avatar name={name} portraits={portraits} size={96} />
            <div className="artist__id">
              {/* The name and the subscription share a line, exactly as the board's title row does: the control holds the
                  row's right end, and the page's own facts (count, genres, credit) read underneath it. */}
              <div className="artist__top">
                <h1 className="artist__h1">{name}</h1>
                <div className="artist__acts">
                  <Subscribe scope="anywhere" label="Anywhere" artists={[{ key: artistKey, name }]} matched={phase.shows.length} />
                </div>
              </div>
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
              {genres.length ? (
                <p className="artist__genres">
                  {genres.map((genre) => (
                    <span className="genre" key={genre}>
                      {genre}
                    </span>
                  ))}
                </p>
              ) : null}
              {portrait?.c ? (
                <p className="artist__credit">
                  Portrait from{" "}
                  <a href={portrait.c} target="_blank" rel="noopener noreferrer">
                    {sourceName(portrait.c)}
                  </a>
                </p>
              ) : null}
            </div>
          </div>
        ) : null}

        <div id="board">
          {phase.kind === "loading" ? (
            <div className="loading">
              <Loader size={30} label="Reading this artist's dates" />
            </div>
          ) : null}
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
        </div>

        <div className="foot">
          <span>
            Lineup{" "}
            <span className="num">
              {ready ? stampLabel(ready.manifest.built_at) : "—"}
            </span>
          </span>
          {portraits && portraits.size > 0 ? (
            <span className="foot__credit">Portraits · {sourceNames(portraits).join(" · ")}</span>
          ) : null}
        </div>
      </main>
    </>
  );
}
