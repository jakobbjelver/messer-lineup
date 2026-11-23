import type { ReactNode } from "react";

/**
 * A square label — a genre on a row or on an artist page.
 *
 * One shape, one hairline, the dimmest ink: a badge is a word the reader scans, not a state. The accent is never spent
 * here — the board uses colour to mark the reader's own rows, and a genre is an identity, not a status. Square like
 * every other corner in this app (`--radius` does not exist on purpose).
 */
export default function Badge({ children }: { children: ReactNode }) {
  return <span className="badge">{children}</span>;
}
