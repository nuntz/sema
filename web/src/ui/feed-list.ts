import type { Feed } from "../types";

export function upsertFeed(
  current: Feed[] | undefined,
  incoming: Feed,
): Feed[] {
  const feeds = current ?? [];
  const existing = feeds.findIndex((feed) => feed.feed_id === incoming.feed_id);
  if (existing === -1) return [...feeds, incoming];

  return feeds.map((feed, index) =>
    index === existing ? { ...feed, ...incoming } : feed,
  );
}

export function feedFetchLabel(
  feed: Pick<Feed, "status">,
  age?: string,
): string {
  if (feed.status === "muted") return "muted";
  if (feed.status === "slowed" || feed.status === "broken")
    return age ? `${feed.status} ${age}` : feed.status;
  return age ? `${age} ago` : "never";
}
