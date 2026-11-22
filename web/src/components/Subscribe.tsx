import { useMemo, useState } from "react";
import * as Popover from "@radix-ui/react-popover";
import { CalendarPlus, Info } from "@phosphor-icons/react";
import SubscribePanel from "./SubscribePanel";
import { tokenFor, webcalUrl, type CalendarSource } from "../lib/calendar";
import { readFeed, readReminder, writeFeed, writeReminder } from "../lib/storage";
import type { TasteEntry } from "../lib/taste";

type Props = {
  /** A town id, or `anywhere`. */
  scope: string;
  /** What the scope is called on the page, for the panel's scope line. */
  label: string;
  /** The reader's list. Empty means the whole scope — or the genres, when there are any. */
  artists: TasteEntry[];
  /** Genre ids, unioned with the artist list on the board and in the feed. */
  genres?: string[];
  /** Shows the feed holds today. Zero is a watch, not a dead control. */
  matched: number;
  /** Shows in the scope, so the whole-town line can count them. */
  total?: number;
  /** A live list to follow, with `artists` as the snapshot it falls back to. */
  source?: CalendarSource | null;
};

/**
 * One control: subscribe, and the explanation on demand.
 *
 * A subscription is a URL a calendar client polls for as long as it lives, and the *shows* in it are live — the feed
 * renders from the index on every fetch. So an empty feed is not a dead end: it is a standing watch, and it fills
 * itself the next time one of these artists announces a date.
 *
 * Two forms of the address, because the platforms differ: `webcal:` opens the subscription sheet in Apple's Calendar,
 * and Google has no handler at all, so the `https:` form is what the panel copies. Neither is ever printed — a token
 * is 300 to 2 700 characters.
 *
 * The label is always `Subscribe`. A token that has changed is a fact about the panel, not a reason to rename the
 * control the reader already learned.
 */
export default function Subscribe({ scope, label, artists, genres, matched, total, source }: Props) {
  const [open, setOpen] = useState(false);
  const [remind, setRemind] = useState(readReminder);
  const origin = typeof location === "undefined" ? "" : location.origin;

  const token = useMemo(() => {
    try {
      return tokenFor({ scope, artists, genres, source: source ?? undefined, reminder: remind });
    } catch {
      // Over the cap: the picker prevents it, and a control that cannot produce a valid link must not be drawn.
      return null;
    }
  }, [scope, artists, genres, source, remind]);

  if (!token) return null;

  const issued = readFeed();
  const remember = () => writeFeed(token);

  return (
    <div className="subbtn">
      <a className="subbtn__go" href={webcalUrl(origin, token)} onClick={remember} rel="noopener">
        <CalendarPlus size={15} aria-hidden="true" />
        Subscribe
      </a>
      <Popover.Root open={open} onOpenChange={setOpen}>
        <Popover.Trigger asChild>
          <button
            type="button"
            className="subbtn__info"
            aria-label="What subscribing means"
            aria-expanded={open}
          >
            <Info size={13} weight="bold" aria-hidden="true" />
          </button>
        </Popover.Trigger>
        <Popover.Portal>
          <Popover.Content className="subpop" side="bottom" align="end" sideOffset={8} collisionPadding={12}>
            <SubscribePanel
              label={label}
              artists={artists.length}
              genres={genres?.length ?? 0}
              matched={matched}
              total={total ?? matched}
              source={source ?? null}
              token={token}
              origin={origin}
              issued={issued !== null && issued !== token}
              remind={remind}
              onRemind={(next) => {
                setRemind(next);
                writeReminder(next);
              }}
              onIssued={remember}
            />
          </Popover.Content>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}
