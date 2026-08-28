export type RedditCollection = "hot" | "top-day" | "new";

export function redditCollectionFromURL(rawURL: string): RedditCollection {
  try {
    const parsed = new URL(rawURL);
    const pathname = parsed.pathname.toLowerCase().replace(/\/+$/, "");
    if (pathname.endsWith("/new.rss")) return "new";
    if (
      pathname.endsWith("/top.rss") &&
      parsed.searchParams.get("t")?.toLowerCase() === "day"
    )
      return "top-day";
  } catch {
    // Stored Reddit feeds are canonical URLs; malformed legacy values fall
    // back to the product default until the migration repairs them.
  }
  return "hot";
}

export function redditCanonicalURL(
  rawURL: string,
  collection: RedditCollection,
): string {
  const subreddit = redditSubreddit(rawURL);
  if (!subreddit) return rawURL;
  const base = `https://www.reddit.com/r/${subreddit}`;
  switch (collection) {
    case "top-day":
      return `${base}/top.rss?t=day`;
    case "new":
      return `${base}/new.rss`;
    default:
      return `${base}/.rss`;
  }
}

export function redditCollectionLabel(
  collection: RedditCollection,
): "Hot" | "Top · day" | "New" {
  switch (collection) {
    case "top-day":
      return "Top · day";
    case "new":
      return "New";
    default:
      return "Hot";
  }
}

export function redditSubreddit(rawURL: string): string | undefined {
  try {
    const parsed = new URL(rawURL);
    const match = parsed.pathname.match(/^\/r\/([a-z0-9_]{2,21})(?:\/|$)/i);
    return match?.[1].toLowerCase();
  } catch {
    return undefined;
  }
}
