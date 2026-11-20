import * as Popover from "@radix-ui/react-popover";
import { Info } from "@phosphor-icons/react";
import type { ReactNode } from "react";

type Props = {
  /** What the control is, for a screen reader: it is an icon, so it needs a name. */
  label: string;
  /** The explanation. One or two sentences — a tooltip is not a paragraph. */
  children: ReactNode;
};

/**
 * A one-mark explanation, on demand.
 *
 * The rule it exists for: a promise the product makes in four words ("Live") must be able to explain itself without
 * putting a paragraph on the page. A popover rather than a `title` attribute, because the canon allows no `title`s and
 * because this has to work under a thumb: a tap opens it, a second tap or Escape closes it.
 */
export default function InfoTip({ label, children }: Props) {
  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <button type="button" className="info" aria-label={label}>
          <Info size={14} weight="bold" />
        </button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content className="info__body" side="top" sideOffset={8} collisionPadding={12}>
          {children}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
