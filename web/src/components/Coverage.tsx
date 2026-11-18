import { useEffect, useRef, useState } from "react";
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
  // Built once per render pass: the token is a pure function of the three things that define the board.
  const links = calendarLink(town, dateWindow, artistNames);

  // The feed's address is long by nature (it *is* the board). Printing it is what made this panel noisy, so it is
  // copied rather than shown — and the button says so for a moment, which is the only feedback a copy control has.
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(links.https);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard refused: the webcal link is still there to click */
    }
  };
  const wrap = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    const onDown = (event: PointerEvent) => {
      if (!wrap.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("pointerdown", onDown);
    return () => {
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("pointerdown", onDown);
    };
  }, [open]);

  return (
    <div className="coverage" ref={wrap}>
      <button
        className="coverage__line"
        aria-expanded={open}
        aria-controls="coverage-panel"
        onClick={() => setOpen((value) => !value)}
      >
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

      {open ? (
        <div className="coverage__panel" id="coverage-panel" role="dialog" aria-label="Where this board comes from">
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
            {canForget
              ? "Clears your taste list, its spellings and the “new” markers on this device. The index itself is public and unchanged."
              : "There is nothing on this device to forget yet."}
          </p>
        </div>
      ) : null}
    </div>
  );
}
