import { useEffect, useState } from "react";
import { ArrowLeft } from "@phosphor-icons/react";
import ArtistPicker from "./ArtistPicker";
import Mark from "./Mark";
import TownPicker from "./TownPicker";
import type { Town, TownEntry } from "../lib/indexData";
import type { Position } from "../lib/location";
import type { TasteEntry } from "../lib/taste";
import type { PortraitMap } from "../lib/portraits";

type Props = {
  posts: TownEntry[];
  names: Town[];
  taste: TasteEntry[];
  onTaste: (entries: TasteEntry[]) => void;
  portraits?: PortraitMap;
  town: string;
  onTown: (id: string) => void;
  position: Position;
  canLocate: boolean;
  onLocate: () => void;
  located: boolean;
  onRelease: () => void;
  /** Commit what is on screen and show the board. */
  onDone: () => void;
  /** Leave the gate as a guest: the auto town, no artists. */
  onSkip: () => void;
};

/**
 * The first run: two questions, one sheet.
 *
 * The steps are `ArtistPicker` and `TownPicker` with a shell around them — the same two components
 * the settings sheet mounts in isolation. The progress rule is the only chrome: no dots, no step
 * names, no "welcome to Lineup" paragraph. The gate is escapable by design; a wall that traps a
 * shared link is a bug.
 */
export default function Wizard({
  posts,
  names,
  taste,
  onTaste,
  portraits,
  town,
  onTown,
  position,
  canLocate,
  onLocate,
  located,
  onRelease,
  onDone,
  onSkip,
}: Props) {
  const [step, setStep] = useState<1 | 2>(1);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        if (step === 2) setStep(1);
        else onSkip();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [step, onSkip]);

  return (
    <div className="wiz" role="dialog" aria-modal="true" aria-labelledby="wiz-h">
      <div className="wiz__bar" data-step={step}>
        <i />
      </div>

      <div className="wiz__body" key={step}>
        <Mark size={24} />
        <h1 className="wiz__h1" id="wiz-h">
          {step === 1 ? "Who do you go see?" : "Where?"}
        </h1>

        {step === 1 ? (
          <ArtistPicker value={taste} onChange={onTaste} portraits={portraits} />
        ) : (
          <TownPicker
            posts={posts}
            names={names}
            value={town}
            onChange={onTown}
            position={position}
            canLocate={canLocate}
            onLocate={onLocate}
            located={located}
            onRelease={onRelease}
          />
        )}
      </div>

      <div className="wiz__foot">
        {step === 2 ? (
          <button className="wiz__back" type="button" aria-label="Back to artists" onClick={() => setStep(1)}>
            <ArrowLeft size={16} />
          </button>
        ) : (
          <span className="wiz__step">1 / 2</span>
        )}
        <button
          className="btn btn--primary"
          type="button"
          onClick={() => {
            if (step === 1) setStep(2);
            else onDone();
          }}
        >
          {step === 1 ? (taste.length > 0 ? "Continue" : "Skip") : "Show me"}
        </button>
      </div>
    </div>
  );
}
