import type { Item } from "./types";

export type SignalValue = -1 | 0 | 1;
export function signalWhy(value: SignalValue, story = false): string {
  if (!value) return "";
  return value === 1
    ? story
      ? "More of this story from tomorrow"
      : "More like this from tomorrow"
    : story
      ? "Fewer of this story · undo"
      : "Fewer like this · undo";
}
export function buryDisabled(item: Pick<Item, "hearted">): boolean {
  return item.hearted;
}
export function signalLabel(value: SignalValue): string {
  return value === 1 ? "boosted" : value === -1 ? "buried" : "";
}
export function signalActionLabel(
  value: SignalValue,
  action: "boost" | "bury",
  kept: boolean,
): string {
  if (action === "bury" && kept) return "Kept items can't be buried";
  return action === "boost"
    ? value === 1
      ? "Boosted (+ to undo)"
      : "Boost (+)"
    : value === -1
      ? "Buried (− to undo)"
      : "Bury (−)";
}
export function clusterActions(size: "S" | "M" | "L") {
  return size === "S"
    ? (["boost", "bury"] as const)
    : (["boost", "bury", "keep", "more"] as const);
}
export interface SignalNotice {
  id: number;
  itemID: string;
  value: SignalValue;
  text: string;
  undo: boolean;
}
// Each action produces a single replacement; counts are shared across all surfaces.
export function nextSignalNotice(
  count: number,
  id: number,
  itemID: string,
  value: SignalValue,
): SignalNotice | undefined {
  if (count >= 5) return undefined;
  return {
    id,
    itemID,
    value,
    text:
      value === 0
        ? "Back to normal."
        : value === 1
          ? "Boosted. Sizes settle overnight."
          : "Buried. Sizes settle overnight.",
    undo: value !== 0,
  };
}
