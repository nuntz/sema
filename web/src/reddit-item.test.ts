import { describe, expect, it } from "vitest";
import {
  connectorKind,
  externalHost,
  isRedditGallery,
  isRedditLink,
  redditPrimaryRoute,
  redditReaderImageSources,
  redditReaderOriginalURL,
  redditSummaryProvenance,
  showsReaderOriginalFallback,
} from "./reddit-item";
import type { Item } from "./types";

const item = (value: Partial<Item>): Item =>
  ({
    item_id: "item",
    feed_id: "feed",
    url: "https://reddit.com/r/example/comments/one/title/",
    title: "Title",
    summary_source: "feed",
    published_ts: "2026-08-26T12:00:00Z",
    fetched_ts: "2026-08-26T12:01:00Z",
    has_body: false,
    extract_quality: 0,
    score: 0,
    size: "M",
    read: false,
    signal: 0,
    hearted: false,
    ...value,
  }) as Item;

describe("Reddit item presentation", () => {
  it("uses a safe RSS fallback for unknown connector values", () => {
    expect(connectorKind("reddit")).toBe("reddit");
    expect(connectorKind("future-connector")).toBe("rss");
  });

  it("recognizes only link posts with an external primary destination", () => {
    expect(
      isRedditLink(
        item({
          connector: "reddit",
          post_type: "link",
          external_url: "https://example.com/story",
        }),
      ),
    ).toBe(true);
    expect(isRedditLink(item({ connector: "reddit", post_type: "text" }))).toBe(
      false,
    );
  });

  it("opens every ordinary Reddit type in Reader except video", () => {
    for (const post_type of ["text", "link", "image"] as const) {
      expect(
        redditPrimaryRoute(
          item({
            connector: "reddit",
            post_type,
            external_url: "https://example.com/story",
          }),
        ),
      ).toEqual({ kind: "reader" });
    }
    expect(
      redditPrimaryRoute(
        item({
          connector: "reddit",
          post_type: "video",
          external_url: "https://v.redd.it/one",
        }),
      ),
    ).toEqual({ kind: "external", url: "https://v.redd.it/one" });
  });

  it("opens galleries in Reader when a body or first image was cached", () => {
    const gallery = item({
      connector: "reddit",
      post_type: "gallery",
      external_url: "https://www.reddit.com/gallery/one",
    });
    expect(isRedditGallery(gallery)).toBe(true);
    expect(redditPrimaryRoute(gallery)).toEqual({
      kind: "external",
      url: "https://www.reddit.com/gallery/one",
    });
    expect(redditPrimaryRoute({ ...gallery, has_body: true })).toEqual({
      kind: "reader",
    });
    expect(
      redditPrimaryRoute({ ...gallery, media_url: "/media/one.webp" }),
    ).toEqual({ kind: "reader" });
  });

  it("recognizes legacy galleries without broad URL inference", () => {
    expect(
      isRedditGallery(
        item({
          connector: "reddit",
          post_type: "image",
          external_url: "https://old.reddit.com/gallery/legacy",
        }),
      ),
    ).toBe(true);
    expect(
      isRedditGallery(
        item({
          connector: "reddit",
          post_type: "link",
          external_url: "https://example.com/gallery/story",
        }),
      ),
    ).toBe(false);
  });

  it("prefers cached reader media before the direct Reddit image", () => {
    expect(
      redditReaderImageSources(
        item({
          connector: "reddit",
          post_type: "image",
          media_url: "/media/lead.jpg",
          external_url: "https://i.redd.it/original.jpg",
        }),
      ),
    ).toEqual([
      { kind: "stored" },
      { kind: "external", url: "https://i.redd.it/original.jpg" },
    ]);
  });

  it("retains direct media only as a fallback when stored media is absent", () => {
    expect(
      redditReaderImageSources(
        item({
          connector: "reddit",
          post_type: "image",
          external_url: "https://i.redd.it/original.jpg",
        }),
      ),
    ).toEqual([{ kind: "external", url: "https://i.redd.it/original.jpg" }]);
    expect(
      redditReaderImageSources(
        item({ connector: "reddit", post_type: "image" }),
      ),
    ).toEqual([]);
  });

  it("does not treat a Reddit gallery page as a direct image fallback", () => {
    expect(
      redditReaderImageSources(
        item({
          connector: "reddit",
          post_type: "gallery",
          media_url: "/media/gallery.jpg",
          external_url: "https://www.reddit.com/gallery/one",
        }),
      ),
    ).toEqual([{ kind: "stored" }]);
  });

  it("sends only failed link extraction to the linked article", () => {
    const linked = item({
      connector: "reddit",
      post_type: "link",
      external_url: "https://example.com/article",
    });
    expect(showsReaderOriginalFallback(linked)).toBe(true);
    expect(redditReaderOriginalURL(linked)).toBe("https://example.com/article");
    for (const post_type of ["text", "image", "gallery", "video"] as const) {
      const reddit = item({ connector: "reddit", post_type });
      expect(showsReaderOriginalFallback(reddit)).toBe(false);
      expect(redditReaderOriginalURL(reddit)).toBe(reddit.url);
    }
  });

  it("leaves the normal RSS original fallback unchanged", () => {
    const rss = item({ connector: "rss", url: "https://example.com/article" });
    expect(showsReaderOriginalFallback(rss)).toBe(true);
    expect(redditReaderOriginalURL(rss)).toBe(rss.url);
  });

  it("formats display domains and summary provenance", () => {
    expect(externalHost("https://www.example.com/path?q=1#part")).toBe(
      "example.com",
    );
    expect(redditSummaryProvenance(item({ post_type: "link" }))).toBe(
      "Summarised from the linked page",
    );
    expect(redditSummaryProvenance(item({ post_type: "text" }))).toBe(
      "Summarised from the post",
    );
  });
});
