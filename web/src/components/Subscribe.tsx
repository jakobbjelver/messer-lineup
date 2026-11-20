import { useMemo, useState } from "react";
import { CalendarPlus, Check, Copy } from "@phosphor-icons/react";
import InfoTip from "./ui/InfoTip";
import { feedUrl, tokenFor, webcalUrl, type CalendarSource } from "../lib/calendar";
import { readFeed, writeFeed } from "../lib/storage";
import type { TasteEntry } from "../lib/taste";

type Props = {
  /** A town id, or `anywhere`. */
  scope: string;
  /** The reader's list. Empty means the whole scope, which is the one case the caller must not offer. */
  artists: TasteEntry[];
  /** Rows the feed holds today. Zero is a watch, not a dead control — see the note. */
  matched: number;
  /** A live list to follow, with `artists` as the snapshot it falls back to. */
  source?: CalendarSource | null;
  /** `quiet` is the whole-scope variant: a different promise, named after what it holds. */
  variant?: "primary" | "quiet";
  /** A sentence for a state that needs one (an empty board, a replaced subscription). */
  note?: string;
};

/**
 * The subscribe control: one button, its count, and the address behind it.
 *
 * A subscription is a URL a calendar client polls for as long as it lives, and the *shows* in it are live — the feed
 * renders from the index on every fetch. So an empty feed is not a dead end: it is a standing watch, and it fills
 * itself the next time one of these artists announces a date. That is why the control is offered at zero matches and
 * why its label counts what you would get rather than what is in it.
 *
 * Two forms of the address, because the platforms differ: `webcal:` opens the subscription sheet in Apple's Calendar,
 * and Google has no handler at all, so the `https:` form is copyable. Neither is ever printed — a token is 300 to
 * 2 700 characters.
 *
 * The token issued is remembered, so a list that has changed since can offer to replace that subscription instead of
 * silently handing over a second calendar with the same events in it.
 */
export default function Subscribe({ scope, artists, matched, source, variant = "primary", note }: Props) {
  const [copied, setCopied] = useState(false);
  const origin = typeof location === "undefined" ? "" : location.origin;

  const token = useMemo(() => {
    try {
      return tokenFor({ scope, artists, source: source ?? undefined });
    } catch {
      // Over the cap: the picker prevents it, and a control that cannot produce a valid link must not be drawn.
      return null;
    }
  }, [scope, artists, source]);

  if (!token) return null;
  if (variant === "primary" && artists.length === 0) return null;

  const issued = readFeed();
  const isUpdate = issued !== null && issued !== token;
  const count =
    variant === "quiet" && artists.length === 0
      ? `${matched} ${matched === 1 ? "show" : "shows"}`
      : matched > 0
        ? `${matched} ${matched === 1 ? "show" : "shows"}`
        : `${artists.length} ${artists.length === 1 ? "artist" : "artists"}`;
  // The label says what the control does and the chip says what it holds; the style is how loud it is where it sits.
  const label = isUpdate ? "Update subscription" : artists.length === 0 ? "Subscribe to everything" : "Subscribe";
  const hint = isUpdate
    ? "Replace the old address in your calendar app."
    : note ?? (matched === 0 ? "Your calendar fills itself when one of them announces." : "Updates nightly.");

  const remember = () => writeFeed(token);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(feedUrl(origin, token));
      writeFeed(token);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      /* a clipboard that refuses is a browser fact; the webcal link is still there */
    }
  };

  return (
    <div className={variant === "quiet" ? "subs subs--quiet" : "subs"}>
      <div className="subs__row">
        <a
          className={variant === "quiet" ? "subs__link subs__link--quiet" : "btn subs__link"}
          href={webcalUrl(origin, token)}
          onClick={remember}
          rel="noopener"
        >
          <CalendarPlus size={variant === "quiet" ? 13 : 16} aria-hidden="true" />
          {label}
          <span className="subs__n">{count}</span>
        </a>
        {variant === "quiet" ? (
          <button type="button" className="subs__copytext" onClick={() => void copy()}>
            {copied ? "Copied" : "Copy link"}
          </button>
        ) : (
          <button
            type="button"
            className="iconbtn subs__copy"
            onClick={() => void copy()}
            aria-label={copied ? "Link copied" : "Copy the calendar link"}
          >
            {copied ? <Check size={15} /> : <Copy size={15} />}
          </button>
        )}
      </div>
      {/* The state belongs to the sentence about what the feed is, not to the row of actions: actions on the left,
          the one state on the right, and no third thing competing for the same line. */}
      <div className="subs__foot">
        <p className="subs__note">{copied && variant !== "quiet" ? "Link copied." : hint}</p>
        {source && !isUpdate ? (
          <span className="live" data-on="">
            <span className="live__dot" aria-hidden="true" />
            Live
            <InfoTip label="What live means here">
              Following {source.u} on ListenBrainz. The calendar re-reads your top artists every six hours; if
              ListenBrainz cannot answer, it falls back to the artists in this link.
            </InfoTip>
          </span>
        ) : null}
      </div>
    </div>
  );
}
