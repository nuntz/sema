import { describe, expect, it } from "vitest";
import {
  redditCanonicalURL,
  redditCollectionFromURL,
  redditCollectionLabel,
  redditSubreddit,
} from "./reddit-feed";

describe("Reddit feed collections", () => {
  it.each([
    ["https://www.reddit.com/r/castles/.rss", "hot"],
    ["https://www.reddit.com/r/castles/top.rss?t=day", "top-day"],
    ["https://www.reddit.com/r/castles/new.rss", "new"],
  ] as const)("reads %s as %s", (url, collection) => {
    expect(redditCollectionFromURL(url)).toBe(collection);
  });

  it("builds canonical URLs without changing the subreddit", () => {
    const source = "https://www.reddit.com/r/Castles/top.rss?t=day";
    expect(redditCanonicalURL(source, "hot")).toBe(
      "https://www.reddit.com/r/castles/.rss",
    );
    expect(redditCanonicalURL(source, "new")).toBe(
      "https://www.reddit.com/r/castles/new.rss",
    );
  });

  it("formats the product labels", () => {
    expect(redditCollectionLabel("top-day")).toBe("Top · day");
    expect(redditSubreddit("https://www.reddit.com/r/Castles/.rss")).toBe(
      "castles",
    );
  });
});
