import { useEffect, useMemo, useRef, useState } from "react";
import { ArrowLeft } from "./icons";
import { parseTasteList, type TasteEntry } from "../lib/taste";
import { townLabel, type Town, type TownEntry } from "../lib/indexData";
import type { Window as DateWindow } from "../lib/state";

export type PaletteTab = "town" | "taste";

type Props = {
  open: boolean;
  tab: PaletteTab;
  onTab: (tab: PaletteTab) => void;
  onClose: () => void;
  towns: TownEntry[];
  townNames: Town[];
  currentTown: string;
  onTown: (id: string) => void;
  taste: TasteEntry[];
  onTaste: (entries: TasteEntry[]) => void;
  /** The window on screen and the one the index publishes, so the travel flow can clamp to what exists. */
  dateWindow: DateWindow | null;
  published: DateWindow | null;
  onWindow: (next: DateWindow) => void;
  /** How many artists on the list have a show on the current board — the curation order. */
  onBoard: (key: string) => boolean;
};

/**
 * One surface, two tabs, flows in place — ⌘K, or either of the two triggers.
 *
 * The taste rows that need a backend (a music account, a Last.fm handle) are **absent**, not disabled: the
 * design's rule is that a control which cannot do its job is a lie about what tapping it will do. They arrive
 * with the taste backends (P5), and they slot in above the two rows below without this panel changing shape.
 */
