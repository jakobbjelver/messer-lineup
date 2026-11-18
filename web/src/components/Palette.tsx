import { useEffect, useMemo, useState } from "react";
import DateRange from "./DateRange";
import ResponsiveDialog from "./ui/ResponsiveDialog";
import * as TabKit from "./ui/Tabs";
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
  const [paste, setPaste] = useState("");
  const [query, setQuery] = useState("");
  const [dropped, setDropped] = useState<Set<string>>(new Set());

  // The overlay (Radix) owns Escape, focus and the scroll lock; this effect owns only the flow's own reset and the
  // arrow keys within the option list, which is not a tablist and therefore not Radix's to move.
  useEffect(() => {
    if (!open) {
      setFlow(null);
      setPaste("");
      setQuery("");
      setDropped(new Set());
      return;
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const options = [...document.querySelectorAll<HTMLElement>(".ui-dialog .opt")];
      if (!options.length) return;
      const at = options.indexOf(document.activeElement as HTMLElement);
      const next = event.key === "ArrowDown" ? (at + 1) % options.length : (at - 1 + options.length) % options.length;
      event.preventDefault();
      options[next]?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  // A list of options is not the tablist: the first option takes focus when the sheet opens on its own content.
  useEffect(() => {
    if (!open || flow) return;
    const timer = window.setTimeout(() => {
      document.querySelector<HTMLElement>(".ui-dialog .opt")?.focus({ preventScroll: true });
    }, 30);
    return () => window.clearTimeout(timer);
  }, [open, flow, tab]);

  const parsed = useMemo(() => parseTasteList(paste), [paste]);
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
    <ResponsiveDialog
      open={open}
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
      label="Town and taste"
      // A sheet's Escape goes back one step before it closes the sheet: the flow is a place, and back is where it goes.
      onEscape={() => {
        if (flow) {
          setFlow(null);
          return true;
        }
        return false;
      }}
    >
      <TabKit.Root
        value={tab}
        onValueChange={(next) => {
          onTab(next as PaletteTab);
          setFlow(null);
        }}
        className="palette"
      >
        <TabKit.List label="Town and taste">
          <TabKit.Tab value="town">Town</TabKit.Tab>
          <TabKit.Tab value="taste">Your taste</TabKit.Tab>
        </TabKit.List>

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
                {published ? (
                  <DateRange
                    from={dateWindow?.from ?? published.from}
                    to={dateWindow?.to ?? published.to}
                    bounds={published}
                    onChange={(next, last) => onWindow({ from: next, to: last })}
                  />
                ) : null}
                <div className="field" style={{ marginTop: 12 }}>
                  {/* the picker already prints the range once; a second copy would read as two facts */}
                  <button className="btn primary" onClick={onClose}>
                    Done
                  </button>
                </div>
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
                onClick={() => setFlow("travel")}
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
      </TabKit.Root>
    </ResponsiveDialog>
  );
}

