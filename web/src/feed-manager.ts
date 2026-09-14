import type { Feed, FeedItemCounts } from "./types";
import { displayFeedTitle } from "./ui/tag-options";
export type FeedFilter =
  | "all"
  | "attention"
  | "muted"
  | "never"
  | "quiet"
  | `tag:${string}`;
export function filterFeeds(
  feeds: readonly Feed[],
  filter: FeedFilter,
  query = "",
  recent: FeedItemCounts = {},
): Feed[] {
  const needle = query.trim().toLowerCase();
  return feeds.filter((feed) => {
    const matches =
      filter === "all" ||
      (filter === "attention" &&
        (feed.status === "broken" || feed.status === "slowed")) ||
      (filter === "muted" && feed.muted) ||
      (filter === "never" && !feed.last_fetch_at) ||
      (filter === "quiet" && !feed.muted && recent[feed.feed_id]?.all === 0) ||
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
export function feedFilterCounts(
  feeds: readonly Feed[],
  recent: FeedItemCounts = {},
) {
  const counts = {
    all: feeds.length,
    attention: 0,
    muted: 0,
    never: 0,
    quiet: 0,
    tags: Object.create(null) as Record<string, number>,
  };
  for (const feed of feeds) {
    if (feed.status === "broken" || feed.status === "slowed")
      counts.attention++;
    if (feed.muted) counts.muted++;
    if (!feed.last_fetch_at) counts.never++;
    if (!feed.muted && recent[feed.feed_id]?.all === 0) counts.quiet++;
    for (const tag of new Set(feed.tags ?? []))
      counts.tags[tag] = (counts.tags[tag] ?? 0) + 1;
  }
  return counts;
}

export type FeedSort =
  | "title"
  | "updated"
  | "errors"
  | "prior"
  | "quality"
  | "unread"
  | "quietest";
export function compareFeeds(
  sort: FeedSort,
  counts: FeedItemCounts = {},
  recent: FeedItemCounts = {},
): (first: Feed, second: Feed) => number {
  return (first, second) => {
    const title = () =>
      displayFeedTitle(first).localeCompare(displayFeedTitle(second));
    if (sort === "unread" || sort === "quietest") {
      const map = sort === "unread" ? counts : recent;
      const a = map[first.feed_id];
      const b = map[second.feed_id];
      if (!a || !b) return a ? -1 : b ? 1 : title();
      return (
        (sort === "unread" ? b.unread - a.unread : a.all - b.all) || title()
      );
    }
    switch (sort) {
      case "updated":
        return (
          (second.last_fetch_at ?? "").localeCompare(
            first.last_fetch_at ?? "",
          ) || title()
        );
      case "errors":
        return statusWeight(second) - statusWeight(first) || title();
      case "prior":
        return second.prior - first.prior || title();
      case "quality":
        return (
          (first.extraction_success_rate ?? 2) -
            (second.extraction_success_rate ?? 2) ||
          (first.average_extract_quality ?? 2) -
            (second.average_extract_quality ?? 2) ||
          title()
        );
      default:
        return title();
    }
  };
}
function statusWeight(feed: Feed): number {
  return feed.status === "broken"
    ? 3
    : feed.status === "slowed"
      ? 2
      : feed.status === "muted"
        ? 1
        : 0;
}
