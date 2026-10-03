import type { Feed, GridScope } from "../types";
import { type TagOption, UNTAGGED_TAG } from "./tag-options";

/**
 * Identity of the user's tag set, independent of how many Feeds carry each
 * tag, so adding a Feed to an existing tag never re-ranks the strip.
 */
export function tagSetKey(feeds: Feed[]): string {
  const tags = new Set<string>();
  for (const feed of feeds)
    if (!feed.muted) for (const tag of feed.tags ?? []) tags.add(tag);
  return [...tags].sort().join("\u0000");
}

export interface TagStripChip {
  tag: string;
  count: number;
}

/**
 * The strip's order for a session: most items first, ties alphabetical.
 * Frozen by the caller so chips never move after a tap.
 */
export function rankStripTags(options: TagOption[]): string[] {
  return options
    .filter((option) => option.tag !== UNTAGGED_TAG)
    .sort(
      (first, second) =>
        second.count - first.count || first.tag.localeCompare(second.tag),
    )
    .map((option) => option.tag);
}

/**
 * Chips to show now: the frozen order applied to current counts, minus
 * zero-count tags, the pseudo-tag "untagged", and the active tag (it sits in
 * the header). Tags the order has not seen go last, in their given order.
 */
export function tagStripChips(
  options: TagOption[],
  order: string[],
  scope: GridScope,
): TagStripChip[] {
  const rank = new Map(order.map((tag, index) => [tag, index]));
  const active = scope?.kind === "tag" ? scope.value : undefined;
  return options
    .filter(
      (option) =>
        option.tag !== UNTAGGED_TAG &&
        option.count > 0 &&
        option.tag !== active,
    )
    .map((option, index) => ({
      chip: { tag: option.tag, count: option.count },
      key: rank.get(option.tag) ?? order.length + index,
    }))
    .sort((first, second) => first.key - second.key)
    .map((entry) => entry.chip);
}
