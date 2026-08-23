import type { Feed } from "../types";

export interface TagOption {
  tag: string;
  count: number;
}

export function feedTagOptions(feeds: Feed[]): TagOption[] {
  const counts = new Map<string, number>();
  let untagged = 0;
  for (const feed of feeds) {
    if (feed.muted) continue;
    if (!feed.tags?.length) untagged += feed.item_count ?? 0;
    for (const tag of feed.tags ?? [])
      counts.set(tag, (counts.get(tag) ?? 0) + (feed.item_count ?? 0));
  }
  return [
    ...[...counts]
      .sort(([first], [second]) => first.localeCompare(second))
      .map(([tag, count]) => ({ tag, count })),
    { tag: "untagged", count: untagged },
  ];
}
