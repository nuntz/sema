import { describe, expect, it } from "vitest";
import { archiveSize } from "../archive";
import { formatPrior, whyText } from "../ranking-display";
import type { Feed, FeedCandidate, Item } from "../types";
import { discoveredCandidateState } from "./feed-discovery";
import { upsertFeed } from "./feed-list";

describe("archive storage estimate", () => {
  it("uses the specified 300 KB per kept item estimate", () => {
    expect(archiveSize(1)).toBe("0.3 MB");
    expect(archiveSize(25)).toBe("7.5 MB");
    expect(archiveSize(124)).toBe("37 MB");
  });
});

describe("ranking diagnostics", () => {
  it("formats signed priors with a typographic minus", () => {
    expect(formatPrior(0.091)).toBe("+0.09");
    expect(formatPrior(0)).toBe("0.00");
    expect(formatPrior(-0.041)).toBe("−0.04");
  });

  it("uses only the two approved why phrasings", () => {
    const item = { why: { title: "A story" } } as Item;
    expect(whyText(item)).toBe("Because you liked: A story");
    item.why = { feed_title: "Example" };
    expect(whyText(item)).toBe("You often like Example");
    item.why = undefined;
    expect(whyText(item)).toBe("");
  });
});

describe("add-feed discovery picker", () => {
  const candidate = { feed_url: "https://example.com/feed" } as FeedCandidate;

  it("distinguishes single, multiple, no-result, and error states", () => {
    expect(discoveredCandidateState([candidate])).toBe("single");
    expect(discoveredCandidateState([candidate, candidate])).toBe("multiple");
    expect(discoveredCandidateState([])).toBe("none");
    expect(discoveredCandidateState([], true)).toBe("error");
  });
});

describe("feed list updates", () => {
  const feed = (feedID: string, title: string) =>
    ({ feed_id: feedID, title }) as Feed;

  it("adds the feed returned by the create request immediately", () => {
    const current = [feed("existing", "Existing")];
    const added = feed("new", "New feed");

    expect(upsertFeed(current, added)).toEqual([...current, added]);
  });

  it("updates an existing feed without duplicating it", () => {
    const current = [feed("same", "Old title")];

    expect(upsertFeed(current, feed("same", "New title"))).toEqual([
      feed("same", "New title"),
    ]);
  });
});
