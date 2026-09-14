import type { Feed } from "./types";
import { displayFeedTitle } from "./ui/tag-options";
export type FeedFilter =
  | "all"
  | "attention"
  | "muted"
  | "never"
  | `tag:${string}`;
export function filterFeeds(
  feeds: readonly Feed[],
  filter: FeedFilter,
  query = "",
): Feed[] {
  const needle = query.trim().toLowerCase();
  return feeds.filter((feed) => {
    const matches =
      filter === "all" ||
      (filter === "attention" &&
        (feed.status === "broken" || feed.status === "slowed")) ||
      (filter === "muted" && feed.muted) ||
      (filter === "never" && !feed.last_fetch_at) ||
      (filter.startsWith("tag:") &&
        (feed.tags ?? []).includes(filter.slice(4)));
    return (
      matches &&
      (!needle ||
        [displayFeedTitle(feed), feed.url, ...(feed.tags ?? [])]
          .join(" ")
          .toLowerCase()
          .includes(needle))
    );
  });
}
export function feedFilterCounts(feeds: readonly Feed[]) {
  const counts = {
    all: feeds.length,
    attention: 0,
    muted: 0,
    never: 0,
    tags: Object.create(null) as Record<string, number>,
  };
  for (const feed of feeds) {
    if (feed.status === "broken" || feed.status === "slowed")
      counts.attention++;
    if (feed.muted) counts.muted++;
    if (!feed.last_fetch_at) counts.never++;
    for (const tag of new Set(feed.tags ?? []))
      counts.tags[tag] = (counts.tags[tag] ?? 0) + 1;
  }
  return counts;
}
