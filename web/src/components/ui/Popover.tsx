import * as Popover from "@radix-ui/react-popover";
import type { ReactNode } from "react";

/**
 * A popup, not a `<div>` that happens to be positioned.
 *
 * The panel that used to be an absolutely-positioned child of the coverage band had every problem a hand-rolled popup
 * has: it could run off the screen, it ignored the scroll container, it closed on a document click it guessed at, and it
 * never gave focus back. Radix Popover supplies collision-aware placement, the outside-press that actually means outside,
 * focus restoration and a portal that escapes the sheet's `overflow:hidden`.
 */
export const Root = Popover.Root;
export const Trigger = Popover.Trigger;
export const Anchor = Popover.Anchor;

type ContentProps = {
  children: ReactNode;
  className?: string;
  side?: "top" | "right" | "bottom" | "left";
  align?: "start" | "center" | "end";
};

export function Content({ children, className, side = "bottom", align = "start" }: ContentProps) {
  return (
    <Popover.Portal>
      <Popover.Content
        className={className ? `ui-pop ${className}` : "ui-pop"}
        side={side}
        align={align}
        sideOffset={6}
        collisionPadding={12}
      >
        {children}
      </Popover.Content>
    </Popover.Portal>
  );
}
