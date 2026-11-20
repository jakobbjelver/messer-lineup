import { useEffect } from "react";
import { nearestTown, type Point } from "./geo";
import type { Town, TownEntry } from "./indexData";
import type { Position } from "./location";

/**
 * A fix answers the town question — and only that.
 *
 * This is the one place a device position chooses a town: the pickers (first run and settings). On
 * the board, a granted fix only adds distances; it must never move the reader to another town, because
 * a silent move is a worse surprise than a missing number.
 */
export function useNearestTown(
  position: Position,
  posts: TownEntry[],
  names: Town[],
  onTown: (id: string) => void,
): void {
  useEffect(() => {
    if (position.state !== "granted") return;
    const published = names.filter((entry) => posts.some((post) => post.town === entry.id));
    const near = nearestTown(published, position.point as Point);
    if (near) onTown(near.town.id);
  }, [position, names, posts, onTown]);
}
