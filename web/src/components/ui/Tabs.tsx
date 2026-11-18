import * as Tabs from "@radix-ui/react-tabs";
import type { ReactNode } from "react";

/**
 * Tabs as a control, not as two buttons that look pressed.
 *
 * `role="tablist"` and `aria-selected` on hand-written buttons is the half of the pattern everyone remembers; roving
 * tabindex, arrow-key movement, Home/End and the `aria-controls`/`aria-labelledby` pair are the half that makes it usable
 * from a keyboard and from a screen reader. Radix supplies all of it, and the panel below is its `Content` so the two
 * halves cannot disagree.
 */
export const Root = Tabs.Root;
export const Content = Tabs.Content;

type ListProps = { children: ReactNode; label: string };

export function List({ children, label }: ListProps) {
  return (
    <Tabs.List className="ui-tabs" aria-label={label}>
      {children}
    </Tabs.List>
  );
}

export function Tab({ value, children }: { value: string; children: ReactNode }) {
  return (
    <Tabs.Trigger className="ui-tab" value={value}>
      {children}
    </Tabs.Trigger>
  );
}
