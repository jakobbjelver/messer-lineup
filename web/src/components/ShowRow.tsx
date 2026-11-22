import type { MouseEvent } from "react";
import { ArrowUpRight } from "@phosphor-icons/react";
import Avatar from "./Avatar";
import { Key } from "../lib/key";
import { dayNumber, dayOf, monthShort, statusLabel, weekdayOf, type Row } from "../lib/indexData";
import { formatKm } from "../lib/geo";
import type { PortraitMap } from "../lib/portraits";

type Props = {
  row: Row;
  portraits?: PortraitMap;
  /** Mark the row as one of the reader's, while the board is showing everyone. */
  mine?: boolean;
  /** Whether the artist name links to `/a/<key>`. Off on that page itself. */
  linkArtist?: boolean;
  /** How far the show is from the reader, when both ends are known. */
  distance?: number | null;
  /** The artist's genres, from the published map. A tag on the meta line, never a filter. */
  genres?: string[];
  /** The town, for a page that spans towns (the artist route). */
  town?: string;
};

/**
 * One show: a face, an artist, a venue, an arrow.
 *
 * The row is one action — the ticket page — and the two links inside it are the two exceptions: the
 * artist name goes to that artist's dates, the arrow repeats the ticket link for a keyboard. There is
 * no third control: a row that offers three things chooses none of them.
 */
export default function ShowRow({ row, portraits, mine, linkArtist = true, distance, genres, town }: Props) {
  const day = dayOf(row.d);
  const status = statusLabel(row.st);
  const place = town ?? "";
  const label = `${row.a} at ${row.v}, ${weekdayOf(day)} ${dayNumber(day)} ${monthShort(day)}${status ? ` (${status})` : ""}`;

  const openRow = (event: MouseEvent<HTMLDivElement>) => {
    if ((event.target as HTMLElement).closest("a,button")) return;
    window.open(row.u, "_blank", "noopener,noreferrer");
  };

  return (
    <div className="show" onClick={openRow}>
      <Avatar name={row.a} portraits={portraits} mine={mine} />
      <span className="show__who">
        {linkArtist ? (
          <a className="show__artist" href={`/a/${Key(row.a)}`}>
            {row.a}
          </a>
        ) : (
          <span className="show__artist">{row.a}</span>
        )}
        <span className="show__meta">
          {/* The genre answers "what kind of act" before the line answers "where" — two at most, because a row's meta
              line is a caption and three tags push the venue into an ellipsis. */}
          {genres && genres.length ? (
            <>
              <span className="genre">{genres.slice(0, 2).join(" · ")}</span>
              <span className="dot" aria-hidden="true">
                ·
              </span>
            </>
          ) : null}
          <span className="venue">{row.v}</span>
          {/* A row on a board that spans towns says which town it is in, before it says how far — the place is the
              identity and the distance is the detail. */}
          {place ? (
            <>
              <span className="dot" aria-hidden="true">
                ·
              </span>
              <span className="venue town">{place}</span>
            </>
          ) : null}
          {typeof distance === "number" ? (
            <>
              <span className="dot" aria-hidden="true">
                ·
              </span>
              <span className="km">
                <b>{formatKm(distance)}</b> km
              </span>
            </>
          ) : null}
          {status ? (
            <>
              <span className="dot" aria-hidden="true">
                ·
              </span>
              <span className="status">{status}</span>
            </>
          ) : null}
        </span>
      </span>
      <a className="show__go" href={row.u} target="_blank" rel="noopener noreferrer" aria-label={`Open ${label}`}>
        <ArrowUpRight size={16} />
      </a>
    </div>
  );
}
