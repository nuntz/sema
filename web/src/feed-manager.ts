import type { Feed, FeedItemCount, FeedItemCounts } from "./types";
import { displayFeedTitle } from "./ui/tag-options";
// Undefined means unavailable; a resolved sparse map (including {}) means loaded.
export function feedCount(
  counts: FeedItemCounts | undefined,
  feedID: string,
): FeedItemCount | undefined {
  return counts === undefined
    ? undefined
    : (counts[feedID] ?? { all: 0, unread: 0 });
}

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
  counts?: FeedItemCounts,
): Feed[] {
  const needle = query.trim().toLowerCase();
  return feeds.filter((feed) => {
    const matches =
      filter === "all" ||
      (filter === "attention" &&
        (feed.status === "broken" || feed.status === "slowed")) ||
      (filter === "muted" && feed.muted) ||
      (filter === "never" && !feed.last_fetch_at) ||
      (filter === "quiet" &&
        !feed.muted &&
        feedCount(counts, feed.feed_id)?.all === 0) ||
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
  itemCounts?: FeedItemCounts,
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
    if (!feed.muted && feedCount(itemCounts, feed.feed_id)?.all === 0)
      counts.quiet++;
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
  counts?: FeedItemCounts,
): (first: Feed, second: Feed) => number {
  return (first, second) => {
    const title = () =>
      displayFeedTitle(first).localeCompare(displayFeedTitle(second));
    if (sort === "unread" || sort === "quietest") {
      const a = feedCount(counts, first.feed_id);
      const b = feedCount(counts, second.feed_id);
      if (!a || !b) return title();
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

export function brokenSince(feed: Feed, now = Date.now()): string | undefined {
  if (feed.status !== "broken") return undefined;
  if (feed.refused_since) {
    const refused = Date.parse(feed.refused_since);
    if (Number.isFinite(refused) && refused <= now) return feed.refused_since;
  }
  if (!feed.last_fetch_at) return undefined;
  const attempt = Date.parse(feed.last_fetch_at);
  if (!Number.isFinite(attempt) || attempt > now) return undefined;
  // Estimate the first failure using worker backoff; ignores rate-limit delays
  // and scheduling jitter. last_fetch_at is the latest attempt, not a success.
  const cap = Math.max(24, feed.fetch_interval_h);
  let hours = 0;
  for (
    let attemptNumber = feed.error_count;
    attemptNumber >= 2;
    attemptNumber--
  ) {
    hours += Math.min(2 ** Math.min(attemptNumber - 1, 5), cap);
  }
  return new Date(attempt - hours * 3600000).toISOString();
}

export function cadencePin(feed: Feed): Feed["fetch_interval_h"] | null {
  return feed.cadence_pin_h === undefined
    ? feed.fetch_interval_h
    : feed.cadence_pin_h;
}

export function cadenceLabel(feed: Feed): string {
  const hours = feed.effective_cadence_h ?? feed.fetch_interval_h;
  const cadence =
    hours === 1 ? "Hourly" : hours === 24 ? "Daily" : `Every ${hours}h`;
  return feed.connector !== "reddit" && cadencePin(feed) === null
    ? `Auto · ${cadence.toLowerCase()}`
    : cadence;
}
