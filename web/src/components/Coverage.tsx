import { useState } from "react";
import * as Popover from "./ui/Popover";
import { Info } from "./icons";
import { calendarLink } from "../lib/calendar";
import { dayOf, stampLabel, type Manifest } from "../lib/indexData";

/**
 * The coverage line, and the panel behind its `i`.
 *
 * The line answers "how much of this is real" at a glance: how many shows, from how many sources. The panel answers
 * the rest — which sources, whether each one answered, what each one contributed, when the index was built, and what
 * the window actually covers. It is a click, not a `title` attribute: a native tooltip never opens from the keyboard
 * or on touch, and the numbers here are the ones a reader is meant to distrust the board by.
 *
 * `Forget everything` lives here because this is the only place that talks about where the data came from; it clears
 * taste, the spelling memory and the "new since last visit" set, then reloads to a clean board.
 */
export default function Coverage({
  manifest,
  shows,
  sourceCount,
  onForget,
  canForget,
  town,
  dateWindow,
  artistNames,
}: {
  manifest: Manifest;
  shows: number;
  sourceCount: number;
  onForget: () => void;
  canForget: boolean;
  /** What the feed should contain: this town, this window, this taste list. */
  town: string;
  dateWindow: { from: string; to: string };
  artistNames: string[];
}) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  // The feed for exactly what is on screen: this town, this window, this taste list.
  const links = calendarLink(town, dateWindow, artistNames);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(links.https);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      /* a browser that refuses the clipboard leaves the subscribe link, which is the action that matters */
    }
  };

  return (
    <div className="coverage">
      <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger asChild>
      <button className="coverage__line" aria-label="Where this board comes from">
        <span>
          <span className="num">{shows}</span> {shows === 1 ? "show" : "shows"}
        </span>
        <span className="g">·</span>
        <span className="g">
          <span className="num">{sourceCount}</span> {sourceCount === 1 ? "source" : "sources"}
        </span>
        <span className="i">
          <Info />
        </span>
      </button>
      </Popover.Trigger>

      <Popover.Content className="coverage__panel" side="bottom" align="end">
          <div className="coverage__row coverage__row--head">
            <span>Source</span>
            <span>State</span>
            <span>In index</span>
          </div>
          {manifest.sources.length === 0 ? (
            <div className="coverage__row">
              <span>—</span>
              <span className="g">no sources reported</span>
              <span className="num">—</span>
            </div>
          ) : (
            manifest.sources.map((source) => (
              <div className="coverage__row" key={source.id}>
                <span>{source.id}</span>
                <span className={source.ok ? "ok" : "bad"}>
                  {source.ok ? "answered" : "failed"}
                  {source.ok && typeof source.parse_yield === "number"
                    ? ` · ${Math.round(source.parse_yield * 100)}% parsed`
                    : ""}
                </span>
                <span className="num">{source.ok ? (source.rows ?? "—") : "—"}</span>
              </div>
            ))
          )}

          <dl className="coverage__facts">
            <div>
              <dt>Window</dt>
              <dd>
                {dayOf(manifest.window.from)} → {dayOf(manifest.window.to)} ·{" "}
                <span className="num">{manifest.window.days}</span> days
              </dd>
            </div>
            <div>
              <dt>Built</dt>
              <dd>{stampLabel(manifest.built_at)}</dd>
            </div>
            <div>
              <dt>Towns</dt>
              <dd className="num">{manifest.towns.length}</dd>
            </div>
          </dl>

          <div className="coverage__cal">
            <a className="coverage__cal-link" href={links.webcal}>
              Subscribe in your calendar
            </a>
            <button type="button" className="coverage__copy" onClick={copy}>
              {copied ? "Copied" : "Copy link"}
            </button>
          </div>

          <button className="coverage__forget" onClick={onForget} disabled={!canForget}>
            Forget everything
          </button>
          <p className="coverage__note">
            {canForget ? "Your taste, spellings and “new” markers. The index is public." : "Nothing to forget yet."}
          </p>
      </Popover.Content>
      </Popover.Root>
    </div>
  );
}
