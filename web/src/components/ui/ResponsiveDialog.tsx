import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";
import { useWide } from "../../lib/useMediaQuery";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The overlay's accessible name. Radix requires a title; this one is not drawn twice. */
  label: string;
  children: ReactNode;
  className?: string;
  /** A sheet can be dismissed by its own back-step before it closes; the dialog's Escape is that step's own key. */
  onEscape?: () => boolean;
};

/**
 * One overlay, two shapes — canon §11 and the family's overlay rules in one block.
 *
 * A **dialog** is right when there is room around it: centred, 560px, focus trapped, scrim behind. A **bottom sheet** is
 * right on a phone: full width, held to the bottom edge, scrollable inside, safe-area padded. They are not two styles of
 * the same box — the position, the radius, the drag-to-dismiss affordance and the scroll container all differ — so the
 * shape is chosen from the medium rather than forced into one component's narrow view.
 *
 * Radix supplies what a hand-rolled overlay always gets wrong: the focus trap and its restore, `aria-modal`, scroll lock,
 * Escape, and outside-press. The look is ours, in `styles/ui.css`.
 */
export default function ResponsiveDialog({ open, onOpenChange, label, children, className, onEscape }: Props) {
  const wide = useWide();

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="ui-scrim" />
        <Dialog.Content
          className={className ? `ui-dialog ${className}` : "ui-dialog"}
          data-medium={wide ? "dialog" : "sheet"}
          aria-modal="true"
          aria-describedby={undefined}
          onEscapeKeyDown={(event) => {
            if (onEscape?.()) event.preventDefault();
          }}
        >
          <Dialog.Title className="ui-sr"> {label} </Dialog.Title>
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
