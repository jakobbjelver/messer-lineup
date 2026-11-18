import type { ReactNode } from "react";

type Props = {
  onClick: () => void;
  children: ReactNode;
  /** Present but quiet: the count of artists already set. */
  badge?: string;
  label: string;
};

/**
 * The board's one action, floating.
 *
 * It sits above the foot of the sheet, centred, because taste is not a property of the town in the head — it is a thing
 * you do to the whole board, from anywhere on it. The border is the page's own background (so the button reads as a cut
 * through the sheet, not as a card on it) while the glow stays, which is the one place this app spends light on a
 * control rather than on a sign.
 */
export default function Fab({ onClick, children, badge, label }: Props) {
  return (
    <button className="fab" onClick={onClick} aria-label={label}>
      <span className="fab__t">{children}</span>
      {badge ? <span className="fab__n num">{badge}</span> : null}
    </button>
  );
}
