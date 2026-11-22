import * as Tabs from "@radix-ui/react-tabs";
import { Moon, Sun } from "@phosphor-icons/react";
import ArtistPicker from "./ArtistPicker";
import ResponsiveDialog from "./ui/ResponsiveDialog";
import TownPicker from "./TownPicker";
import type { Town, TownEntry } from "../lib/indexData";
import type { Position } from "../lib/location";
import type { Theme } from "../lib/theme";
import type { TasteEntry } from "../lib/taste";
import type { PortraitMap } from "../lib/portraits";

export type Panel = "taste" | "town";

type Props = {
  open: boolean;
  tab: Panel;
  onTab: (tab: Panel) => void;
  onClose: () => void;
  taste: TasteEntry[];
  onTaste: (entries: TasteEntry[]) => void;
  town: string;
  onTown: (id: string) => void;
  posts: TownEntry[];
  names: Town[];
  portraits?: PortraitMap;
  position: Position;
  canLocate: boolean;
  onLocate: () => void;
  located: boolean;
  onRelease: () => void;
  theme: Theme;
  onTheme: () => void;
};

/**
 * Settings: the wizard's two steps, mounted one at a time.
 *
 * No flow, no back stack, no progress rule — the steps are the same components, and the panel is a
 * place to change one thing. Every change applies immediately; `Done` closes the sheet, it does not
 * save. The theme lives here because the bar has enough controls already and a display preference is
 * a setting.
 */
export default function Preferences(props: Props) {
  const { open, tab, onTab, onClose, taste, onTaste, town, onTown, posts, names, portraits, position, canLocate, onLocate, located, onRelease, theme, onTheme } = props;

  return (
    <ResponsiveDialog
      open={open}
      onOpenChange={(next) => (next ? undefined : onClose())}
      label="Artists and town"
      className="ui-dialog--settings"
    >
      <Tabs.Root value={tab} onValueChange={(next) => onTab(next as Panel)} className="panel">
        <Tabs.List className="ui-tabs" aria-label="Artists and town">
          <Tabs.Trigger className="ui-tab" value="taste">
            Artists
          </Tabs.Trigger>
          <Tabs.Trigger className="ui-tab" value="town">
            Town
          </Tabs.Trigger>
        </Tabs.List>

        <Tabs.Content value="taste" className="panel__body">
          <ArtistPicker value={taste} onChange={onTaste} compact portraits={portraits} />
        </Tabs.Content>
        <Tabs.Content value="town" className="panel__body">
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
        </Tabs.Content>

        <div className="panel__foot">
          <button
            className="iconbtn"
            aria-label={theme === "dark" ? "Switch to light theme" : "Switch to dark theme"}
            onClick={onTheme}
          >
            {theme === "dark" ? <Sun size={16} /> : <Moon size={16} />}
          </button>
          <span className="spacer" />
          <button className="btn" onClick={onClose}>
            Done
          </button>
        </div>
      </Tabs.Root>
    </ResponsiveDialog>
  );
}
