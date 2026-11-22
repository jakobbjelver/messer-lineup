import { useState } from "react";
import { feedUrl, webcalUrl, type CalendarSource } from "../lib/calendar";

type Props = {
  /** The town's name, or `Anywhere`. */
  label: string;
  /** How many artists the feed carries. Zero means the whole scope — unless genres are set. */
  artists: number;
  /** How many genres the feed carries. */
  genres: number;
  /** Shows in the feed today. Zero is a watch, not an empty promise. */
  matched: number;
  /** Shows in the scope, for the whole-town line. */
  total: number;
  source: CalendarSource | null;
  token: string;
  origin: string;
  /** This device already subscribed to a different board. */
  issued: boolean;
  remind: boolean;
  onRemind: (next: boolean) => void;
  /** Remember the token this link hands over. */
  onIssued: () => void;
};

/**
 * What a subscription is, in one place.
 *
 * The button says four words and nothing else, so everything that used to sit around it — the count, the note, the
 * copied state, the live chip, the reminder — lives here, where it can be read deliberately instead of competing with
 * the board. The scope line is the honest part: a feed with no artist list is the whole town, and it says so before
 * the reader's calendar fills with it.
 */
export default function SubscribePanel({
  label,
  artists,
  genres,
  matched,
  total,
  source,
  token,
  origin,
  issued,
  remind,
  onRemind,
  onIssued,
}: Props) {
  const [copied, setCopied] = useState(false);

  const parts = [label];
  if (artists > 0) parts.push(`${artists} ${artists === 1 ? "artist" : "artists"}`);
  if (genres > 0) parts.push(`${genres} ${genres === 1 ? "genre" : "genres"}`);
  const scope =
    artists > 0 || genres > 0
      ? `${parts.join(" · ")} · ${matched} ${matched === 1 ? "show" : "shows"}`
      : `Everything in ${label} · ${total} ${total === 1 ? "show" : "shows"}`;

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(feedUrl(origin, token));
      onIssued();
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      /* a clipboard that refuses is a browser fact; the calendar link is still there */
    }
  };

  return (
    <div className="subpop__in">
      <p className="subpop__title">Subscribe</p>
      <p className="subpop__lead">
        A calendar you subscribe to once. It fills itself when one of your artists announces a date.
      </p>

      <dl className="subpop__facts">
        <div>
          <dt>Scope</dt>
          <dd>{scope}</dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>Nightly, 04:10</dd>
        </div>
        {source ? (
          <div>
            <dt>Follows</dt>
            <dd>
              {source.u} on ListenBrainz — re-read every six hours, with the artists in this link as the fallback.
            </dd>
          </div>
        ) : null}
      </dl>

      <div className="subpop__acts">
        <a className="subpop__go" href={webcalUrl(origin, token)} onClick={onIssued} rel="noopener">
          Open in Calendar
        </a>
        <button type="button" className="subpop__copy" onClick={() => void copy()}>
          {copied ? "Copied" : "Copy link"}
        </button>
      </div>

      <label className="subpop__check">
        <input type="checkbox" checked={remind} onChange={(event) => onRemind(event.target.checked)} />
        Remind me the day before
      </label>

      {issued ? (
        <p className="subpop__note">
          This device already follows an earlier board. Opening the link replaces that calendar rather than adding a
          second one.
        </p>
      ) : null}
    </div>
  );
}
