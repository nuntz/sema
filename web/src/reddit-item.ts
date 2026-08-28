import type { Connector, Item } from "./types";

export function connectorKind(value: unknown): Connector {
  return value === "youtube" || value === "reddit" ? value : "rss";
}

export function isRedditItem(item: Item): boolean {
  return connectorKind(item.connector) === "reddit";
}

export function isRedditLink(item: Item): boolean {
  return isRedditItem(item) && item.post_type === "link" && !!item.external_url;
}

export type RedditPrimaryRoute =
  | { kind: "reader" }
  | { kind: "external"; url: string };

function parsedExternalURL(item: Item): URL | undefined {
  if (!item.external_url) return;
  try {
    return new URL(item.external_url);
  } catch {
    return;
  }
}

export function isRedditGallery(item: Item): boolean {
  if (!isRedditItem(item)) return false;
  if (item.post_type === "gallery") return true;
  if (item.post_type !== "image") return false;
  const parsed = parsedExternalURL(item);
  if (!parsed) return false;
  const host = parsed.hostname.toLowerCase().replace(/\.$/, "");
  return (
    (host === "reddit.com" || host.endsWith(".reddit.com")) &&
    parsed.pathname.toLowerCase().startsWith("/gallery/")
  );
}

export function redditPrimaryRoute(item: Item): RedditPrimaryRoute {
  if (!isRedditItem(item)) return { kind: "reader" };
  const parsed = parsedExternalURL(item);
  const video =
    item.post_type === "video" ||
    parsed?.hostname.toLowerCase() === "v.redd.it";
  if (video && item.external_url)
    return { kind: "external", url: item.external_url };
  if (
    isRedditGallery(item) &&
    !item.has_body &&
    !item.media_url &&
    item.external_url
  )
    return { kind: "external", url: item.external_url };
  return { kind: "reader" };
}

export function redditReaderOriginalURL(item: Item): string {
  if (isRedditItem(item) && item.post_type === "link" && item.external_url)
    return item.external_url;
  return item.url;
}

export function showsReaderOriginalFallback(item: Item): boolean {
  return !isRedditItem(item) || item.post_type === "link";
}

export function externalHost(raw?: string): string {
  if (!raw) return "";
  try {
    return new URL(raw).hostname.replace(/^www\./i, "");
  } catch {
    return "";
  }
}

export function redditSummaryProvenance(item: Item): string {
  return item.post_type === "link"
    ? "Summarised from the linked page"
    : "Summarised from the post";
}
