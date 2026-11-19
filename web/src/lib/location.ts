/**
 * The visitor's own position, asked for once and kept in memory.
 *
 * Browser GPS is the design's first-ranked location input because it is the only precise one: no account, no server
 * call, one permission prompt. It is also the only input that may produce a printed distance, because it is the only
 * one that knows where the reader is standing.
 *
 * Three rules:
 *
 *   - **Never stored.** The fix lives in this component's state and dies with the tab. Nothing writes it to
 *     localStorage, the URL, or a request payload.
 *   - **Ask, never nag.** `request()` is only called by a control the visitor pressed; nothing fires on mount, so a
 *     cold visit costs no prompt.
 *   - **A refusal is a state.** `denied` is something the panel reports plainly and then leaves alone; it is never
 *     retried automatically, and the town list is still the answer.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import type { Point } from "./geo";

export type Position =
  | { state: "idle" }
  | { state: "asking" }
  | { state: "granted"; point: Point }
  | { state: "denied" }
  | { state: "unavailable" };

/** GPS needs a secure context: on plain HTTP the browser answers nothing at all, so the row is not offered. */
export function locationSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    window.isSecureContext &&
    typeof navigator !== "undefined" &&
    "geolocation" in navigator
  );
}

export function useDevicePosition(): { position: Position; request: () => void } {
  const [position, setPosition] = useState<Position>({ state: "idle" });

  // A tab that unmounts mid-prompt must not set state afterwards.
  const live = useRef(true);
  useEffect(
    () => () => {
      live.current = false;
    },
    [],
  );

  const request = useCallback(() => {
    if (!locationSupported()) {
      setPosition({ state: "unavailable" });
      return;
    }
    setPosition({ state: "asking" });
    navigator.geolocation.getCurrentPosition(
      (fix) => {
        if (!live) return;
        setPosition({ state: "granted", point: { lat: fix.coords.latitude, lon: fix.coords.longitude } });
      },
      (error) => {
        if (!live) return;
        // PERMISSION_DENIED is a decision; a timeout or a missing signal is not, so it keeps the door open.
        setPosition({ state: error.code === error.PERMISSION_DENIED ? "denied" : "unavailable" });
      },
      // A town-level answer is enough for a board, and a cached fix inside five minutes is the same answer.
      { enableHighAccuracy: false, timeout: 10_000, maximumAge: 300_000 },
    );
  }, [live]);

  return { position, request };
}
