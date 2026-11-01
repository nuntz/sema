import { clockNow } from "../clock";
import { cadenceLabel } from "../feed-manager";
import { redditCollectionFromURL, redditCollectionLabel } from "../reddit-feed";
import { agoPhrase, relativeAgo } from "../relative-time";
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
  return age ? agoPhrase(age) : "never";
}

export function feedDescriptor(feed: Feed): string {
  if (feed.connector === "youtube") return "YouTube";
  if (feed.connector === "reddit")
    return `reddit.com · ${redditCollectionLabel(
      redditCollectionFromURL(feed.url),
    ).toLowerCase()}`;
  return domainName(feed.url);
}

export function drawerDescriptor(feed: Feed): string {
  const checked = feed.last_fetch_at
    ? `checked ${relativeAgo(feed.last_fetch_at, clockNow())}`
    : "not checked yet";
  if (feed.connector === "youtube") return `YouTube · uploads · ${checked}`;
  if (feed.connector === "reddit")
    return `${feedDescriptor(feed)} · ${cadenceLabel(feed)} · ${checked}`;
  return feed.last_fetch_at
    ? `last fetched ${relativeAgo(feed.last_fetch_at, clockNow())}`
    : "not fetched yet";
}

export function domainName(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    return (
      parsed.hostname.replace(/^www\./, "") +
      (parsed.pathname === "/" ? "" : parsed.pathname.replace(/\/$/, ""))
    );
  } catch {
    return rawURL;
  }
}
