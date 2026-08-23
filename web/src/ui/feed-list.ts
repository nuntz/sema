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