export default function Palette(props: Props) {
  const {
    open,
    tab,
    onTab,
    onClose,
    towns,
    townNames,
    currentTown,
    onTown,
    taste,
    onTaste,
    onBoard,
    dateWindow,
    published,
    onWindow,
  } = props;
  const [flow, setFlow] = useState<null | "manual" | "travel">(null);
  const [fromDate, setFromDate] = useState("");
  const [toDate, setToDate] = useState("");
  const [paste, setPaste] = useState("");
  const [query, setQuery] = useState("");
  const [dropped, setDropped] = useState<Set<string>>(new Set());
  const panel = useRef<HTMLDivElement | null>(null);

  // A closed palette is not in the document at all, so no focus can land in it and no key handler is live.
  useEffect(() => {
    if (!open) {
      setFlow(null);
      setPaste("");
      setQuery("");
      setDropped(new Set());
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        if (flow) setFlow(null);
        else onClose();
        return;
      }
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const options = [...(panel.current?.querySelectorAll<HTMLElement>(".opt, .palette__tabs button") ?? [])];
      if (!options.length) return;
      const at = options.indexOf(document.activeElement as HTMLElement);
      const next = event.key === "ArrowDown" ? (at + 1) % options.length : (at - 1 + options.length) % options.length;
      event.preventDefault();
      options[next]?.focus();
    };
    window.addEventListener("keydown", onKey);
    panel.current?.querySelector<HTMLElement>(".opt, .palette__tabs button")?.focus();
    return () => window.removeEventListener("keydown", onKey);
  }, [open, flow, onClose]);

  const parsed = useMemo(() => parseTasteList(paste), [paste]);
  // The travel flow's own validation, so the button can be absent rather than fail on click.
  const travelProblem = (() => {
    if (!fromDate || !toDate) return "Pick both days.";
    if (fromDate > toDate) return "The first day is after the last one.";
    if (published && (fromDate < published.from || toDate > published.to)) {
      return `The index covers ${published.from} to ${published.to}.`;
    }
    return null;
  })();
  const visibleTowns = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return towns;
    return towns.filter((entry) => townLabel(entry, townNames).toLowerCase().includes(q) || entry.town.includes(q));
  }, [towns, townNames, query]);

  if (!open) return null;

  const applyTaste = (entries: TasteEntry[]) => {
    onTaste(entries);
    setFlow(null);
    setPaste("");
    setDropped(new Set());
  };

  const curation = parsed.filter((entry) => !dropped.has(entry.key));
  const linkedState = taste.length > 0;

  return (
    <>
      <div className="scrim" data-open="" onClick={onClose} />
      <div className="palette" data-open="" role="dialog" aria-modal="true" aria-label="Town and taste" ref={panel}>
        <div className="palette__tabs" role="tablist">
          <button role="tab" aria-selected={tab === "town"} onClick={() => { onTab("town"); setFlow(null); }}>
            Town
          </button>
          <button role="tab" aria-selected={tab === "taste"} onClick={() => { onTab("taste"); setFlow(null); }}>
            Your taste
          </button>
        </div>

        {tab === "town" && !flow ? (
          <div className="palette__search">
            <input
              type="text"
              placeholder="Search a town…"
              aria-label="Search a town"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
          </div>
        ) : null}

        <div className="palette__body">
          {flow === "travel" ? (
            <>
              <button className="backlink" onClick={() => setFlow(null)}>
                <ArrowLeft /> All options
              </button>
              <div className="flow" style={{ paddingTop: 0 }}>
                <h3>Travel dates</h3>
                <div className="field" style={{ marginTop: 10 }}>
                  <input
                    type="date"
                    aria-label="First day"
                    value={fromDate}
                    min={published?.from}
                    max={published?.to}
                    onChange={(event) => setFromDate(event.target.value)}
                  />
                  <span className="lab" style={{ alignSelf: "center" }}>
                    to
                  </span>
                  <input
                    type="date"
                    aria-label="Last day"
                    value={toDate}
                    min={published?.from}
                    max={published?.to}
                    onChange={(event) => setToDate(event.target.value)}
                  />
                </div>
                {travelProblem ? (
                  <p className="hint" style={{ marginTop: 8 }}>{travelProblem}</p>
                ) : (
                  <div className="field" style={{ marginTop: 12 }}>
                    <button
                      className="btn primary"
                      onClick={() => {
                        onWindow({ from: fromDate, to: toDate });
                        onClose();
                      }}
                    >
                      Show these dates
                    </button>
                    {published ? (
                      <button className="btn" onClick={() => { onWindow(published); onClose(); }}>
                        Whole season
                      </button>
                    ) : null}
                    <span className="lab" style={{ marginLeft: "auto", alignSelf: "center" }}>
                      <span className="num">{daysBetween(fromDate, toDate) + 1}</span> days
                    </span>
                  </div>
                )}
              </div>
            </>
          ) : flow === "manual" ? (
            <>
              <button className="backlink" onClick={() => setFlow(null)}>
                <ArrowLeft /> All options
              </button>
              <div className="flow" style={{ paddingTop: 0 }}>
                <h3>Type or paste artists</h3>
                <p>Tip: the list lives in the link — bookmark the board and it keeps your artists.</p>
                <textarea
                  className="paste"
                  value={paste}
                  onChange={(event) => setPaste(event.target.value)}
                  placeholder={"Björk\nFour Tet\nPortishead"}
                  aria-label="Artists, one per line"
                  rows={6}
                  autoFocus
                />
                {curation.length ? (
                  <>
                    <div className="chips">
                      {curation.map((entry) => (
                        <button
                          key={entry.key}
                          className={onBoard(entry.key) ? "chip on" : "chip"}
                          title={onBoard(entry.key) ? "Has a show on this board" : "No show in this window"}
                          onClick={() => setDropped(new Set([...dropped, entry.key]))}
                        >
                          {entry.name} <b>×</b>
                        </button>
                      ))}
                    </div>
                    <div className="field" style={{ marginTop: 14 }}>
                      <button
                        className="btn primary"
                        onClick={() => {
                          applyTaste(curation);
                          // The board is the answer, so the panel gets out of the way the moment it is applied.
                          onClose();
                        }}
                      >
                        Keep {curation.length} {curation.length === 1 ? "artist" : "artists"}
                      </button>
                      <button className="btn" onClick={() => setPaste("")}>
                        Clear
                      </button>
                      <span className="lab" style={{ marginLeft: "auto", alignSelf: "center" }}>
                        {curation.filter((entry) => onBoard(entry.key)).length} on this board
                      </span>
                    </div>
                  </>
                ) : null}
              </div>
            </>
          ) : tab === "taste" ? (
            <>
              {linkedState ? (
                <div className="flow" style={{ paddingBottom: 0 }}>
                  <p>
                    Applied to this board: <b>{taste.length}</b> artists ·{" "}
                    <b>{taste.filter((entry) => onBoard(entry.key)).length}</b> with a show.{" "}
                    <button className="linkish" onClick={() => applyTaste([])}>
                      Clear taste
                    </button>
                  </p>
                  <div className="chips">
                    {taste.slice(0, 12).map((entry) => (
                      <span key={entry.key} className={onBoard(entry.key) ? "chip on" : "chip"}>
                        {entry.name}
                      </span>
                    ))}
                    {taste.length > 12 ? <span className="chip">+{taste.length - 12}</span> : null}
                  </div>
                </div>
              ) : null}
              <div className="flow" style={{ padding: "10px 16px 2px" }}>
                <span className="lab">Ranked · best first</span>
              </div>
              <button className="opt" onClick={() => { setFlow("manual"); setPaste(taste.map((e) => e.name).join("\n")); }}>
                <span className="opt__ic">≡</span>
                <span className="opt__t">
                  <b>Type or paste artists</b>
                  <span>No account. Full control over the list</span>
                </span>
                <span className="opt__tag plain">No account</span>
              </button>
            </>
          ) : (
            <>
              <button
                className="opt"
                onClick={() => {
                  setFlow("travel");
                  setFromDate(dateWindow?.from ?? published?.from ?? "");
                  setToDate(dateWindow?.to ?? published?.to ?? "");
                }}
              >
                <span className="opt__ic">⌖</span>
                <span className="opt__t">
                  <b>Travel dates</b>
                  <span>
                    {dateWindow
                      ? `${dateWindow.from} to ${dateWindow.to}`
                      : "The whole season — the miner's window"}
                  </span>
                </span>
                <span className="opt__tag plain">dates</span>
              </button>
              <div className="flow" style={{ padding: "10px 16px 2px" }}>
                <span className="lab">Or pick a town</span>
              </div>
              {visibleTowns.map((entry) => {
                const selected = entry.town === currentTown;
                return (
                  <button key={entry.town} className="opt" onClick={() => { onTown(entry.town); onClose(); }}>
                    <span className="opt__ic">◎</span>
                    <span className="opt__t">
                      <b>{townLabel(entry, townNames)}</b>
                      <span>
                        <span className="num">{entry.count}</span> shows · {entry.sources.join(", ")}
                      </span>
                    </span>
                    {selected ? <span className="opt__check">✓</span> : <span className="opt__tag plain">{entry.town.split("-")[1] ?? ""}</span>}
                  </button>
                );
              })}
              {visibleTowns.length === 0 ? (
                <div className="flow">
                  <p className="hint">No published town matches that. The list is what the miner found, not a gazetteer.</p>
                </div>
              ) : null}
            </>
          )}
        </div>

        <div className="palette__foot">
          <span className="esc">esc</span>
        </div>
      </div>
    </>
  );
}

/** Whole days between two `YYYY-MM-DD` days; 0 when either is unreadable. */
function daysBetween(from: string, to: string): number {
  const a = new Date(`${from}T12:00:00Z`).getTime();
  const b = new Date(`${to}T12:00:00Z`).getTime();
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return 0;
  return Math.round((b - a) / 86_400_000);
}
